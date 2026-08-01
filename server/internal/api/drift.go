package api

// Source-drift detection (see the drift plan): an AI job verifies requirement
// documents against the selected read-only reference sources, one document
// per tool loop, and files confirmed findings as work items on a configured
// tracker (GitHub/GitLab issues, Jira). Findings are derived state in SQLite;
// the durable artifact of a filed finding is the `work-items:` frontmatter
// entry written into the document itself.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"specquill/server/internal/ai"
	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/forge"
	"specquill/server/internal/mdfm"
	"specquill/server/internal/okf"
	"specquill/server/internal/project"
	"specquill/server/internal/store"
	"specquill/server/internal/tracker"
)

const defaultDriftMaxDocs = 50

// driftRegistry tracks the in-flight run per repo+branch: one at a time, and
// the cancel endpoint needs a handle on the worker's context.
type driftRegistry struct {
	mu sync.Mutex
	m  map[string]context.CancelFunc
}

func newDriftRegistry() *driftRegistry { return &driftRegistry{m: map[string]context.CancelFunc{}} }

func driftKey(repoKey, branch string) string { return repoKey + "\x00" + branch }

// claim registers a run unless one is already active for the key.
func (d *driftRegistry) claim(key string, cancel context.CancelFunc) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, busy := d.m[key]; busy {
		return false
	}
	d.m[key] = cancel
	return true
}

func (d *driftRegistry) release(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.m, key)
}

// cancel stops the active run for key, reporting whether one existed.
func (d *driftRegistry) cancel(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.m[key]
	if ok {
		c()
	}
	return ok
}

// ---------------------------------------------------------------- findings

var driftKindSet = map[string]bool{
	"missing-implementation": true, "undocumented-behavior": true,
	"contradiction": true, "outdated-requirement": true,
}

func normDriftKind(k string) string {
	k = strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(k)), " ", "-"), "_", "-")
	if !driftKindSet[k] {
		return "contradiction"
	}
	return k
}

func normDriftSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high":
		return "high"
	case "low":
		return "low"
	default:
		return "medium"
	}
}

// driftFingerprint is the stable finding identity: document structure, never
// model prose — dismissals stick across runs and reworded titles.
func driftFingerprint(docPath, source, kind, anchor string) string {
	h := sha256.Sum256([]byte(docPath + "\x00" + source + "\x00" + kind + "\x00" +
		strings.ToLower(strings.Join(strings.Fields(anchor), " "))))
	return hex.EncodeToString(h[:])[:16]
}

type driftEvidence struct {
	Path  string `json:"path"`
	Quote string `json:"quote"`
}

// modelFinding is the JSON shape the drift prompt asks the model for.
type modelFinding struct {
	Anchor      string          `json:"anchor"`
	Source      string          `json:"source"`
	Kind        string          `json:"kind"`
	Severity    string          `json:"severity"`
	Title       string          `json:"title"`
	Detail      string          `json:"detail"`
	SourcePaths []string        `json:"sourcePaths"`
	Evidence    []driftEvidence `json:"evidence"`
}

// verifyEvidence checks every quote verbatim (whitespace-normalized) against
// the named source's snapshot — hallucinated evidence never reaches the user.
func verifyEvidence(f modelFinding, sources []ai.GroundingSource) bool {
	if len(f.Evidence) == 0 {
		return false
	}
	var src *ai.GroundingSource
	for i := range sources {
		if sources[i].Name == f.Source {
			src = &sources[i]
			break
		}
	}
	if src == nil {
		return false
	}
	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	for _, ev := range f.Evidence {
		quote := norm(ev.Quote)
		if quote == "" {
			return false
		}
		path := strings.TrimPrefix(ev.Path, "~"+src.Name+"/")
		content, ok := src.Files[path]
		if !ok {
			return false
		}
		if !strings.Contains(norm(content), quote) {
			return false
		}
	}
	return true
}

// resolveDriftScope expands the requested paths (fallback: the config's
// drift.paths, then every doc) into the concrete markdown doc list.
func resolveDriftScope(files map[string]string, requested, configured []string) []string {
	paths := requested
	if len(paths) == 0 {
		paths = configured
	}
	candidate := func(p string) bool {
		return strings.HasSuffix(p, ".md") && !strings.HasPrefix(p, ".") &&
			!strings.HasPrefix(p, "uploads/") && !okf.Reserved(base(p))
	}
	seen := map[string]bool{}
	var docs []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			docs = append(docs, p)
		}
	}
	if len(paths) == 0 {
		for p := range files {
			if candidate(p) {
				add(p)
			}
		}
		sort.Strings(docs)
		return docs
	}
	for _, req := range paths {
		req = strings.TrimPrefix(strings.TrimSpace(req), "/")
		if req == "" {
			continue
		}
		if _, ok := files[req]; ok && candidate(req) {
			add(req)
			continue
		}
		prefix := strings.TrimSuffix(req, "/") + "/"
		var under []string
		for p := range files {
			if strings.HasPrefix(p, prefix) && candidate(p) {
				under = append(under, p)
			}
		}
		sort.Strings(under)
		for _, p := range under {
			add(p)
		}
	}
	sort.Strings(docs)
	return docs
}

// ---------------------------------------------------------------- handlers

// GET /api/repos/{repo}/drift?branch= — latest run + live findings + targets.
func (s *Server) getDrift(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	out := map[string]any{"enabled": s.ai != nil}
	if run, err := s.store.LatestDriftRun(repo.Key(), branch); err == nil {
		out["run"] = driftRunWire(run)
	} else {
		out["run"] = nil
	}
	findings, err := s.store.DriftFindings(repo.Key(), branch)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	wireFindings := make([]map[string]any, 0, len(findings))
	for _, f := range findings {
		wireFindings = append(wireFindings, driftFindingWire(f))
	}
	out["findings"] = wireFindings
	targets := s.driftTargets(repo, branch)
	wireTargets := make([]map[string]any, 0, len(targets))
	for _, t := range targets {
		wireTargets = append(wireTargets, map[string]any{"name": t.name, "kind": t.kind, "project": t.project})
	}
	out["targets"] = wireTargets
	jsonOK(w, out)
}

func driftRunWire(run *store.DriftRun) map[string]any {
	var scope []string
	_ = json.Unmarshal([]byte(run.ScopeJSON), &scope)
	return map[string]any{
		"id": run.ID, "status": run.Status, "error": run.Error, "scope": scope,
		"docsTotal": run.DocsTotal, "docsDone": run.DocsDone,
		"droppedUnverified": run.DroppedUnverified, "headSha": run.HeadSHA,
		"startedAt": run.StartedAt, "finishedAt": run.FinishedAt,
	}
}

func driftFindingWire(f store.DriftFinding) map[string]any {
	var evidence []driftEvidence
	_ = json.Unmarshal([]byte(f.EvidenceJSON), &evidence)
	return map[string]any{
		"fingerprint": f.Fingerprint, "docPath": f.DocPath, "anchor": f.Anchor,
		"source": f.Source, "kind": f.Kind, "severity": f.Severity,
		"title": f.Title, "detail": f.Detail, "evidence": evidence,
		"status": f.Status, "workItemUrl": f.WorkItemURL, "workItemTarget": f.WorkItemTarget,
		"updatedAt": f.UpdatedAt,
	}
}

// POST /api/repos/{repo}/drift/run?branch= {paths?} — start an async run.
func (s *Server) postDriftRun(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "Speccy is not configured (ai: in specquill.yml)")
		return
	}
	var body struct {
		Paths []string `json:"paths"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // empty body = default scope
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	files, err := repo.Snapshot(branch)
	if err != nil {
		gitFail(w, err)
		return
	}
	var driftCfg project.DriftConfig
	if cfg := inRepoConfig(repo, branch); cfg != nil {
		driftCfg = cfg.Drift
	}
	docs := resolveDriftScope(files, body.Paths, driftCfg.Paths)
	if len(docs) == 0 {
		jsonError(w, http.StatusUnprocessableEntity, "no documents in scope")
		return
	}
	maxDocs := driftCfg.MaxDocs
	if maxDocs <= 0 {
		maxDocs = defaultDriftMaxDocs
	}
	if len(docs) > maxDocs {
		jsonError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("scope resolves to %d documents (max %d) — narrow the paths or raise drift.max_docs", len(docs), maxDocs))
		return
	}
	sources := s.driftSources(r, repo, branch, driftCfg)
	if len(sources) == 0 {
		jsonError(w, http.StatusUnprocessableEntity,
			"no reference sources selected — drift needs references in .specquill/config.yml")
		return
	}

	key := driftKey(repo.Key(), branch)
	ctx, cancel := context.WithCancel(context.Background())
	if !s.drift.claim(key, cancel) {
		cancel()
		jsonError(w, http.StatusConflict, "a drift run is already in progress for this branch")
		return
	}
	scopeJSON, _ := json.Marshal(docs)
	headSHA, _ := repo.Repo.Head(branch)
	runID, err := s.store.CreateDriftRun(store.DriftRun{
		RepoKey: repo.Key(), Branch: branch, ScopeJSON: string(scopeJSON),
		DocsTotal: len(docs), HeadSHA: headSHA,
	})
	if err != nil {
		s.drift.release(key)
		cancel()
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	go s.driftWorker(ctx, cancel, key, runID, repo, branch, docs, files, sources, driftCfg.Instructions)
	jsonOK(w, map[string]any{"runId": runID, "docsTotal": len(docs)})
}

// POST /api/repos/{repo}/drift/cancel?branch=
func (s *Server) postDriftCancel(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	if !s.drift.cancel(driftKey(repo.Key(), branch)) {
		jsonError(w, http.StatusNotFound, "no drift run in progress")
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

// POST /api/repos/{repo}/drift/findings/{fp}/dismiss {reopen?}
func (s *Server) postDriftDismiss(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	var body struct {
		Reopen bool `json:"reopen"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	status := "dismissed"
	if body.Reopen {
		status = "open"
	}
	err := s.store.SetDriftFindingStatus(repo.Key(), branch, r.PathValue("fp"), status)
	if err == store.ErrNotFound {
		jsonError(w, http.StatusNotFound, "unknown finding")
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.publish("drift", repo.Key(), branch)
	jsonOK(w, map[string]string{"status": status})
}

// driftSources resolves the run's reference sources: every selected reference
// (tool-reachable, same set as the speccy chat), optionally narrowed by the
// drift.references list.
func (s *Server) driftSources(r *http.Request, repo *project.Project, branch string, driftCfg project.DriftConfig) []ai.GroundingSource {
	all, _ := s.resolveSources(r, repo, branch)
	if len(driftCfg.References) == 0 {
		return all
	}
	want := map[string]bool{}
	for _, n := range driftCfg.References {
		want[n] = true
	}
	var out []ai.GroundingSource
	for _, src := range all {
		if want[src.Name] {
			out = append(out, src)
		}
	}
	return out
}

// ---------------------------------------------------------------- worker

// driftWorker checks the scope one document per AI loop, persisting findings
// incrementally. Per-doc failures are recorded and the run continues; only
// cancellation stops it early.
func (s *Server) driftWorker(ctx context.Context, cancel context.CancelFunc, key string, runID int64,
	repo *project.Project, branch string, docs []string, files map[string]string,
	sources []ai.GroundingSource, instructions string) {
	defer cancel()
	defer s.drift.release(key)

	dropped := 0
	var docErrs []string
	status := "ok"
	for i, doc := range docs {
		if ctx.Err() != nil {
			status = "cancelled"
			break
		}
		findings, droppedDoc, err := s.driftCheckDoc(ctx, repo, branch, doc, files, sources, instructions)
		dropped += droppedDoc
		if err != nil {
			if ctx.Err() != nil {
				status = "cancelled"
				break
			}
			log.Printf("drift [%s@%s]: %s: %v", repo.ID, branch, doc, err)
			docErrs = append(docErrs, doc+": "+err.Error())
			continue
		}
		keep := make([]string, 0, len(findings))
		for _, f := range findings {
			keep = append(keep, f.Fingerprint)
			if err := s.store.UpsertDriftFinding(f); err != nil {
				log.Printf("drift [%s@%s]: persist %s: %v", repo.ID, branch, f.Fingerprint, err)
			}
		}
		// scope-aware reconciliation: only THIS doc's stale findings resolve
		if err := s.store.ResolveDriftFindingsExcept(repo.Key(), branch, doc, keep); err != nil {
			log.Printf("drift [%s@%s]: reconcile %s: %v", repo.ID, branch, doc, err)
		}
		_ = s.store.UpdateDriftRunProgress(runID, i+1, dropped)
		s.publish("drift", repo.Key(), branch)
	}
	errMsg := ""
	if len(docErrs) > 0 {
		if status == "ok" {
			status = "error"
		}
		errMsg = fmt.Sprintf("%d document(s) failed: %s", len(docErrs), strings.Join(docErrs, "; "))
		if len(errMsg) > 1000 {
			errMsg = errMsg[:1000] + "…"
		}
	}
	if err := s.store.FinishDriftRun(runID, status, errMsg); err != nil {
		log.Printf("drift [%s@%s]: finish run: %v", repo.ID, branch, err)
	}
	s.publish("drift", repo.Key(), branch)
}

// driftCheckDoc runs one document through the audit loop and returns its
// verified findings plus the count dropped by evidence verification.
func (s *Server) driftCheckDoc(ctx context.Context, repo *project.Project, branch, doc string,
	files map[string]string, sources []ai.GroundingSource, instructions string) ([]store.DriftFinding, int, error) {
	content, ok := files[doc]
	if !ok {
		return nil, 0, fmt.Errorf("not in snapshot")
	}
	tb := &speccyToolbox{repo: repo, branch: branch, writable: false, sources: sources,
		files: files, publish: func() {}}
	// read tools only — ask_user has no human to halt for in a background run
	var specs []ai.ToolSpec
	for _, spec := range tb.specs(files) {
		if spec.Name != "ask_user" {
			specs = append(specs, spec)
		}
	}
	names := make([]string, 0, len(sources))
	for _, src := range sources {
		names = append(names, src.Name)
	}
	sort.Strings(names)
	msgs := ai.DriftPrompt(doc, content, instructions, names)

	var reply strings.Builder
	_, _, err := s.ai.StreamTools(ctx, msgs, specs, tb.exec,
		func(delta string) error { reply.WriteString(delta); return nil },
		func(ai.ToolCall, string, error) error { return nil })
	if err != nil {
		return nil, 0, err
	}
	var out struct {
		Findings []modelFinding `json:"findings"`
	}
	if err := ai.ExtractJSON(reply.String(), &out); err != nil {
		return nil, 0, fmt.Errorf("model reply was not findings JSON: %w", err)
	}
	var findings []store.DriftFinding
	dropped := 0
	seen := map[string]bool{}
	for _, f := range out.Findings {
		if !verifyEvidence(f, sources) {
			dropped++
			continue
		}
		kind := normDriftKind(f.Kind)
		anchor := strings.TrimSpace(f.Anchor)
		fp := driftFingerprint(doc, f.Source, kind, anchor)
		if seen[fp] {
			continue
		}
		seen[fp] = true
		evidence, _ := json.Marshal(f.Evidence)
		findings = append(findings, store.DriftFinding{
			RepoKey: repo.Key(), Branch: branch, Fingerprint: fp, RunID: 0,
			DocPath: doc, Anchor: anchor, Source: f.Source, Kind: kind,
			Severity: normDriftSeverity(f.Severity), Title: strings.TrimSpace(f.Title),
			Detail: strings.TrimSpace(f.Detail), EvidenceJSON: string(evidence),
		})
	}
	return findings, dropped, nil
}

// ---------------------------------------------------------------- targets

// driftTarget is one resolved work-item destination.
type driftTarget struct {
	name     string
	kind     string // github | gitlab | jira
	project  string // display: forge path / Jira key ("" for the implicit target)
	implicit bool
	catalog  *config.TargetConfig // nil for implicit and forge-path targets
}

const implicitTargetName = "this-repo"

// driftTargets resolves the effective work-item targets for a project:
// implicit "this repo's forge" + (in-repo drift.targets ∩ server catalog).
// In forge-PAT mode a selection entry may also be an owner/repo path on the
// deployment's forge, filed with the caller's own PAT — never a new host.
func (s *Server) driftTargets(repo *project.Project, ref string) []driftTarget {
	var out []driftTarget
	fcfg := repo.Repo.Cfg.Forge
	if fcfg.Enabled() {
		out = append(out, driftTarget{
			name: implicitTargetName, kind: fcfg.Kind, project: fcfg.Project, implicit: true,
		})
	}
	cfg := inRepoConfig(repo, ref)
	if cfg == nil {
		return out
	}
	catalog := map[string]*config.TargetConfig{}
	for i := range s.cfg.WorkItemTargets {
		catalog[s.cfg.WorkItemTargets[i].Name] = &s.cfg.WorkItemTargets[i]
	}
	for _, name := range cfg.Drift.Targets {
		if t, ok := catalog[name]; ok {
			out = append(out, driftTarget{name: t.Name, kind: t.Kind, project: t.Project, catalog: t})
			continue
		}
		// forge-PAT mode: an owner/repo path on the deployment's forge host
		if s.patMode() && strings.Contains(name, "/") && !strings.ContainsAny(name, " \t") {
			out = append(out, driftTarget{name: name, kind: s.cfg.Auth.Forge.Kind, project: name})
		}
	}
	return out
}

// driftIssueClient builds the tracker client for a target. The returned
// funcs share one signature so the filing handler stays a plain switch.
func (s *Server) driftIssueClient(r *http.Request, repo *project.Project, t driftTarget) (
	find func(ctx context.Context, marker string) (string, error),
	create func(ctx context.Context, title, body string, labels []string) (string, error),
	err error) {

	if t.catalog != nil && t.catalog.Kind == "jira" {
		j := tracker.NewJira(t.catalog.BaseURL, t.catalog.Project, t.catalog.IssueType,
			os.Getenv(t.catalog.TokenEnv))
		return func(ctx context.Context, marker string) (string, error) {
				_, u, err := j.FindIssue(ctx, forge.DriftLabel, marker)
				return u, err
			}, func(ctx context.Context, title, body string, labels []string) (string, error) {
				_, u, err := j.CreateIssue(ctx, title, body, labels)
				return u, err
			}, nil
	}

	var client *forge.Client
	switch {
	case t.implicit:
		fcfg := repo.Repo.Cfg.Forge
		client, err = forge.New(fcfg, repo.Repo.Cfg.Remote, s.forgeToken(r, fcfg))
	case t.catalog != nil:
		fcfg := forge.Config{Kind: t.catalog.Kind,
			BaseURL: config.ForgeAPIBase(t.catalog.Kind, t.catalog.BaseURL),
			Project: t.catalog.Project, TokenEnv: t.catalog.TokenEnv}
		client, err = forge.New(fcfg, "", os.Getenv(t.catalog.TokenEnv))
	default: // forge-PAT owner/repo path: same host as the deployment forge
		fcfg := forge.Config{Kind: t.kind, BaseURL: repo.Repo.Cfg.Forge.BaseURL, Project: t.project}
		if fcfg.BaseURL == "" && s.cfg.Auth.Forge.BaseURL != "" {
			fcfg.BaseURL = config.ForgeAPIBase(t.kind, s.cfg.Auth.Forge.BaseURL)
		}
		client, err = forge.New(fcfg, "", s.tok(r))
	}
	if err != nil {
		return nil, nil, err
	}
	if client == nil {
		return nil, nil, fmt.Errorf("target %s has no forge configured", t.name)
	}
	return func(ctx context.Context, marker string) (string, error) {
			issue, err := client.FindIssueByMarker(ctx, marker)
			if err != nil || issue == nil {
				return "", err
			}
			return issue.URL, nil
		}, func(ctx context.Context, title, body string, labels []string) (string, error) {
			issue, err := client.CreateIssue(ctx, title, body, labels)
			if err != nil {
				return "", err
			}
			return issue.URL, nil
		}, nil
}

// ---------------------------------------------------------------- filing

// POST /api/repos/{repo}/drift/findings/{fp}/file {target} — create (or
// re-find) the work item and write the backlink into the doc's frontmatter.
func (s *Server) postDriftFile(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	var body struct {
		Target string `json:"target"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	finding, err := s.store.DriftFinding(repo.Key(), branch, r.PathValue("fp"))
	if err == store.ErrNotFound {
		jsonError(w, http.StatusNotFound, "unknown finding")
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	targets := s.driftTargets(repo, branch)
	if body.Target == "" && len(targets) == 1 {
		body.Target = targets[0].name
	}
	var target *driftTarget
	for i := range targets {
		if targets[i].name == body.Target {
			target = &targets[i]
			break
		}
	}
	if target == nil {
		// 404, not 400: an unselected target must be indistinguishable from a
		// nonexistent one — selection ∩ catalog, no minting
		jsonError(w, http.StatusNotFound, "unknown work-item target "+body.Target)
		return
	}
	find, create, err := s.driftIssueClient(r, repo, *target)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "target misconfigured: "+err.Error())
		return
	}

	marker := driftMarker(finding.Fingerprint)
	itemURL, err := find(r.Context(), marker)
	if err != nil {
		jsonError2(w, http.StatusBadGateway, "tracker lookup failed: "+err.Error(), "tracker_failed")
		return
	}
	created := false
	if itemURL == "" {
		labels := []string{forge.DriftLabel}
		if target.catalog != nil {
			labels = append(labels, target.catalog.Labels...)
		}
		itemURL, err = create(r.Context(), "[drift] "+finding.Title, s.driftIssueBody(repo, finding, marker), labels)
		if err != nil {
			jsonError2(w, http.StatusBadGateway, "create work item: "+err.Error(), "tracker_failed")
			return
		}
		created = true
	}
	if err := s.store.FileDriftFinding(repo.Key(), branch, finding.Fingerprint, itemURL, target.name); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	backlinked, backlinkBranch, backlinkErr := s.writeDriftBacklink(r, repo, branch, finding.DocPath, itemURL)
	out := map[string]any{
		"url": itemURL, "created": created, "target": target.name,
		"backlinked": backlinked,
	}
	if backlinked {
		out["backlinkBranch"] = backlinkBranch
	}
	if backlinkErr != "" {
		out["backlinkError"] = backlinkErr
	}
	s.publish("drift", repo.Key(), branch)
	jsonOK(w, out)
}

func driftMarker(fingerprint string) string {
	return "<!-- specquill:drift:" + fingerprint + " -->"
}

// driftIssueBody renders the work item: finding + evidence as quoted blocks
// (model output is data — never markup we vouch for) + the deep link back to
// the document + the idempotency marker.
func (s *Server) driftIssueBody(repo *project.Project, f *store.DriftFinding, marker string) string {
	var b strings.Builder
	b.WriteString(f.Detail + "\n\n")
	base := strings.TrimSuffix(s.cfg.BaseURL, "/")
	docURL := f.DocPath
	if base != "" {
		docURL = base + "/p/" + repo.ID + "/editor/" + f.DocPath
	}
	fmt.Fprintf(&b, "- Document: %s", docURL)
	if f.Anchor != "" {
		fmt.Fprintf(&b, " (%s)", f.Anchor)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "- Reference source: ~%s\n", f.Source)
	fmt.Fprintf(&b, "- Kind: %s · Severity: %s\n", f.Kind, f.Severity)
	var evidence []driftEvidence
	_ = json.Unmarshal([]byte(f.EvidenceJSON), &evidence)
	if len(evidence) > 0 {
		b.WriteString("\nEvidence:\n")
		for _, ev := range evidence {
			fmt.Fprintf(&b, "\n`%s`:\n\n```\n%s\n```\n", ev.Path, ev.Quote)
		}
	}
	b.WriteString("\n" + marker + "\n")
	return b.String()
}

// writeDriftBacklink appends the work-item URL to the doc's `work-items:`
// frontmatter list as a normal worktree save. Filing IS an edit: on a
// protected branch it routes to the caller's workspace branch (claiming it
// like the first edit would). Best-effort — the issue exists either way, so
// failures degrade to a backlinkError instead of failing the request.
func (s *Server) writeDriftBacklink(r *http.Request, repo *project.Project, branch, docPath, itemURL string) (ok bool, usedBranch, errMsg string) {
	writeBranch := branch
	if repo.Repo.Cfg.IsProtected(branch) {
		ws, err := s.claimWorkspace(r, repo)
		if err != nil {
			return false, "", "workspace branch: " + err.Error()
		}
		writeBranch = ws
	}
	content, sha, err := repo.File(writeBranch, docPath)
	if err != nil {
		return false, "", "read " + docPath + ": " + err.Error()
	}
	next, added, err := mdfm.AppendListItem(content, "work-items", itemURL)
	if err != nil {
		return false, "", err.Error()
	}
	if !added {
		return true, writeBranch, "" // already backlinked — idempotent
	}
	if next, err = mdfm.Touch(next, false, time.Now()); err != nil {
		return false, "", err.Error()
	}
	if _, err := repo.SaveFile(writeBranch, docPath, next, sha); err != nil {
		return false, "", err.Error()
	}
	s.publish("save", repo.Key(), writeBranch)
	return true, writeBranch, ""
}

// claimWorkspace resolves (or claims) the caller's personal workspace branch
// and ensures it exists — the same flow as POST /workspace.
func (s *Server) claimWorkspace(r *http.Request, repo *project.Project) (string, error) {
	u := auth.UserFrom(r.Context())
	branch, err := s.store.WorkspaceBranch(repo.Key(), u.ID)
	if err != nil {
		return "", err
	}
	if branch == "" {
		branch = "ws/" + workspaceSlug(u)
		if err := s.store.ClaimWorkspaceBranch(repo.Key(), branch, u.ID); err != nil {
			branch = branch + "-" + fmt.Sprint(u.ID)
			if err := s.store.ClaimWorkspaceBranch(repo.Key(), branch, u.ID); err != nil {
				return "", err
			}
		}
	}
	if _, err := repo.EnsureWorkspace(branch); err != nil {
		return "", err
	}
	return branch, nil
}
