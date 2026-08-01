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

// The git-native run report: a markdown document the worker rewrites as the
// run progresses, so alignment state lives IN the specs repository (committed
// like any doc) instead of only in the server's database. Reports are LIVING
// documents: the engine owns only the marker-delimited block — everything
// outside it (the human's conclusions, decisions, sign-offs) is preserved —
// and each run can target the project's standing report, continue any
// existing one, or start a fresh one (`report` on the run request).
//
// Every report path is PROJECT-relative and declared by the project itself
// (`drift.report:` in its .specquill/config.yml): in a monorepo each
// project's alignment docs land under its own content_root, beside the
// config that names them — never in a repo-wide location the server picked.
const (
	builtinDriftReportPath = "reports/source-alignment.md"
	reportBegin            = "<!-- specquill:alignment:begin — engine-maintained, edit OUTSIDE this block -->"
	reportEnd              = "<!-- specquill:alignment:end -->"
)

// driftReportPath is the project's standing alignment report: whatever its
// own .specquill/config.yml declares (project-relative), else the built-in
// default. An unusable configured value falls back rather than failing the
// run — the report is a convenience, never a gate.
func driftReportPath(cfg project.DriftConfig) string {
	if p := cleanDocPath(cfg.Report); p != "" && !okf.Reserved(base(p)) {
		return p
	}
	return builtinDriftReportPath
}

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

// modelFinding is the JSON shape the drift/gap prompts ask the model for.
type modelFinding struct {
	Anchor        string          `json:"anchor"`
	Source        string          `json:"source"`
	Kind          string          `json:"kind"`
	Severity      string          `json:"severity"`
	Title         string          `json:"title"`
	Detail        string          `json:"detail"`
	SuggestedPath string          `json:"suggestedPath"` // gaps: where the missing doc should live
	SourcePaths   []string        `json:"sourcePaths"`
	Evidence      []driftEvidence `json:"evidence"`
}

// cleanDocPath sanitizes a model-suggested workspace path: relative, .md,
// no traversal, no reference prefix. "" when unusable.
func cleanDocPath(p string) string {
	p = strings.TrimPrefix(strings.TrimSpace(p), "/")
	if p == "" || !strings.HasSuffix(p, ".md") || strings.HasPrefix(p, "~") ||
		strings.HasPrefix(p, ".") || strings.Contains(p, "..") || strings.ContainsAny(p, " \t\n\\") {
		return ""
	}
	return p
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
			!strings.HasPrefix(p, "uploads/") && !okf.Reserved(base(p)) &&
			// alignment reports (any of them) never audit themselves
			!strings.Contains(files[p], reportBegin)
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
	// the reference sources a gaps run would sweep — the scope picker shows them
	var driftCfg project.DriftConfig
	if cfg := inRepoConfig(repo, branch); cfg != nil {
		driftCfg = cfg.Drift
	}
	names := []string{}
	for _, src := range s.driftSources(r, repo, branch, driftCfg) {
		names = append(names, src.Name)
	}
	sort.Strings(names)
	out["sources"] = names
	// existing report docs (engine-marked or under reports/) — the run
	// dialog offers them for continuation, plus the standing default
	if files, err := repo.Snapshot(branch); err == nil {
		standing := driftReportPath(driftCfg)
		folder := standing[:strings.LastIndex(standing, "/")+1]
		reports := map[string]bool{standing: true}
		for p, content := range files {
			if strings.HasSuffix(p, ".md") && !okf.Reserved(base(p)) &&
				(strings.Contains(content, reportBegin) || (folder != "" && strings.HasPrefix(p, folder))) {
				reports[p] = true
			}
		}
		list := make([]string, 0, len(reports))
		for p := range reports {
			list = append(list, p)
		}
		sort.Strings(list)
		out["reports"] = list
	} else {
		out["reports"] = []string{driftReportPath(driftCfg)}
	}
	jsonOK(w, out)
}

func driftRunWire(run *store.DriftRun) map[string]any {
	var scope, activity []string
	_ = json.Unmarshal([]byte(run.ScopeJSON), &scope)
	_ = json.Unmarshal([]byte(run.ActivityJSON), &activity)
	return map[string]any{
		"id": run.ID, "mode": run.Mode, "status": run.Status, "error": run.Error, "scope": scope,
		"docsTotal": run.DocsTotal, "docsDone": run.DocsDone,
		"droppedUnverified": run.DroppedUnverified, "headSha": run.HeadSHA,
		"activity": activity, "reportPath": run.ReportPath, "reportBranch": run.ReportBranch,
		"startedAt": run.StartedAt, "finishedAt": run.FinishedAt,
	}
}

func driftFindingWire(f store.DriftFinding) map[string]any {
	var evidence []driftEvidence
	_ = json.Unmarshal([]byte(f.EvidenceJSON), &evidence)
	return map[string]any{
		"fingerprint": f.Fingerprint, "docPath": f.DocPath, "anchor": f.Anchor,
		"suggestedPath": f.SuggestedPath, "draftPath": f.DraftPath,
		"remedyPath": f.RemedyPath, "remedyKind": f.RemedyKind,
		"source": f.Source, "kind": f.Kind, "severity": f.Severity,
		"title": f.Title, "detail": f.Detail, "evidence": evidence,
		"status": f.Status, "workItemUrl": f.WorkItemURL, "workItemTarget": f.WorkItemTarget,
		"updatedAt": f.UpdatedAt,
	}
}

// POST /api/repos/{repo}/drift/run?branch= {mode?, paths?} — start an async
// run. mode "drift" (default) verifies each doc in scope against the sources;
// mode "gaps" sweeps each source for capabilities no document covers.
func (s *Server) postDriftRun(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "Speccy is not configured (ai: in specquill.yml)")
		return
	}
	var body struct {
		Mode   string   `json:"mode"`
		Paths  []string `json:"paths"`
		Report string   `json:"report"` // report doc to create/continue; default: the standing report
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // empty body = default scope
	if body.Mode == "" {
		body.Mode = "drift"
	}
	if body.Mode != "drift" && body.Mode != "gaps" {
		jsonError(w, http.StatusBadRequest, "mode must be drift or gaps")
		return
	}
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
	if body.Report == "" { // the project's own standing report
		body.Report = driftReportPath(driftCfg)
	}
	if cleanDocPath(body.Report) != body.Report || okf.Reserved(base(body.Report)) {
		jsonError(w, http.StatusBadRequest, "report must be a project-relative .md path")
		return
	}
	sources := s.driftSources(r, repo, branch, driftCfg)
	if len(sources) == 0 {
		jsonError(w, http.StatusUnprocessableEntity,
			"no reference sources selected — drift needs references in .specquill/config.yml")
		return
	}
	// units: the docs to verify (drift) or the sources to sweep (gaps)
	var units []string
	if body.Mode == "gaps" {
		for _, src := range sources {
			units = append(units, src.Name)
		}
		sort.Strings(units)
	} else {
		units = resolveDriftScope(files, body.Paths, driftCfg.Paths)
		// a pre-existing doc chosen as the report target leaves the scope
		// even before it carries the engine markers
		for i, u := range units {
			if u == body.Report {
				units = append(units[:i], units[i+1:]...)
				break
			}
		}
		if len(units) == 0 {
			jsonError(w, http.StatusUnprocessableEntity, "no documents in scope")
			return
		}
		// large scopes just loop — the worker is sequential, incremental and
		// cancellable. An EXPLICIT drift.max_docs remains a hard ceiling.
		if driftCfg.MaxDocs > 0 && len(units) > driftCfg.MaxDocs {
			jsonError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("scope resolves to %d documents (max_docs %d) — narrow the paths or raise drift.max_docs", len(units), driftCfg.MaxDocs))
			return
		}
	}

	key := driftKey(repo.Key(), branch)
	ctx, cancel := context.WithCancel(context.Background())
	if !s.drift.claim(key, cancel) {
		cancel()
		jsonError(w, http.StatusConflict, "a drift run is already in progress for this branch")
		return
	}
	// the in-repo report is written to the run branch, or the caller's
	// workspace branch when that one is protected. Best-effort: a run whose
	// report cannot land anywhere still runs (findings stay in the store).
	reportBranch := branch
	if repo.Repo.Cfg.IsProtected(branch) {
		if ws, err := s.claimWorkspace(r, repo); err == nil {
			reportBranch = ws
		} else {
			log.Printf("drift [%s@%s]: no report branch: %v", repo.ID, branch, err)
			reportBranch = ""
		}
	}
	reportPath := body.Report
	if reportBranch == "" {
		reportPath = ""
	}
	scopeJSON, _ := json.Marshal(units)
	headSHA, _ := repo.Repo.Head(branch)
	runID, err := s.store.CreateDriftRun(store.DriftRun{
		RepoKey: repo.Key(), Branch: branch, Mode: body.Mode, ScopeJSON: string(scopeJSON),
		DocsTotal: len(units), HeadSHA: headSHA,
		ReportPath: reportPath, ReportBranch: reportBranch,
	})
	if err != nil {
		s.drift.release(key)
		cancel()
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// the link graph rides along: each doc is audited WITH its linked
	// documents inlined, so the check sees the chain around it
	idx := buildLinkIndex(files, linkFieldNames(files))
	go s.driftWorker(ctx, cancel, key, runID, body.Mode, repo, branch, units, files, sources, idx, driftCfg.Instructions)
	jsonOK(w, map[string]any{"runId": runID, "docsTotal": len(units), "mode": body.Mode})
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
		// reopening treats the finding as fresh again — stale draft/remedy
		// pointers (their documents may have been discarded) are dropped
		// with the dismissal
		_ = s.store.SetDriftFindingDraft(repo.Key(), branch, r.PathValue("fp"), "")
		_ = s.store.SetDriftFindingRemedy(repo.Key(), branch, r.PathValue("fp"), "", "")
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
	s.refreshDriftReport(repo, branch)
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

// driftWorker checks the scope one unit (document / source) per AI loop,
// persisting findings incrementally. Per-unit failures are recorded and the
// run continues; only cancellation stops it early.
func (s *Server) driftWorker(ctx context.Context, cancel context.CancelFunc, key string, runID int64,
	mode string, repo *project.Project, branch string, units []string, files map[string]string,
	sources []ai.GroundingSource, idx *linkIndex, instructions string) {
	defer cancel()
	defer s.drift.release(key)

	dropped := 0
	var docErrs []string
	var activity []string
	status := "ok"
	label := func(unit string) string {
		if mode == "gaps" {
			return "~" + unit
		}
		return unit
	}
	note := func(line string) {
		activity = append(activity, time.Now().Format("15:04:05")+" "+line)
	}
	for i, unit := range units {
		if ctx.Err() != nil {
			status = "cancelled"
			break
		}
		var findings []store.DriftFinding
		var droppedDoc int
		var err error
		if mode == "gaps" {
			findings, droppedDoc, err = s.driftCheckGaps(ctx, repo, branch, unit, files, sources, instructions)
		} else {
			findings, droppedDoc, err = s.driftCheckDoc(ctx, repo, branch, unit, files, sources, idx, instructions)
		}
		dropped += droppedDoc
		if err != nil {
			if ctx.Err() != nil {
				status = "cancelled"
				break
			}
			log.Printf("drift [%s@%s]: %s: %v", repo.ID, branch, unit, err)
			docErrs = append(docErrs, unit+": "+err.Error())
			note("✗ " + label(unit) + " — " + err.Error())
		} else {
			keep := make([]string, 0, len(findings))
			for _, f := range findings {
				keep = append(keep, f.Fingerprint)
				if err := s.store.UpsertDriftFinding(f); err != nil {
					log.Printf("drift [%s@%s]: persist %s: %v", repo.ID, branch, f.Fingerprint, err)
				}
			}
			// scope-aware reconciliation: only THIS unit's stale findings resolve
			if mode == "gaps" {
				err = s.store.ResolveGapFindingsExcept(repo.Key(), branch, unit, keep)
			} else {
				err = s.store.ResolveDriftFindingsExcept(repo.Key(), branch, unit, keep)
			}
			if err != nil {
				log.Printf("drift [%s@%s]: reconcile %s: %v", repo.ID, branch, unit, err)
			}
			word := "finding"
			if mode == "gaps" {
				word = "gap"
			}
			if n := len(findings); n == 0 {
				note("✓ " + label(unit) + " — clean")
			} else {
				note(fmt.Sprintf("✓ %s — %d %s%s", label(unit), n, word, plural(n)))
			}
			if droppedDoc > 0 {
				note(fmt.Sprintf("  %d unverified finding%s dropped", droppedDoc, plural(droppedDoc)))
			}
		}
		actJSON, _ := json.Marshal(activity)
		_ = s.store.UpdateDriftRunProgress(runID, i+1, dropped, string(actJSON))
		// the in-repo report follows along — every completed unit updates it
		s.writeDriftReport(runID, repo, branch, false)
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
	if rb := s.writeDriftReport(runID, repo, branch, true); rb != "" {
		s.publish("save", repo.Key(), rb)
	}
	s.publish("drift", repo.Key(), branch)
}

// writeDriftReport refreshes the in-repo report from the current store state
// and saves it on the run's report branch as an uncommitted draft. Only the
// marker-delimited engine block is rewritten — the human's text around it
// survives. appendLog adds the run's summary line to the accumulated run log
// (once, when it finishes). Best-effort: report trouble never disturbs the
// run. Returns the branch written to ("" when the run has no report target).
func (s *Server) writeDriftReport(runID int64, repo *project.Project, branch string, appendLog bool) string {
	run, err := s.store.LatestDriftRun(repo.Key(), branch)
	if err != nil || run.ID != runID || run.ReportBranch == "" {
		return ""
	}
	findings, err := s.store.DriftFindings(repo.Key(), branch)
	if err != nil {
		return ""
	}
	existing, sha, _ := repo.File(run.ReportBranch, run.ReportPath)
	runLog := extractRunLog(existing)
	if appendLog {
		runLog = append(runLog, runLogLine(run, findings))
	}
	content := mergeDriftReport(existing, driftReportBlock(run, findings, runLog), run.ReportPath)
	if content, err = mdfm.Touch(content, false, time.Now()); err != nil {
		log.Printf("drift [%s@%s]: report: %v", repo.ID, branch, err)
		return ""
	}
	if _, err := repo.SaveFile(run.ReportBranch, run.ReportPath, content, sha); err != nil {
		log.Printf("drift [%s@%s]: report: %v", repo.ID, branch, err)
		return ""
	}
	return run.ReportBranch
}

// refreshDriftReport re-renders the last finished run's report after a
// finding changed outside a run (dismissed, filed, drafted) — the report
// tracks the workflow, not just the run. No-op while a run is in flight
// (the worker owns the report) or when no run/report exists.
func (s *Server) refreshDriftReport(repo *project.Project, branch string) {
	run, err := s.store.LatestDriftRun(repo.Key(), branch)
	if err != nil || run.Status == "running" || run.ReportBranch == "" {
		return
	}
	if rb := s.writeDriftReport(run.ID, repo, branch, false); rb != "" {
		s.publish("save", repo.Key(), rb)
	}
}

// runLogLine is one run's entry in the report's accumulated run log.
func runLogLine(run *store.DriftRun, findings []store.DriftFinding) string {
	unitNoun := "docs"
	if run.Mode == "gaps" {
		unitNoun = "sources"
	}
	open := 0
	for _, f := range findings {
		if f.Status != "dismissed" {
			open++
		}
	}
	line := fmt.Sprintf("- %s · %s · %d/%d %s · %s · %d finding%s live",
		time.Unix(run.StartedAt, 0).Format("2006-01-02 15:04"), run.Mode,
		run.DocsDone, run.DocsTotal, unitNoun, run.Status, open, plural(open))
	if run.DroppedUnverified > 0 {
		line += fmt.Sprintf(" · %d dropped", run.DroppedUnverified)
	}
	return line
}

// extractRunLog pulls the accumulated run-log lines out of an existing
// report's engine block (empty for a fresh or markerless document).
func extractRunLog(existing string) []string {
	start := strings.Index(existing, reportBegin)
	end := strings.Index(existing, reportEnd)
	if start < 0 || end < start {
		return nil
	}
	block := existing[start:end]
	i := strings.Index(block, "## Run log")
	if i < 0 {
		return nil
	}
	var out []string
	for _, line := range strings.Split(block[i:], "\n") {
		if strings.HasPrefix(line, "- ") {
			out = append(out, line)
		}
	}
	return out
}

// mergeDriftReport splices the engine block into the report document:
// between the markers of an existing report (the human's text around them is
// theirs), appended to a markerless document someone picked as a report
// target, or into a fresh scaffold when the file does not exist yet.
func mergeDriftReport(existing, block, reportPath string) string {
	wrapped := reportBegin + "\n\n" + block + "\n" + reportEnd
	if existing != "" {
		start := strings.Index(existing, reportBegin)
		end := strings.Index(existing, reportEnd)
		if start >= 0 && end > start {
			return existing[:start] + wrapped + existing[end+len(reportEnd):]
		}
		return strings.TrimRight(existing, "\n") + "\n\n" + wrapped + "\n"
	}
	name := strings.TrimSuffix(base(reportPath), ".md")
	words := strings.Fields(strings.ReplaceAll(strings.ReplaceAll(name, "-", " "), "_", " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	title := strings.Join(words, " ")
	return "---\ntitle: " + title + "\ntype: report\ngenerated_by: specquill drift engine\n---\n\n" +
		"# " + title + "\n\n" +
		"_The block below is engine-maintained and rewritten on every alignment run. " +
		"Everything outside it is yours — add conclusions, decisions and sign-offs, " +
		"and commit the report with your work; git history is the archive._\n\n" +
		wrapped + "\n"
}

// driftReportBlock renders the engine-maintained section of the report:
// run summary, the live findings, the activity log, and the accumulated
// run log. Everything around it belongs to the human.
func driftReportBlock(run *store.DriftRun, findings []store.DriftFinding, runLog []string) string {
	var scope, activity []string
	_ = json.Unmarshal([]byte(run.ScopeJSON), &scope)
	_ = json.Unmarshal([]byte(run.ActivityJSON), &activity)

	var b strings.Builder
	mode := "drift check (documents verified against the reference sources)"
	unitNoun := "documents"
	if run.Mode == "gaps" {
		mode = "gap analysis (reference sources swept for uncovered capabilities)"
		unitNoun = "sources"
	}
	status := run.Status
	if status == "running" {
		status = fmt.Sprintf("running — %d/%d %s checked", run.DocsDone, run.DocsTotal, unitNoun)
	}
	b.WriteString("## Last run\n\n")
	fmt.Fprintf(&b, "- Mode: %s\n", mode)
	fmt.Fprintf(&b, "- Branch: `%s`", run.Branch)
	if run.HeadSHA != "" {
		fmt.Fprintf(&b, " @ `%.10s`", run.HeadSHA)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "- Started: %s\n", time.Unix(run.StartedAt, 0).Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "- Status: %s\n", status)
	fmt.Fprintf(&b, "- Scope: %d %s\n", len(scope), unitNoun)
	if run.DroppedUnverified > 0 {
		fmt.Fprintf(&b, "- Dropped: %d finding(s) whose evidence did not verify\n", run.DroppedUnverified)
	}
	if run.Error != "" {
		fmt.Fprintf(&b, "- Errors: %s\n", run.Error)
	}

	open, dismissed := []store.DriftFinding{}, 0
	for _, f := range findings {
		if f.Status == "dismissed" {
			dismissed++
			continue
		}
		open = append(open, f)
	}
	sort.SliceStable(open, func(i, j int) bool {
		rank := map[string]int{"high": 0, "medium": 1, "low": 2}
		return rank[open[i].Severity] < rank[open[j].Severity]
	})
	fmt.Fprintf(&b, "\n## Findings (%d open", len(open))
	if dismissed > 0 {
		fmt.Fprintf(&b, ", %d dismissed", dismissed)
	}
	b.WriteString(")\n\n")
	if len(open) == 0 {
		b.WriteString("Nothing open — the workspace and its reference sources agree.\n")
	} else {
		b.WriteString("| Severity | Kind | Where | Finding | Status |\n|---|---|---|---|---|\n")
		for _, f := range open {
			where := f.DocPath
			if f.Anchor != "" && f.DocPath != "" {
				where += " · " + f.Anchor
			}
			if f.DocPath == "" { // coverage gap: anchored on the source
				where = "~" + f.Source + "/" + f.Anchor
				if f.DraftPath != "" {
					where += " → " + f.DraftPath
				} else if f.SuggestedPath != "" {
					where += " → (suggested) " + f.SuggestedPath
				}
			} else {
				where += " vs ~" + f.Source
			}
			st := f.Status
			if f.Status == "filed" && f.WorkItemURL != "" {
				st = "filed: " + f.WorkItemURL
			}
			if f.RemedyPath != "" {
				st += " · " + strings.ReplaceAll(f.RemedyKind, "_", " ") + ": " + f.RemedyPath
			}
			cell := func(v string) string { return strings.ReplaceAll(v, "|", "\\|") }
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
				f.Severity, f.Kind, cell(where), cell(f.Title), cell(st))
		}
	}

	if len(activity) > 0 {
		b.WriteString("\n## Run activity\n\n```\n")
		for _, line := range activity {
			b.WriteString(line + "\n")
		}
		b.WriteString("```\n")
	}
	if len(runLog) > 0 {
		b.WriteString("\n## Run log\n\n")
		for _, line := range runLog {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// driftCheckDoc runs one document through the audit loop and returns its
// verified findings plus the count dropped by evidence verification. The
// doc's linked documents ride along as context (idx may be nil).
func (s *Server) driftCheckDoc(ctx context.Context, repo *project.Project, branch, doc string,
	files map[string]string, sources []ai.GroundingSource, idx *linkIndex, instructions string) ([]store.DriftFinding, int, error) {
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
	linked := ""
	if idx != nil {
		linked = idx.linkedBlock(files, doc)
	}
	msgs := ai.DriftPrompt(doc, content, linked, instructions, names)

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

// driftCheckGaps sweeps one reference source for capabilities no workspace
// document covers (gap analysis). Findings are anchored to the SOURCE and
// carry no doc_path — reverse engineering (postDriftDraft) creates the
// missing document from them.
func (s *Server) driftCheckGaps(ctx context.Context, repo *project.Project, branch, sourceName string,
	files map[string]string, sources []ai.GroundingSource, instructions string) ([]store.DriftFinding, int, error) {
	var src *ai.GroundingSource
	for i := range sources {
		if sources[i].Name == sourceName {
			src = &sources[i]
			break
		}
	}
	if src == nil {
		return nil, 0, fmt.Errorf("unknown source")
	}
	tb := &speccyToolbox{repo: repo, branch: branch, writable: false, sources: sources,
		files: files, publish: func() {}}
	var specs []ai.ToolSpec
	for _, spec := range tb.specs(files) {
		if spec.Name != "ask_user" {
			specs = append(specs, spec)
		}
	}
	docs := resolveDriftScope(files, nil, nil)
	msgs := ai.GapPrompt(sourceName, strings.Join(docs, "\n"), instructions)

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
	droppedCount := 0
	seen := map[string]bool{}
	for _, f := range out.Findings {
		f.Source = sourceName // the sweep's source, whatever the model claims
		anchor := strings.TrimSpace(f.Anchor)
		if anchor == "" || !verifyEvidence(f, sources) {
			droppedCount++
			continue
		}
		fp := driftFingerprint("", sourceName, "coverage-gap", anchor)
		if seen[fp] {
			continue
		}
		seen[fp] = true
		evidence, _ := json.Marshal(f.Evidence)
		findings = append(findings, store.DriftFinding{
			RepoKey: repo.Key(), Branch: branch, Fingerprint: fp,
			DocPath: "", SuggestedPath: cleanDocPath(f.SuggestedPath),
			Anchor: anchor, Source: sourceName, Kind: "coverage-gap",
			Severity: normDriftSeverity(f.Severity), Title: strings.TrimSpace(f.Title),
			Detail: strings.TrimSpace(f.Detail), EvidenceJSON: string(evidence),
		})
	}
	return findings, droppedCount, nil
}

// ---------------------------------------------------------------- reverse engineering

// POST /api/repos/{repo}/drift/findings/{fp}/draft — reverse-engineer the
// MISSING requirement document from a coverage-gap finding: the AI drafts it
// from the finding's source evidence, following the workspace conventions,
// and it lands as a normal uncommitted worktree save the human reviews.
func (s *Server) postDriftDraft(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "Speccy is not configured (ai: in specquill.yml)")
		return
	}
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	finding, err := s.store.DriftFinding(repo.Key(), branch, r.PathValue("fp"))
	if err == store.ErrNotFound {
		jsonError(w, http.StatusNotFound, "unknown finding")
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if finding.DocPath != "" {
		jsonError(w, http.StatusBadRequest, "only coverage-gap findings can be drafted — this one already has a document")
		return
	}
	// idempotent: an existing draft is returned, a discarded one is redrafted
	if finding.DraftPath != "" {
		probe := branch
		if repo.Repo.Cfg.IsProtected(branch) {
			if ws, err := s.store.WorkspaceBranch(repo.Key(), auth.UserFrom(r.Context()).ID); err == nil && ws != "" {
				probe = ws
			}
		}
		if _, _, err := repo.File(probe, finding.DraftPath); err == nil {
			jsonOK(w, map[string]any{"path": finding.DraftPath, "branch": probe, "existing": true})
			return
		}
	}
	files, err := repo.Snapshot(branch)
	if err != nil {
		gitFail(w, err)
		return
	}
	var driftCfg project.DriftConfig
	instructions := ""
	if cfg := inRepoConfig(repo, branch); cfg != nil {
		driftCfg = cfg.Drift
		instructions = cfg.Speccy.Instructions
	}
	sources := s.driftSources(r, repo, branch, driftCfg)

	// source material: the full content of every quoted evidence file
	var evidence []driftEvidence
	_ = json.Unmarshal([]byte(finding.EvidenceJSON), &evidence)
	var excerpts strings.Builder
	for i := range sources {
		if sources[i].Name != finding.Source {
			continue
		}
		seen := map[string]bool{}
		for _, ev := range evidence {
			path := strings.TrimPrefix(ev.Path, "~"+finding.Source+"/")
			if seen[path] {
				continue
			}
			seen[path] = true
			if content, ok := sources[i].Files[path]; ok {
				if len(content) > 12*1024 {
					content = content[:12*1024] + "\n… (truncated)"
				}
				fmt.Fprintf(&excerpts, "\n## ~%s/%s\n```\n%s\n```\n", finding.Source, path, content)
			}
		}
	}
	if excerpts.Len() == 0 {
		jsonError(w, http.StatusBadGateway, "the finding's evidence files are no longer readable — re-run the gap analysis")
		return
	}
	findingJSON, _ := json.Marshal(map[string]any{
		"anchor": finding.Anchor, "source": finding.Source, "title": finding.Title,
		"detail": finding.Detail, "suggestedPath": finding.SuggestedPath, "evidence": evidence,
	})
	guidance := modelRules(files) + workspaceVocabulary(files) + ai.AuthoringRules(files, instructions)
	reply, err := s.ai.Complete(r.Context(), ai.ReversePrompt(string(findingJSON), excerpts.String(), guidance))
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	var draft struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := ai.ExtractJSON(reply, &draft); err != nil {
		jsonError(w, http.StatusBadGateway, "model reply was not draft JSON: "+err.Error())
		return
	}
	path := cleanDocPath(draft.Path)
	if path == "" || okf.Reserved(base(path)) {
		jsonError(w, http.StatusBadGateway, "model suggested an invalid document path: "+draft.Path)
		return
	}
	if err := mdfm.Validate(draft.Content); err != nil {
		jsonError(w, http.StatusBadGateway, "model draft has broken frontmatter: "+err.Error())
		return
	}
	content, err := mdfm.Touch(draft.Content, true, time.Now())
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}

	// drafting IS an edit: protected branches route to the caller's workspace
	writeBranch := branch
	if repo.Repo.Cfg.IsProtected(branch) {
		if writeBranch, err = s.claimWorkspace(r, repo); err != nil {
			gitFail(w, err)
			return
		}
	}
	if _, _, err := repo.File(writeBranch, path); err == nil {
		jsonError(w, http.StatusConflict, path+" already exists on "+writeBranch)
		return
	}
	if _, err := repo.SaveFile(writeBranch, path, content, ""); err != nil {
		gitFail(w, err)
		return
	}
	if err := s.store.SetDriftFindingDraft(repo.Key(), branch, finding.Fingerprint, path); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.refreshDriftReport(repo, branch)
	s.publish("save", repo.Key(), writeBranch)
	s.publish("drift", repo.Key(), branch)
	jsonOK(w, map[string]any{"path": path, "branch": writeBranch})
}

// POST /api/repos/{repo}/drift/findings/{fp}/remedy {kind} — create the
// in-repo document that tracks fixing a finding: a change record (WHY) or a
// work item (WHEN), drafted by the AI in the workspace's own conventions,
// linked to the affected document with the configured typed link, and saved
// as an uncommitted worktree draft.
func (s *Server) postDriftRemedy(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "Speccy is not configured (ai: in specquill.yml)")
		return
	}
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	var body struct {
		Kind string `json:"kind"` // change | work_item
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Kind == "" {
		body.Kind = "work_item"
	}
	if body.Kind != "change" && body.Kind != "work_item" {
		jsonError(w, http.StatusBadRequest, "kind must be change or work_item")
		return
	}
	finding, err := s.store.DriftFinding(repo.Key(), branch, r.PathValue("fp"))
	if err == store.ErrNotFound {
		jsonError(w, http.StatusNotFound, "unknown finding")
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	files, err := repo.Snapshot(branch)
	if err != nil {
		gitFail(w, err)
		return
	}
	entities, links := workspaceModel(files)
	entity, ok := entities[body.Kind]
	if !ok {
		jsonError(w, http.StatusUnprocessableEntity,
			"this workspace has no "+body.Kind+" family configured (.specquill/config.yml entities:)")
		return
	}

	// idempotent: an existing remedy doc is returned rather than re-drafted
	writeBranch := branch
	if repo.Repo.Cfg.IsProtected(branch) {
		if writeBranch, err = s.claimWorkspace(r, repo); err != nil {
			gitFail(w, err)
			return
		}
	}
	if finding.RemedyPath != "" {
		if _, _, err := repo.File(writeBranch, finding.RemedyPath); err == nil {
			jsonOK(w, map[string]any{"path": finding.RemedyPath, "kind": finding.RemedyKind,
				"branch": writeBranch, "existing": true})
			return
		}
	}

	// the affected document: the drifted doc, or a gap's reverse-engineered
	// draft when one exists (a bare gap has no document yet)
	targetPath := finding.DocPath
	if targetPath == "" {
		targetPath = finding.DraftPath
	}
	targetKind, targetExcerpt := "", ""
	if targetPath != "" {
		if content, _, err := repo.File(writeBranch, targetPath); err == nil {
			targetKind = docKind(targetPath, content, entities)
			if len(content) > 6*1024 {
				content = content[:6*1024] + "\n… (truncated)"
			}
			targetExcerpt = "## " + targetPath + "\n```\n" + content + "\n```\n"
		}
	}
	// the typed link connecting the new document and the affected one — the
	// model's own rule decides which side carries it
	linkField, linkOnRemedy := "", false
	if targetKind != "" {
		linkField, linkOnRemedy = linkBetween(links, body.Kind, targetKind)
	}
	linkNote := ""
	switch {
	case linkField != "" && linkOnRemedy:
		linkNote = "The server adds `" + linkField + ": [" + targetPath + "]` to the new document — do not write it yourself."
	case linkField != "":
		linkNote = "The server adds `" + linkField + ": [<this document>]` to " + targetPath + " — do not write link fields yourself."
	}
	// an existing document of the same family teaches the real conventions
	example := ""
	for _, p := range sortedKeys(files) {
		if p != targetPath && strings.HasPrefix(p, entity.Folder) && strings.HasSuffix(p, ".md") &&
			!okf.Reserved(base(p)) {
			content := files[p]
			if len(content) > 4*1024 {
				content = content[:4*1024] + "\n… (truncated)"
			}
			example = "## " + p + "\n```\n" + content + "\n```\n"
			break
		}
	}

	var evidence []driftEvidence
	_ = json.Unmarshal([]byte(finding.EvidenceJSON), &evidence)
	findingJSON, _ := json.Marshal(map[string]any{
		"kind": finding.Kind, "severity": finding.Severity, "title": finding.Title,
		"detail": finding.Detail, "document": finding.DocPath, "anchor": finding.Anchor,
		"source": "~" + finding.Source, "evidence": evidence,
	})
	instructions := ""
	if cfg := inRepoConfig(repo, branch); cfg != nil {
		instructions = cfg.Speccy.Instructions
	}
	kindLabel := strings.ReplaceAll(body.Kind, "_", " ")
	guidance := modelRules(files) + workspaceVocabulary(files) + ai.AuthoringRules(files, instructions)
	reply, err := s.ai.Complete(r.Context(), ai.RemedyPrompt(kindLabel, entity.Folder, linkNote,
		string(findingJSON), targetExcerpt, example, guidance))
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	var draft struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := ai.ExtractJSON(reply, &draft); err != nil {
		jsonError(w, http.StatusBadGateway, "model reply was not draft JSON: "+err.Error())
		return
	}
	path := cleanDocPath(draft.Path)
	if path == "" || okf.Reserved(base(path)) {
		jsonError(w, http.StatusBadGateway, "model suggested an invalid document path: "+draft.Path)
		return
	}
	if !strings.HasPrefix(path, entity.Folder) { // keep the family's folder authoritative
		path = entity.Folder + base(path)
	}
	if err := mdfm.Validate(draft.Content); err != nil {
		jsonError(w, http.StatusBadGateway, "model draft has broken frontmatter: "+err.Error())
		return
	}
	content, err := mdfm.Touch(draft.Content, true, time.Now())
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	if _, _, err := repo.File(writeBranch, path); err == nil {
		jsonError(w, http.StatusConflict, path+" already exists on "+writeBranch)
		return
	}
	// the typed link is written server-side, never trusted to the model
	linked := ""
	if linkField != "" && linkOnRemedy {
		if next, added, err := mdfm.AppendListItem(content, linkField, targetPath); err == nil && added {
			content, linked = next, linkField+" → "+targetPath
		}
	}
	if _, err := repo.SaveFile(writeBranch, path, content, ""); err != nil {
		gitFail(w, err)
		return
	}
	if linkField != "" && !linkOnRemedy { // the affected document points up at it
		if tc, sha, err := repo.File(writeBranch, targetPath); err == nil {
			if next, added, err := mdfm.AppendListItem(tc, linkField, path); err == nil && added {
				if next, err := mdfm.Touch(next, false, time.Now()); err == nil {
					if _, err := repo.SaveFile(writeBranch, targetPath, next, sha); err == nil {
						linked = targetPath + " " + linkField + " → " + path
					}
				}
			}
		}
	}
	if err := s.store.SetDriftFindingRemedy(repo.Key(), branch, finding.Fingerprint, path, body.Kind); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.refreshDriftReport(repo, branch)
	s.publish("save", repo.Key(), writeBranch)
	s.publish("drift", repo.Key(), branch)
	jsonOK(w, map[string]any{"path": path, "kind": body.Kind, "branch": writeBranch, "linked": linked})
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

	// backlink target: the drifted doc, or the reverse-engineered draft for a
	// coverage gap; a gap without a draft has no document to backlink yet
	backlinkDoc := finding.DocPath
	if backlinkDoc == "" {
		backlinkDoc = finding.DraftPath
	}
	backlinked, backlinkBranch, backlinkErr := false, "", ""
	if backlinkDoc != "" {
		backlinked, backlinkBranch, backlinkErr = s.writeDriftBacklink(r, repo, branch, backlinkDoc, itemURL)
	}
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
	s.refreshDriftReport(repo, branch)
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
	link := func(p string) string {
		if base != "" {
			return base + "/p/" + repo.ID + "/editor/" + p
		}
		return p
	}
	switch {
	case f.DocPath != "":
		fmt.Fprintf(&b, "- Document: %s", link(f.DocPath))
		if f.Anchor != "" {
			fmt.Fprintf(&b, " (%s)", f.Anchor)
		}
		b.WriteString("\n")
	default: // coverage gap: nothing covers it yet
		fmt.Fprintf(&b, "- Coverage gap at: %s\n", f.Anchor)
		if f.DraftPath != "" {
			fmt.Fprintf(&b, "- Draft requirement: %s\n", link(f.DraftPath))
		} else if f.SuggestedPath != "" {
			fmt.Fprintf(&b, "- Suggested document: %s\n", f.SuggestedPath)
		}
	}
	if f.RemedyPath != "" {
		fmt.Fprintf(&b, "- %s: %s\n", strings.ReplaceAll(f.RemedyKind, "_", " "), link(f.RemedyPath))
	}
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
