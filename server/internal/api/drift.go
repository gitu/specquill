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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

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
	// dated by default: each day's alignment work gets its own report, and
	// runs within a day continue the same one. A project that wants a single
	// standing report configures a path without a {date} token.
	defaultDriftReportPath = "reports/alignment-{date}.md"
	reportBegin            = "<!-- specquill:alignment:begin — engine-maintained, edit OUTSIDE this block -->"
	reportEnd              = "<!-- specquill:alignment:end -->"
	// the extraction inventory: what the application itself requires, grouped
	// by capability. Persisted BESIDE the alignment report, same living-document
	// contract — the engine owns only the marked block.
	extractionBegin = "<!-- specquill:extraction:begin — engine-maintained, edit OUTSIDE this block -->"
	extractionEnd   = "<!-- specquill:extraction:end -->"
)

// divide-and-conquer bounds: areas surveyed per source, and how many
// extracted requirements are matched against the specs per AI call.
const (
	maxExtractAreas = 12
	matchBatchSize  = 8
)

// extractionPath is where a source's extracted requirements live: beside the
// project's alignment report, named after the source.
func extractionPath(reportPath, source string) string {
	folder := reportPath[:strings.LastIndex(reportPath, "/")+1]
	return folder + "extracted-" + source + ".md"
}

// toolNote renders one model tool call as an activity line — the run's
// "what is it actually doing" signal (which sources it read, what it
// searched for), not just its verdict.
func toolNote(tc ai.ToolCall, execErr error) string {
	var a struct{ Path, Source, Query string }
	_ = json.Unmarshal([]byte(tc.Function.Arguments), &a)
	what := ""
	switch tc.Function.Name {
	case "read_file":
		what = "read " + a.Path
	case "search":
		what = "search " + strconv.Quote(a.Query)
		if a.Source != "" {
			what += " in ~" + strings.TrimPrefix(a.Source, "~")
		}
	case "list_files":
		what = "list " + strings.TrimPrefix("~"+a.Source, "~~")
		if a.Source == "" {
			what = "list workspace"
		}
	default:
		what = tc.Function.Name
	}
	if execErr != nil {
		return "    · " + what + " — " + execErr.Error()
	}
	return "    · " + what
}

// resolveReportTokens expands the date tokens a report path may carry, so a
// configured pattern names a different document as time moves on. UTC, like
// every other date this writes into the repo: which report a run continues
// must not depend on the server's timezone.
func resolveReportTokens(p string, now time.Time) string {
	now = now.UTC()
	r := strings.NewReplacer(
		"{date}", now.Format("2006-01-02"),
		"{yyyy}", now.Format("2006"),
		"{mm}", now.Format("01"),
		"{dd}", now.Format("02"),
	)
	return r.Replace(p)
}

// driftReportPath is the project's current alignment report: whatever its own
// .specquill/config.yml declares (project-relative, date tokens expanded),
// else the built-in dated default. An unusable configured value falls back
// rather than failing the run — the report is a convenience, never a gate.
func driftReportPath(cfg project.DriftConfig) string {
	if p := cleanDocPath(resolveReportTokens(cfg.Report, time.Now())); p != "" && !okf.Reserved(base(p)) {
		return p
	}
	return resolveReportTokens(defaultDriftReportPath, time.Now())
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
	// the source mandates something no requirement states yet — the finding
	// proposes a NEW document (suggestedPath) and is draftable like a gap
	"new-requirement": true,
	"coverage-gap":    true,
}

// draftableKinds are the findings whose remedy is a NEW document, so the
// reverse-engineer action applies to them.
func draftableKind(k string) bool { return k == "coverage-gap" || k == "new-requirement" }

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
			// engine-owned documents (reports, extractions) are never audited
			!strings.Contains(files[p], reportBegin) &&
			!strings.Contains(files[p], extractionBegin)
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
	// the page shows ONE run: the newest by default, or the one asked for.
	// A selection that no longer exists here (another branch, a reset store)
	// degrades to the newest rather than breaking the page.
	latest, latestErr := s.store.LatestDriftRun(repo.Key(), branch)
	sel, picked := latest, false
	if q := r.URL.Query().Get("run"); q != "" {
		if id, err := strconv.ParseInt(q, 10, 64); err == nil {
			if run, err := s.store.DriftRunByID(id); err == nil &&
				run.RepoKey == repo.Key() && run.Branch == branch {
				sel, picked = run, true
			}
		}
	}
	if latestErr == nil || picked {
		out["run"] = driftRunWire(sel)
	} else {
		out["run"] = nil
		sel = nil
	}
	// a run in flight is always the newest one (one worker per repo+branch) —
	// the client polls on this even while looking at an older run
	out["activeRunId"] = int64(0)
	if latestErr == nil && latest.Status == "running" {
		out["activeRunId"] = latest.ID
	}
	runs, err := s.store.ListDriftRuns(repo.Key(), branch, 30)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	counts, err := s.store.DriftFindingCountsByRun(repo.Key(), branch)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	wireRuns := make([]map[string]any, 0, len(runs))
	for i := range runs {
		wireRuns = append(wireRuns, driftRunSummaryWire(&runs[i], counts[runs[i].ID]))
	}
	out["runs"] = wireRuns
	findings, err := s.store.DriftFindings(repo.Key(), branch)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	wireFindings := make([]map[string]any, 0, len(findings))
	for _, f := range findings {
		// the default view keeps every live finding — a scoped run never
		// resolved the others. Asking for ONE run narrows to what it found.
		if picked && f.RunID != sel.ID {
			continue
		}
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
	// the project's standing report — stated EXPLICITLY so the client never
	// has to guess it (a hardcoded client fallback would override the
	// project's own drift.report on a first run)
	out["defaultReport"] = driftReportPath(driftCfg)
	// existing report docs (engine-marked or under the standing report's
	// folder) — the run dialog offers them for continuation
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
		// the analyzed application inventories: what the shown run recorded
		// (it knows the branch it wrote them to — possibly the caller's
		// workspace) plus any already present on this branch
		extractions := []map[string]any{}
		seen := map[string]bool{}
		if run := sel; run != nil && run.ExtractionsJSON != "" {
			var rec []struct{ Source, Path string }
			if json.Unmarshal([]byte(run.ExtractionsJSON), &rec) == nil {
				for _, e := range rec {
					if e.Path != "" && !seen[e.Path] {
						seen[e.Path] = true
						extractions = append(extractions, map[string]any{"source": e.Source, "path": e.Path})
					}
				}
			}
		}
		for _, name := range names {
			path := extractionPath(standing, name)
			if _, ok := files[path]; ok && !seen[path] {
				seen[path] = true
				extractions = append(extractions, map[string]any{"source": name, "path": path})
			}
		}
		out["extractions"] = extractions
	} else {
		out["reports"] = []string{driftReportPath(driftCfg)}
		out["extractions"] = []map[string]any{}
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
		"focus": run.Focus, "resumedFrom": run.ResumedFrom, "resumable": run.Resumable(),
		"startedAt": run.StartedAt, "finishedAt": run.FinishedAt,
	}
}

// driftRunSummaryWire is one row of the run picker: what a run was, how far it
// got and what it left behind — without its scope or activity, which only the
// run being looked at needs.
func driftRunSummaryWire(run *store.DriftRun, findings int) map[string]any {
	var sources []string
	_ = json.Unmarshal([]byte(run.SourcesJSON), &sources)
	if sources == nil {
		sources = []string{}
	}
	return map[string]any{
		"id": run.ID, "mode": run.Mode, "status": run.Status, "error": run.Error,
		"docsTotal": run.DocsTotal, "docsDone": run.DocsDone,
		"droppedUnverified": run.DroppedUnverified, "sources": sources, "focus": run.Focus,
		"reportPath": run.ReportPath, "reportBranch": run.ReportBranch,
		"resumedFrom": run.ResumedFrom, "resumable": run.Resumable(),
		"startedAt": run.StartedAt, "finishedAt": run.FinishedAt, "findings": findings,
	}
}

func driftFindingWire(f store.DriftFinding) map[string]any {
	var evidence []driftEvidence
	_ = json.Unmarshal([]byte(f.EvidenceJSON), &evidence)
	return map[string]any{
		"fingerprint": f.Fingerprint, "runId": f.RunID, "docPath": f.DocPath, "anchor": f.Anchor,
		"suggestedPath": f.SuggestedPath, "draftPath": f.DraftPath,
		"remedyPath": f.RemedyPath, "remedyKind": f.RemedyKind,
		"documents": func() []map[string]string {
			var d []map[string]string
			_ = json.Unmarshal([]byte(f.DocumentsJSON), &d)
			if d == nil {
				d = []map[string]string{}
			}
			return d
		}(),
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
		Mode  string   `json:"mode"`
		Paths []string `json:"paths"`
		// Sources restricts which references this run touches (default: all
		// selected); Focus aims the run at one area — a gaps sweep, an
		// extraction and a drift check all narrow to it.
		Sources []string `json:"sources"`
		Focus   string   `json:"focus"`
		Report  string   `json:"report"` // report doc to create/continue; default: the standing report
		// Resume picks up a run that stopped with units left (the server
		// restarted, the user cancelled it): everything it was configured
		// with is inherited and only its unchecked units are run.
		Resume int64 `json:"resume"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // empty body = default scope

	branchQ := repo.ResolveRef(r.URL.Query().Get("branch"))
	var prior *store.DriftRun
	if body.Resume > 0 {
		var err error
		if prior, err = s.store.DriftRunByID(body.Resume); err != nil {
			jsonError(w, http.StatusNotFound, "no such run")
			return
		}
		if prior.RepoKey != repo.Key() || prior.Branch != branchQ {
			jsonError(w, http.StatusBadRequest, "that run belongs to another project or branch")
			return
		}
		if !prior.Resumable() {
			jsonError(w, http.StatusConflict, "that run has nothing left to pick up")
			return
		}
		if by, err := s.store.DriftRunResumedBy(prior.ID); err == nil && by > 0 {
			jsonError(w, http.StatusConflict,
				fmt.Sprintf("run %d already picked that one up — continue from run %d instead", by, by))
			return
		}
		// the run is picked up AS IT WAS configured — mode, sources, focus and
		// the report it was writing
		body.Mode, body.Focus = prior.Mode, prior.Focus
		if body.Report == "" {
			body.Report = prior.ReportPath
		}
		if len(body.Sources) == 0 {
			_ = json.Unmarshal([]byte(prior.SourcesJSON), &body.Sources)
		}
	}
	if body.Mode == "" {
		body.Mode = "drift"
	}
	if body.Mode != "drift" && body.Mode != "gaps" && body.Mode != "extract" {
		jsonError(w, http.StatusBadRequest, "mode must be drift, gaps or extract")
		return
	}
	branch := branchQ
	files, err := repo.Snapshot(branch)
	if err != nil {
		gitFail(w, err)
		return
	}
	var driftCfg project.DriftConfig
	if cfg := inRepoConfig(repo, branch); cfg != nil {
		driftCfg = cfg.Drift
	}
	if body.Report == "" { // the project's own current report
		body.Report = driftReportPath(driftCfg)
	} else {
		body.Report = resolveReportTokens(body.Report, time.Now())
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
	if len(body.Sources) > 0 { // restrict this run to the named references
		want := map[string]bool{}
		for _, n := range body.Sources {
			want[strings.TrimPrefix(strings.TrimSpace(n), "~")] = true
		}
		kept := sources[:0:0]
		for _, src := range sources {
			if want[src.Name] {
				kept = append(kept, src)
			}
		}
		if len(kept) == 0 {
			jsonError(w, http.StatusUnprocessableEntity,
				"none of the requested sources is selected by this project")
			return
		}
		sources = kept
	}
	body.Focus = strings.TrimSpace(body.Focus)
	if len(body.Focus) > 200 {
		body.Focus = body.Focus[:200]
	}
	// units: the docs to verify (drift) or the sources to sweep (gaps)
	var units []string
	if body.Mode == "gaps" || body.Mode == "extract" {
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

	// resume: only the units the prior run never reached. The worker is
	// sequential, so scope[DocsDone:] is exactly what is left — minus anything
	// that has since disappeared from the workspace or the selection.
	var resumedFrom int64
	if prior != nil {
		var priorScope []string
		_ = json.Unmarshal([]byte(prior.ScopeJSON), &priorScope)
		if prior.DocsDone < len(priorScope) {
			priorScope = priorScope[prior.DocsDone:]
		} else {
			priorScope = nil
		}
		still := map[string]bool{}
		for _, u := range units {
			still[u] = true
		}
		remaining := make([]string, 0, len(priorScope))
		for _, u := range priorScope {
			if still[u] {
				remaining = append(remaining, u)
			}
		}
		if len(remaining) == 0 {
			jsonError(w, http.StatusUnprocessableEntity,
				"nothing left to pick up — the remaining units are gone from this branch")
			return
		}
		units, resumedFrom = remaining, prior.ID
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
	runSources := make([]string, 0, len(sources))
	for _, src := range sources {
		runSources = append(runSources, src.Name)
	}
	sourcesJSON, _ := json.Marshal(runSources)
	runID, err := s.store.CreateDriftRun(store.DriftRun{
		RepoKey: repo.Key(), Branch: branch, Mode: body.Mode, ScopeJSON: string(scopeJSON),
		DocsTotal: len(units), HeadSHA: headSHA,
		ReportPath: reportPath, ReportBranch: reportBranch,
		SourcesJSON: string(sourcesJSON), Focus: body.Focus, ResumedFrom: resumedFrom,
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
	srcNames := make([]string, 0, len(sources))
	for _, src := range sources {
		srcNames = append(srcNames, "~"+src.Name)
	}
	log.Printf("drift [%s@%s]: run %d start — mode=%s units=%d sources=%s report=%s%s%s%s",
		repo.ID, branch, runID, body.Mode, len(units), strings.Join(srcNames, ","),
		reportPath, map[bool]string{true: " on " + reportBranch, false: " (none)"}[reportBranch != ""],
		map[bool]string{true: " focus=" + strconv.Quote(body.Focus), false: ""}[body.Focus != ""],
		map[bool]string{true: fmt.Sprintf(" resuming run %d", resumedFrom), false: ""}[resumedFrom > 0])
	go s.driftWorker(ctx, cancel, key, runID, body.Mode, repo, branch, units, files, sources, idx,
		driftCfg.Instructions, body.Report, body.Focus, resumedFrom)
	jsonOK(w, map[string]any{"runId": runID, "docsTotal": len(units), "mode": body.Mode,
		"sources": len(sources), "focus": body.Focus, "resumedFrom": resumedFrom})
}

// POST /api/repos/{repo}/drift/cancel?branch=
func (s *Server) postDriftCancel(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	if !s.drift.cancel(driftKey(repo.Key(), branch)) {
		jsonError(w, http.StatusNotFound, "no drift run in progress")
		return
	}
	log.Printf("drift [%s@%s]: run cancelled by %s", repo.ID, branch, auth.UserFrom(r.Context()).Email)
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
		// reopening treats the finding as fresh again — every pointer to a
		// document it produced (draft, remedy, the planned set) is dropped
		// with the dismissal, since those documents may have been discarded
		_ = s.store.SetDriftFindingDraft(repo.Key(), branch, r.PathValue("fp"), "")
		_ = s.store.SetDriftFindingRemedy(repo.Key(), branch, r.PathValue("fp"), "", "")
		_ = s.store.SetDriftFindingDocuments(repo.Key(), branch, r.PathValue("fp"), "[]")
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
	sources []ai.GroundingSource, idx *linkIndex, instructions, reportPath, focus string,
	resumedFrom int64) {
	defer cancel()
	defer s.drift.release(key)

	runStarted := time.Now().Unix()
	dropped := 0
	var docErrs []string
	var activity []string
	var extracted []map[string]string
	status := "ok"
	label := func(unit string) string {
		if mode == "gaps" {
			return "~" + unit
		}
		return unit
	}
	// the activity feed is the run's live narration (card) and ends up in the
	// report — bounded so a long run cannot bloat either
	const maxActivity = 400
	persist := func(done int) {
		if len(activity) > maxActivity {
			activity = append([]string{fmt.Sprintf("… %d earlier lines trimmed", len(activity)-maxActivity)},
				activity[len(activity)-maxActivity:]...)
		}
		actJSON, _ := json.Marshal(activity)
		_ = s.store.UpdateDriftRunProgress(runID, done, dropped, string(actJSON))
	}
	// UTC, marked as such: these lines are persisted verbatim into the report
	// document. The SPA localizes them for display (lib/feed.ts).
	note := func(line string) {
		activity = append(activity, time.Now().UTC().Format("15:04:05")+"Z "+line)
	}
	// live: tool calls land in the feed as they happen, not after the unit
	liveNote := func(done int) func(string) {
		return func(line string) {
			note(line)
			persist(done)
			s.publish("drift", repo.Key(), branch)
		}
	}
	srcNames := make([]string, 0, len(sources))
	for _, src := range sources {
		srcNames = append(srcNames, "~"+src.Name)
	}
	sort.Strings(srcNames)
	var line string
	switch mode {
	case "extract":
		line = fmt.Sprintf("▸ analyzing %d application source%s into extracted requirements",
			len(units), plural(len(units)))
	case "gaps":
		line = fmt.Sprintf("▸ gap analysis over %d source%s (%s)",
			len(units), plural(len(units)), strings.Join(srcNames, ", "))
	default:
		line = fmt.Sprintf("▸ drift check of %d document%s against %s",
			len(units), plural(len(units)), strings.Join(srcNames, ", "))
	}
	if focus != "" { // every mode can be aimed at one area
		line += " · focus: " + focus
	}
	note(line)
	if resumedFrom > 0 {
		note(fmt.Sprintf("▸ picking up run %d where it stopped", resumedFrom))
	}
	persist(0)
	// how far the run actually got: a cancelled or interrupted run must NOT
	// report every unit done, or its remaining work looks finished and it
	// cannot be picked up (store.DriftRun.Resumable)
	done := 0
	for i, unit := range units {
		if ctx.Err() != nil {
			status = "cancelled"
			break
		}
		var findings []store.DriftFinding
		var droppedDoc int
		var err error
		note(fmt.Sprintf("[%d/%d] %s", i+1, len(units), label(unit)))
		persist(i)
		s.publish("drift", repo.Key(), branch)
		started := time.Now()
		switch mode {
		case "extract":
			var groups []extractedGroup
			groups, droppedDoc, err = s.driftCheckExtract(ctx, repo, branch, unit, files, sources, instructions, focus, liveNote(i))
			if err == nil {
				reqs := 0
				for _, g := range groups {
					reqs += len(g.Requirements)
				}
				run, rerr := s.store.LatestDriftRun(repo.Key(), branch)
				if rerr == nil && run.ReportBranch != "" {
					path, werr := s.writeExtraction(repo, run.ReportBranch, run.ReportPath, unit, run.HeadSHA, groups)
					if werr != nil {
						err = werr
					} else {
						note(fmt.Sprintf("  ✓ %d requirement%s in %d group%s → %s",
							reqs, plural(reqs), len(groups), plural(len(groups)), path))
						log.Printf("drift [%s@%s]: extracted ~%s → %s (%d requirement(s), %d group(s))",
							repo.ID, branch, unit, path, reqs, len(groups))
						extracted = append(extracted, map[string]string{"source": unit, "path": path})
						exJSON, _ := json.Marshal(extracted)
						_ = s.store.SetDriftRunExtractions(runID, string(exJSON))
						s.publish("save", repo.Key(), run.ReportBranch)
					}
				} else {
					note(fmt.Sprintf("  ✓ %d requirement%s in %d group%s (no report branch — not persisted)",
						reqs, plural(reqs), len(groups), plural(len(groups))))
				}
			}
		case "gaps":
			findings, droppedDoc, err = s.driftCheckGaps(ctx, repo, branch, unit, files, sources, instructions, reportPath, focus, liveNote(i))
		default:
			findings, droppedDoc, err = s.driftCheckDoc(ctx, repo, branch, unit, files, sources, idx, instructions, reportPath, focus, liveNote(i))
		}
		dropped += droppedDoc
		if err != nil {
			if ctx.Err() != nil {
				status = "cancelled"
				break
			}
			log.Printf("drift [%s@%s]: %s: %v", repo.ID, branch, unit, err)
			docErrs = append(docErrs, unit+": "+err.Error())
			note("  ✗ " + err.Error())
		} else if mode == "extract" {
			if droppedDoc > 0 {
				note(fmt.Sprintf("    ✗ %d requirement%s dropped — evidence not found in the source",
					droppedDoc, plural(droppedDoc)))
			}
		} else {
			keep := make([]string, 0, len(findings))
			for _, f := range findings {
				keep = append(keep, f.Fingerprint)
				// the run that last found it — what the run picker scopes by
				f.RunID = runID
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
			took := time.Since(started).Round(time.Millisecond)
			log.Printf("drift [%s@%s]: run %d [%d/%d] %s → %d %s(s), %d dropped (%s)",
				repo.ID, branch, runID, i+1, len(units), unit, len(findings), word, droppedDoc, took)
			if n := len(findings); n == 0 {
				note(fmt.Sprintf("  ✓ clean (%s)", took))
			} else {
				note(fmt.Sprintf("  ✓ %d %s%s (%s)", n, word, plural(n), took))
				for _, f := range findings {
					where := f.Anchor
					if f.Kind == "coverage-gap" {
						where = "~" + f.Source + "/" + f.Anchor
					}
					line := fmt.Sprintf("    ⚠ %s %s @ %s — %s", f.Severity, f.Kind, where, f.Title)
					if f.SuggestedPath != "" {
						line += " → " + f.SuggestedPath
					}
					note(line)
				}
			}
			if droppedDoc > 0 {
				note(fmt.Sprintf("    ✗ %d finding%s dropped — evidence not found in the source",
					droppedDoc, plural(droppedDoc)))
			}
		}
		done = i + 1
		persist(done)
		// the in-repo report follows along — every completed unit updates it
		s.writeDriftReport(runID, repo, branch, false)
		s.publish("drift", repo.Key(), branch)
	}
	open, _ := s.store.DriftFindings(repo.Key(), branch)
	live := 0
	for _, f := range open {
		if f.Status != "dismissed" {
			live++
		}
	}
	if done < len(units) {
		note(fmt.Sprintf("▪ stopped after %d of %d — the rest can be picked up",
			done, len(units)))
	}
	note(fmt.Sprintf("▪ %s — %d finding%s live%s", status, live, plural(live),
		map[bool]string{true: fmt.Sprintf(", %d dropped", dropped), false: ""}[dropped > 0]))
	persist(done)

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
	log.Printf("drift [%s@%s]: run %d %s in %s — %d finding(s) live, %d dropped%s",
		repo.ID, branch, runID, status, time.Since(time.Unix(runStarted, 0)).Round(time.Second),
		live, dropped, map[bool]string{true: fmt.Sprintf(", %d unit(s) failed", len(docErrs)), false: ""}[len(docErrs) > 0])
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
	line := fmt.Sprintf("- %sZ · %s · %d/%d %s · %s · %d finding%s live",
		time.Unix(run.StartedAt, 0).UTC().Format("2006-01-02 15:04"), run.Mode,
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

// reportTitle derives a document title from a report's filename, keeping a
// trailing date stamp readable ("alignment-2026-08-01" → "Alignment —
// 2026-08-01") instead of smearing it into words.
func reportTitle(reportPath string) string {
	name := strings.TrimSuffix(base(reportPath), ".md")
	stamp := ""
	switch {
	case wholeDate.MatchString(name): // the filename IS the stamp
		stamp, name = name, ""
	default:
		if m := dateSuffix.FindString(name); m != "" {
			stamp = strings.TrimPrefix(m, "-")
			name = strings.TrimSuffix(name, m)
		}
	}
	words := strings.Fields(strings.ReplaceAll(strings.ReplaceAll(name, "-", " "), "_", " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	title := strings.Join(words, " ")
	switch {
	case title == "" && stamp == "":
		return "Alignment report"
	case title == "":
		return "Alignment report — " + stamp
	case stamp == "":
		return title
	}
	return title + " — " + stamp
}

// a trailing ISO date, optionally with an -HHMM stamp
var (
	dateSuffix = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}(-\d{4})?$`)
	wholeDate  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(-\d{4})?$`)
)

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
	title := reportTitle(reportPath)
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
	switch run.Mode {
	case "gaps":
		mode = "gap analysis (reference sources swept for uncovered capabilities)"
		unitNoun = "sources"
	case "extract":
		mode = "app analysis (application sources extracted into a requirement inventory)"
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
	fmt.Fprintf(&b, "- Started: %s UTC\n", time.Unix(run.StartedAt, 0).UTC().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "- Status: %s\n", status)
	fmt.Fprintf(&b, "- Scope: %d %s\n", len(scope), unitNoun)
	if run.Focus != "" {
		fmt.Fprintf(&b, "- Focus: %s\n", run.Focus)
	}
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
			var docs []map[string]string
			_ = json.Unmarshal([]byte(f.DocumentsJSON), &docs)
			if len(docs) > 0 {
				parts := make([]string, 0, len(docs))
				for _, d := range docs {
					parts = append(parts, strings.ReplaceAll(d["kind"], "_", " ")+": "+d["path"])
				}
				st += " · " + strings.Join(parts, ", ")
			} else if f.RemedyPath != "" {
				st += " · " + strings.ReplaceAll(f.RemedyKind, "_", " ") + ": " + f.RemedyPath
			}
			cell := func(v string) string { return strings.ReplaceAll(v, "|", "\\|") }
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
				f.Severity, f.Kind, cell(where), cell(f.Title), cell(st))
		}
	}

	if len(run.ExtractionsJSON) > 0 && run.ExtractionsJSON != "[]" {
		var ex []struct{ Source, Path string }
		if json.Unmarshal([]byte(run.ExtractionsJSON), &ex) == nil && len(ex) > 0 {
			b.WriteString("\n## Extracted requirements\n\n")
			for _, e := range ex {
				fmt.Fprintf(&b, "- `~%s` → %s\n", e.Source, e.Path)
			}
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
	files map[string]string, sources []ai.GroundingSource, idx *linkIndex, instructions, reportPath, focus string,
	note func(string)) ([]store.DriftFinding, int, error) {
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
	extracted := ""
	for _, n := range names { // the analyzed baseline, when the app was extracted
		if b := extractionContext(files, reportPath, n); b != "" {
			extracted += "\n## ~" + n + "\n" + b + "\n"
		}
	}
	if extracted != "" {
		note("    · using extracted requirements as the baseline")
	}
	msgs := ai.DriftPrompt(doc, content, linked, extracted, focus, instructions, names)

	var out struct {
		Findings []modelFinding `json:"findings"`
	}
	if err := s.askJSON(ai.WithLabel(ctx, "drift "+doc), msgs, specs, tb.exec, note, &out); err != nil {
		return nil, 0, err
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
			RepoKey: repo.Key(), Branch: branch, Fingerprint: fp,
			DocPath: doc, SuggestedPath: cleanDocPath(f.SuggestedPath),
			Anchor: anchor, Source: f.Source, Kind: kind,
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
	files map[string]string, sources []ai.GroundingSource, instructions, reportPath, focus string,
	note func(string)) ([]store.DriftFinding, int, error) {
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
	extracted := extractionContext(files, reportPath, sourceName)
	if extracted != "" {
		note("    · using extracted requirements as the baseline")
	}
	msgs := ai.GapPrompt(sourceName, strings.Join(docs, "\n"), extracted, focus, instructions)

	var out struct {
		Findings []modelFinding `json:"findings"`
	}
	if err := s.askJSON(ai.WithLabel(ctx, "gaps ~"+sourceName), msgs, specs, tb.exec, note, &out); err != nil {
		return nil, 0, err
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

// ---------------------------------------------------------------- extraction

// extractionContext returns the engine block of the source's persisted
// extraction — the app's analyzed requirement inventory — so drift and gap
// runs compare against THAT rather than re-deriving it from raw source text
// every time. "" when the source has never been extracted.
func extractionContext(files map[string]string, reportPath, source string) string {
	content, ok := files[extractionPath(reportPath, source)]
	if !ok {
		return ""
	}
	start := strings.Index(content, extractionBegin)
	end := strings.Index(content, extractionEnd)
	if start < 0 || end <= start {
		return ""
	}
	block := strings.TrimSpace(content[start+len(extractionBegin) : end])
	if len(block) > 12*1024 {
		block = block[:12*1024] + "\n… (truncated)"
	}
	return block
}

// extractedRequirement is one atomic requirement the application itself
// imposes, as read out of a reference source.
type extractedRequirement struct {
	Title     string          `json:"title"`
	Statement string          `json:"statement"`
	Evidence  []driftEvidence `json:"evidence"`
	// filled by the matching pass
	Coverage  string `json:"coverage"` // full | partial | none
	CoveredBy string `json:"coveredBy"`
	Note      string `json:"note"`
}

// extractedGroup bundles requirements by capability.
type extractedGroup struct {
	Name         string                 `json:"name"`
	Summary      string                 `json:"summary"`
	Requirements []extractedRequirement `json:"requirements"`
}

// driftCheckExtract analyzes ONE application source by divide and conquer:
// survey it into capability areas, extract each area's requirements on its
// own AI loop, then walk the results in batches and match them against the
// workspace's documents. Evidence is verified exactly like a finding's.
func (s *Server) driftCheckExtract(ctx context.Context, repo *project.Project, branch, sourceName string,
	files map[string]string, sources []ai.GroundingSource, instructions, focus string,
	note func(string)) ([]extractedGroup, int, error) {
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
	ask := func(askCtx context.Context, msgs []ai.Message, out any) error {
		tb := &speccyToolbox{repo: repo, branch: branch, writable: false, sources: sources,
			files: files, publish: func() {}}
		var specs []ai.ToolSpec
		for _, spec := range tb.specs(files) {
			if spec.Name != "ask_user" {
				specs = append(specs, spec)
			}
		}
		return s.askJSON(askCtx, msgs, specs, tb.exec, note, out)
	}

	// ---- divide: survey the application into areas
	var survey struct {
		Areas []struct {
			Name    string   `json:"name"`
			Summary string   `json:"summary"`
			Paths   []string `json:"paths"`
		} `json:"areas"`
	}
	if err := ask(ai.WithLabel(ctx, "extract survey ~"+sourceName),
		ai.SurveyPrompt(sourceName, focus, instructions), &survey); err != nil {
		return nil, 0, fmt.Errorf("survey: %w", err)
	}
	areas := survey.Areas[:0:0]
	for _, a := range survey.Areas {
		if strings.TrimSpace(a.Name) != "" {
			areas = append(areas, a)
		}
	}
	if len(areas) == 0 {
		if focus != "" { // aiming at an area the source has nothing in is an answer, not a failure
			note("  ▸ nothing in ~" + sourceName + " belongs to “" + focus + "”")
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("survey returned no areas")
	}
	if len(areas) > maxExtractAreas {
		note(fmt.Sprintf("    · %d areas surveyed — capped at %d", len(areas), maxExtractAreas))
		areas = areas[:maxExtractAreas]
	}
	note(fmt.Sprintf("  ▸ divided ~%s into %d area%s", sourceName, len(areas), plural(len(areas))))

	// ---- conquer: one extraction pass per area
	dropped := 0
	var groups []extractedGroup
	for i, a := range areas {
		if ctx.Err() != nil {
			return groups, dropped, ctx.Err()
		}
		note(fmt.Sprintf("  ▸ area %d/%d: %s", i+1, len(areas), a.Name))
		var out struct {
			Requirements []extractedRequirement `json:"requirements"`
		}
		if err := ask(ai.WithLabel(ctx, "extract area "+a.Name),
			ai.ExtractPrompt(sourceName, a.Name, a.Summary, a.Paths, focus, instructions), &out); err != nil {
			note("    ✗ " + a.Name + ": " + err.Error()) // one bad area never sinks the source
			continue
		}
		kept := make([]extractedRequirement, 0, len(out.Requirements))
		for _, r := range out.Requirements {
			if strings.TrimSpace(r.Statement) == "" ||
				!verifyEvidence(modelFinding{Source: sourceName, Evidence: r.Evidence}, sources) {
				dropped++
				continue
			}
			r.Coverage, r.CoveredBy, r.Note = "none", "", ""
			kept = append(kept, r)
		}
		note(fmt.Sprintf("    ✓ %d requirement%s", len(kept), plural(len(kept))))
		if len(kept) > 0 {
			groups = append(groups, extractedGroup{Name: strings.TrimSpace(a.Name),
				Summary: strings.TrimSpace(a.Summary), Requirements: kept})
		}
	}

	// ---- match: walk the extracted requirements against the specs
	s.matchExtracted(ctx, groups, files, ask, note, instructions)
	return groups, dropped, nil
}

// matchExtracted walks every extracted requirement in batches and asks which
// workspace document already states it. Best-effort per batch: a failed batch
// leaves its requirements unmatched rather than failing the extraction.
func (s *Server) matchExtracted(ctx context.Context, groups []extractedGroup, files map[string]string,
	ask func(context.Context, []ai.Message, any) error, note func(string), instructions string) {
	type ref struct{ g, r int }
	var flat []ref
	for gi := range groups {
		for ri := range groups[gi].Requirements {
			flat = append(flat, ref{gi, ri})
		}
	}
	if len(flat) == 0 {
		return
	}
	docIndex := strings.Join(resolveDriftScope(files, nil, nil), "\n")
	matched := 0
	for start := 0; start < len(flat); start += matchBatchSize {
		if ctx.Err() != nil {
			return
		}
		end := start + matchBatchSize
		if end > len(flat) {
			end = len(flat)
		}
		var items strings.Builder
		for i, f := range flat[start:end] {
			r := groups[f.g].Requirements[f.r]
			fmt.Fprintf(&items, "%d. [%s] %s\n", i+1, groups[f.g].Name, r.Statement)
		}
		note(fmt.Sprintf("  ▸ matching %d-%d of %d against the specs", start+1, end, len(flat)))
		var out struct {
			Matches []struct {
				Index    int    `json:"index"`
				Coverage string `json:"coverage"`
				Document string `json:"document"`
				Note     string `json:"note"`
			} `json:"matches"`
		}
		if err := ask(ai.WithLabel(ctx, fmt.Sprintf("match %d-%d", start+1, end)),
			ai.MatchPrompt(items.String(), docIndex, instructions), &out); err != nil {
			note("    ✗ matching failed: " + err.Error())
			continue
		}
		for _, m := range out.Matches {
			if m.Index < 1 || m.Index > end-start {
				continue
			}
			f := flat[start+m.Index-1]
			r := &groups[f.g].Requirements[f.r]
			doc := cleanDocPath(m.Document)
			if _, ok := files[doc]; !ok { // only a real document counts
				doc = ""
			}
			switch strings.ToLower(strings.TrimSpace(m.Coverage)) {
			case "full":
				r.Coverage = "full"
			case "partial":
				r.Coverage = "partial"
			default:
				r.Coverage, doc = "none", ""
			}
			if doc == "" && r.Coverage != "none" { // a claim without a document is no claim
				r.Coverage = "none"
			}
			r.CoveredBy, r.Note = doc, strings.TrimSpace(m.Note)
			if r.Coverage != "none" {
				matched++
			}
		}
	}
	note(fmt.Sprintf("  ✓ matched %d of %d requirement%s to documents", matched, len(flat), plural(len(flat))))
}

// writeExtraction persists a source's inventory beside the alignment report,
// preserving whatever the human wrote outside the engine block.
func (s *Server) writeExtraction(repo *project.Project, reportBranch, reportPath, source, headSHA string,
	groups []extractedGroup) (string, error) {
	path := extractionPath(reportPath, source)
	existing, sha, _ := repo.File(reportBranch, path)
	content := mergeExtraction(existing, extractionBlock(source, headSHA, groups), source)
	content, err := mdfm.Touch(content, existing == "", time.Now())
	if err != nil {
		return "", err
	}
	if _, err := repo.SaveFile(reportBranch, path, content, sha); err != nil {
		return "", err
	}
	return path, nil
}

// extractionBlock renders the engine-owned section: a summary, then one table
// per capability group with each requirement, its evidence and the document
// that covers it (or a gap marker).
func extractionBlock(source, headSHA string, groups []extractedGroup) string {
	total, full, partial := 0, 0, 0
	for _, g := range groups {
		for _, r := range g.Requirements {
			total++
			switch r.Coverage {
			case "full":
				full++
			case "partial":
				partial++
			}
		}
	}
	var b strings.Builder
	b.WriteString("## Summary\n\n")
	fmt.Fprintf(&b, "- Source: `~%s`", source)
	if headSHA != "" {
		fmt.Fprintf(&b, " @ `%.10s`", headSHA)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "- Extracted: %s UTC\n", time.Now().UTC().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "- %d requirement(s) across %d area(s)\n", total, len(groups))
	fmt.Fprintf(&b, "- Coverage: %d fully stated by a document, %d partially, %d not covered\n",
		full, partial, total-full-partial)

	cell := func(v string) string { return strings.ReplaceAll(strings.ReplaceAll(v, "|", "\\|"), "\n", " ") }
	mark := map[string]string{"full": "✓ full", "partial": "◐ partial"}
	for _, g := range groups {
		fmt.Fprintf(&b, "\n## %s\n\n", g.Name)
		if g.Summary != "" {
			b.WriteString(g.Summary + "\n\n")
		}
		b.WriteString("| Requirement | Evidence | Coverage | Document |\n|---|---|---|---|\n")
		for _, r := range g.Requirements {
			ev := make([]string, 0, len(r.Evidence))
			for _, e := range r.Evidence {
				ev = append(ev, "`"+cell(e.Path)+"`: \u201c"+cell(e.Quote)+"\u201d")
			}
			cov, doc := mark[r.Coverage], r.CoveredBy
			if cov == "" {
				cov, doc = "— *not covered*", ""
			}
			if r.Note != "" {
				cov += "<br>*" + cell(r.Note) + "*"
			}
			if doc == "" {
				doc = "—"
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", cell(r.Statement), strings.Join(ev, "<br>"), cov, cell(doc))
		}
	}
	return b.String()
}

// mergeExtraction splices the engine block into the extraction document —
// same living-document contract as the alignment report.
func mergeExtraction(existing, block, source string) string {
	wrapped := extractionBegin + "\n\n" + block + "\n" + extractionEnd
	if existing != "" {
		start := strings.Index(existing, extractionBegin)
		end := strings.Index(existing, extractionEnd)
		if start >= 0 && end > start {
			return existing[:start] + wrapped + existing[end+len(extractionEnd):]
		}
		return strings.TrimRight(existing, "\n") + "\n\n" + wrapped + "\n"
	}
	title := "Extracted requirements — " + source
	return "---\ntitle: " + title + "\ntype: extraction\nsource: " + source +
		"\ngenerated_by: specquill drift engine\n---\n\n# " + title + "\n\n" +
		"_What the application itself requires, read out of `~" + source + "` and grouped by " +
		"capability. The block below is engine-maintained and rewritten on every extraction run; " +
		"everything outside it is yours._\n\n" + wrapped + "\n"
}

// POST /api/repos/{repo}/drift/focus?branch= {sources?} — propose the areas
// worth aiming the next gap analysis at. Read-only: it writes nothing and
// creates no run, it only answers "where should I look?".
func (s *Server) postDriftFocus(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "Speccy is not configured (ai: in specquill.yml)")
		return
	}
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	var body struct {
		Sources []string `json:"sources"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	files, err := repo.Snapshot(branch)
	if err != nil {
		gitFail(w, err)
		return
	}
	var driftCfg project.DriftConfig
	instructions := ""
	if cfg := inRepoConfig(repo, branch); cfg != nil {
		driftCfg, instructions = cfg.Drift, cfg.Speccy.Instructions
	}
	sources := s.driftSources(r, repo, branch, driftCfg)
	if len(body.Sources) > 0 {
		want := map[string]bool{}
		for _, n := range body.Sources {
			want[strings.TrimPrefix(strings.TrimSpace(n), "~")] = true
		}
		kept := sources[:0:0]
		for _, src := range sources {
			if want[src.Name] {
				kept = append(kept, src)
			}
		}
		sources = kept
	}
	if len(sources) == 0 {
		jsonError(w, http.StatusUnprocessableEntity, "no reference sources selected")
		return
	}

	// what we already know about each source: its extracted inventory when it
	// has one (coverage included), else just its size
	reportPath := driftReportPath(driftCfg)
	var b strings.Builder
	for _, src := range sources {
		fmt.Fprintf(&b, "\n## ~%s (%d files)\n", src.Name, len(src.Files))
		if block := extractionContext(files, reportPath, src.Name); block != "" {
			b.WriteString(block + "\n")
		} else {
			b.WriteString("(not extracted yet — explore it with the tools)\n")
		}
	}
	tb := &speccyToolbox{repo: repo, branch: branch, writable: false, sources: sources,
		files: files, publish: func() {}}
	var specs []ai.ToolSpec
	for _, spec := range tb.specs(files) {
		if spec.Name != "ask_user" {
			specs = append(specs, spec)
		}
	}
	var out struct {
		Areas []struct {
			Name    string   `json:"name"`
			Reason  string   `json:"reason"`
			Sources []string `json:"sources"`
		} `json:"areas"`
	}
	if err = s.askJSON(ai.WithLabel(r.Context(), "focus areas"),
		ai.FocusPrompt(b.String(), strings.Join(resolveDriftScope(files, nil, nil), "\n"), instructions),
		specs, tb.exec, nil, &out); err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	known := map[string]bool{}
	for _, src := range sources {
		known[src.Name] = true
	}
	areas := []map[string]any{}
	for _, a := range out.Areas {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		names := []string{}
		for _, n := range a.Sources { // only sources this project actually has
			if n = strings.TrimPrefix(strings.TrimSpace(n), "~"); known[n] {
				names = append(names, n)
			}
		}
		areas = append(areas, map[string]any{
			"name": name, "reason": strings.TrimSpace(a.Reason), "sources": names,
		})
	}
	log.Printf("drift [%s@%s]: proposed %d focus area(s) over %d source(s)",
		repo.ID, branch, len(areas), len(sources))
	jsonOK(w, map[string]any{"areas": areas})
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
	if !draftableKind(finding.Kind) {
		jsonError(w, http.StatusBadRequest,
			"only coverage-gap and new-requirement findings propose a new document to draft")
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
	var draft struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := s.completeJSON(ai.WithLabel(r.Context(), "reverse-engineer "+finding.Fingerprint),
		ai.ReversePrompt(string(findingJSON), excerpts.String(), guidance), &draft); err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
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
	log.Printf("drift [%s@%s]: drafted %s for %s on %s", repo.ID, branch, path, finding.Fingerprint, writeBranch)
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

	// the affected document: the drafted document when the finding produced
	// one (a gap, or a new requirement beside the audited doc), else the
	// document the finding is about
	targetPath := finding.DraftPath
	if targetPath == "" {
		targetPath = finding.DocPath
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
	var draft struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := s.completeJSON(ai.WithLabel(r.Context(), "remedy "+body.Kind),
		ai.RemedyPrompt(kindLabel, entity.Folder, linkNote,
			string(findingJSON), targetExcerpt, example, guidance), &draft); err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
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
	log.Printf("drift [%s@%s]: %s remedy for %s → %s on %s (%s)",
		repo.ID, branch, body.Kind, finding.Fingerprint, path, writeBranch,
		map[bool]string{true: linked, false: "no typed link"}[linked != ""])
	s.refreshDriftReport(repo, branch)
	s.publish("save", repo.Key(), writeBranch)
	s.publish("drift", repo.Key(), branch)
	jsonOK(w, map[string]any{"path": path, "kind": body.Kind, "branch": writeBranch, "linked": linked})
}

// ---------------------------------------------------------------- planning

// plannedDoc is one document a plan proposes: a family, a path, and what it
// should link to (indices into the plan, or the finding's own document).
type plannedDoc struct {
	Kind            string `json:"kind"`
	Title           string `json:"title"`
	Path            string `json:"path"`
	Purpose         string `json:"purpose"`
	LinksTo         []int  `json:"linksTo"`
	LinksToDocument bool   `json:"linksToDocument"`
	// filled in by the server
	Field       string   `json:"field,omitempty"`       // the typed link that will be written
	LinkTargets []string `json:"linkTargets,omitempty"` // resolved, for display
}

// familyType reads the frontmatter `type:` a family's existing documents use.
// The label is workspace prose ("Change Record", "Specification"), so it can
// only be observed, never derived — "" when the family has no document yet.
func familyType(files map[string]string, folder string) string {
	for _, p := range sortedKeys(files) {
		if !strings.HasPrefix(p, folder) || !strings.HasSuffix(p, ".md") || okf.Reserved(base(p)) {
			continue
		}
		fm, _, _ := mdfm.Split(files[p])
		var meta struct {
			Type string `yaml:"type"`
		}
		if yaml.Unmarshal([]byte(fm), &meta) == nil && strings.TrimSpace(meta.Type) != "" {
			return strings.TrimSpace(meta.Type)
		}
	}
	return ""
}

// planContext gathers what both planning and creating need from the branch.
func (s *Server) planContext(r *http.Request, repo *project.Project, branch string) (
	files map[string]string, entities map[string]workspaceEntity, links []workspaceLink,
	guidance string, err error) {
	files, err = repo.Snapshot(branch)
	if err != nil {
		return nil, nil, nil, "", err
	}
	entities, links = workspaceModel(files)
	instructions := ""
	if cfg := inRepoConfig(repo, branch); cfg != nil {
		instructions = cfg.Speccy.Instructions
	}
	guidance = modelRules(files) + workspaceVocabulary(files) + ai.AuthoringRules(files, instructions)
	return files, entities, links, guidance, nil
}

// validatePlan keeps a proposed plan inside what the workspace allows: known
// families, paths in their folders, and only links its link_types permit.
func validatePlan(docs []plannedDoc, targetPath, targetKind string,
	entities map[string]workspaceEntity, links []workspaceLink) []plannedDoc {
	out := make([]plannedDoc, 0, len(docs))
	kept := map[int]int{} // original index → index in out
	for i, d := range docs {
		e, ok := entities[strings.TrimSpace(d.Kind)]
		if !ok || strings.TrimSpace(d.Title) == "" {
			continue // a family this workspace does not have
		}
		path := cleanDocPath(d.Path)
		if path == "" || okf.Reserved(base(path)) {
			continue
		}
		if !strings.HasPrefix(path, e.Folder) { // the family's folder is authoritative
			path = e.Folder + base(path)
		}
		kept[i] = len(out)
		out = append(out, plannedDoc{Kind: e.Kind, Title: strings.TrimSpace(d.Title), Path: path,
			Purpose: strings.TrimSpace(d.Purpose), LinksTo: d.LinksTo, LinksToDocument: d.LinksToDocument})
	}
	// resolve links now that the surviving set is known
	for i := range out {
		var targets []string
		seen := map[string]bool{}
		add := func(p, kind string) {
			if p == "" || p == out[i].Path || seen[p] {
				return
			}
			field, onFrom := linkBetween(links, out[i].Kind, kind)
			if field == "" || !onFrom { // only links this document may carry itself
				return
			}
			seen[p] = true
			out[i].Field = field
			targets = append(targets, p)
		}
		for _, orig := range docs[i].LinksTo {
			if j, ok := kept[orig]; ok {
				add(out[j].Path, out[j].Kind)
			}
		}
		if docs[i].LinksToDocument && targetPath != "" {
			add(targetPath, targetKind)
		}
		out[i].LinkTargets = targets
	}
	return out
}

// POST /api/repos/{repo}/drift/findings/{fp}/plan — propose WHICH documents
// to create for a finding, from the families this workspace actually has.
// Read-only: it writes nothing.
func (s *Server) postDriftPlan(w http.ResponseWriter, r *http.Request, repo *project.Project) {
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
	files, entities, links, guidance, err := s.planContext(r, repo, branch)
	if err != nil {
		gitFail(w, err)
		return
	}

	// the families this workspace has, and how they may link
	var fam strings.Builder
	kinds := make([]string, 0, len(entities))
	for k := range entities {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		e := entities[k]
		n := 0
		for p := range files {
			if strings.HasPrefix(p, e.Folder) && strings.HasSuffix(p, ".md") {
				n++
			}
		}
		fmt.Fprintf(&fam, "- %s → %s (%s level, %d existing document(s))\n", e.Kind, e.Folder, e.Group, n)
	}
	var lt strings.Builder
	for _, l := range links {
		fmt.Fprintf(&lt, "- `%s:` on %s → %s\n", l.Name, strings.Join(l.From, ", "), strings.Join(l.To, ", "))
	}

	targetPath := finding.DraftPath
	if targetPath == "" {
		targetPath = finding.DocPath
	}
	target, targetKind := "", ""
	if targetPath != "" {
		if content, ok := files[targetPath]; ok {
			targetKind = docKind(targetPath, content, entities)
			if len(content) > 4*1024 {
				content = content[:4*1024] + "\n… (truncated)"
			}
			target = "## " + targetPath + " (" + targetKind + ")\n```\n" + content + "\n```\n"
		}
	}
	var evidence []driftEvidence
	_ = json.Unmarshal([]byte(finding.EvidenceJSON), &evidence)
	findingJSON, _ := json.Marshal(map[string]any{
		"kind": finding.Kind, "severity": finding.Severity, "title": finding.Title,
		"detail": finding.Detail, "document": finding.DocPath, "anchor": finding.Anchor,
		"source": "~" + finding.Source, "suggestedPath": finding.SuggestedPath, "evidence": evidence,
	})
	var plan struct {
		Rationale string       `json:"rationale"`
		Documents []plannedDoc `json:"documents"`
	}
	if err := s.completeJSON(ai.WithLabel(r.Context(), "plan "+finding.Fingerprint),
		ai.PlanPrompt(string(findingJSON), target,
			fam.String(), lt.String(), strings.Join(resolveDriftScope(files, nil, nil), "\n"), guidance), &plan); err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	docs := validatePlan(plan.Documents, targetPath, targetKind, entities, links)
	if len(docs) == 0 {
		jsonError(w, http.StatusBadGateway, "the plan proposed no document this workspace can create")
		return
	}
	log.Printf("drift [%s@%s]: planned %d document(s) for %s (model proposed %d)",
		repo.ID, branch, len(docs), finding.Fingerprint, len(plan.Documents))
	jsonOK(w, map[string]any{"rationale": strings.TrimSpace(plan.Rationale), "documents": docs})
}

// POST /api/repos/{repo}/drift/findings/{fp}/create {documents} — draft and
// write a planned SET of documents, wiring the typed links between them and
// to the finding's document. Partial success is reported, never rolled back:
// every document that landed is a real worktree draft.
func (s *Server) postDriftCreate(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "Speccy is not configured (ai: in specquill.yml)")
		return
	}
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	var body struct {
		Documents []plannedDoc `json:"documents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Documents) == 0 {
		jsonError(w, http.StatusBadRequest, "documents required")
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
	files, entities, links, guidance, err := s.planContext(r, repo, branch)
	if err != nil {
		gitFail(w, err)
		return
	}
	writeBranch := branch
	if repo.Repo.Cfg.IsProtected(branch) {
		if writeBranch, err = s.claimWorkspace(r, repo); err != nil {
			gitFail(w, err)
			return
		}
	}
	targetPath := finding.DraftPath
	if targetPath == "" {
		targetPath = finding.DocPath
	}
	targetKind := ""
	if targetPath != "" {
		if content, ok := files[targetPath]; ok {
			targetKind = docKind(targetPath, content, entities)
		}
	}
	docs := validatePlan(body.Documents, targetPath, targetKind, entities, links)
	if len(docs) == 0 {
		jsonError(w, http.StatusUnprocessableEntity, "no document in the plan is valid for this workspace")
		return
	}

	var evidence []driftEvidence
	_ = json.Unmarshal([]byte(finding.EvidenceJSON), &evidence)
	findingJSON, _ := json.Marshal(map[string]any{
		"kind": finding.Kind, "severity": finding.Severity, "title": finding.Title,
		"detail": finding.Detail, "document": finding.DocPath, "anchor": finding.Anchor,
		"source": "~" + finding.Source, "evidence": evidence,
	})
	created := []map[string]string{}
	failures := []string{}
	for _, d := range docs {
		if _, _, err := repo.File(writeBranch, d.Path); err == nil {
			failures = append(failures, d.Path+": already exists")
			continue
		}
		example := ""
		for _, p := range sortedKeys(files) {
			if strings.HasPrefix(p, entities[d.Kind].Folder) && strings.HasSuffix(p, ".md") && !okf.Reserved(base(p)) {
				c := files[p]
				if len(c) > 4*1024 {
					c = c[:4*1024] + "\n… (truncated)"
				}
				example = "## " + p + "\n```\n" + c + "\n```\n"
				break
			}
		}
		note := "Write this document: " + d.Title + ". " + d.Purpose
		if d.Field != "" && len(d.LinkTargets) > 0 {
			note += "\nThe server adds `" + d.Field + ": [" + strings.Join(d.LinkTargets, ", ") +
				"]` — do not write link fields yourself."
		}
		kindLabel := strings.ReplaceAll(d.Kind, "_", " ")
		var draft struct{ Path, Content string }
		if err := s.completeJSON(ai.WithLabel(r.Context(), "create "+d.Kind+" "+d.Path),
			ai.RemedyPrompt(kindLabel, entities[d.Kind].Folder,
				note, string(findingJSON), "", example, guidance), &draft); err != nil {
			failures = append(failures, d.Path+": "+err.Error())
			continue
		}
		if err := mdfm.Validate(draft.Content); err != nil {
			failures = append(failures, d.Path+": "+err.Error())
			continue
		}
		content, err := mdfm.Touch(draft.Content, true, time.Now())
		if err != nil {
			failures = append(failures, d.Path+": "+err.Error())
			continue
		}
		// the plan's path AND the family's type win: the model drafts content,
		// not placement or classification
		if t := familyType(files, entities[d.Kind].Folder); t != "" {
			if fixed, err := mdfm.SetScalar(content, "type", t); err == nil {
				content = fixed
			}
		}
		for _, t := range d.LinkTargets {
			if next, added, err := mdfm.AppendListItem(content, d.Field, t); err == nil && added {
				content = next
			}
		}
		if _, err := repo.SaveFile(writeBranch, d.Path, content, ""); err != nil {
			failures = append(failures, d.Path+": "+err.Error())
			continue
		}
		files[d.Path] = content
		created = append(created, map[string]string{"kind": d.Kind, "path": d.Path})
	}
	if len(created) == 0 {
		jsonError(w, http.StatusBadGateway, "nothing could be created: "+strings.Join(failures, "; "))
		return
	}
	// record every document this finding produced (the set, not just one)
	var have []map[string]string
	_ = json.Unmarshal([]byte(finding.DocumentsJSON), &have)
	have = append(have, created...)
	docsJSON, _ := json.Marshal(have)
	_ = s.store.SetDriftFindingDocuments(repo.Key(), branch, finding.Fingerprint, string(docsJSON))
	// keep the single-document pointers meaningful for the existing actions
	for _, c := range created {
		if c["kind"] == "requirement" && finding.DraftPath == "" {
			_ = s.store.SetDriftFindingDraft(repo.Key(), branch, finding.Fingerprint, c["path"])
			break
		}
	}
	for _, c := range created {
		if c["kind"] != "requirement" && finding.RemedyPath == "" {
			_ = s.store.SetDriftFindingRemedy(repo.Key(), branch, finding.Fingerprint, c["path"], c["kind"])
			break
		}
	}
	made := make([]string, 0, len(created))
	for _, c := range created {
		made = append(made, c["kind"]+":"+c["path"])
	}
	log.Printf("drift [%s@%s]: created %d document(s) for %s on %s — %s%s",
		repo.ID, branch, len(created), finding.Fingerprint, writeBranch, strings.Join(made, ", "),
		map[bool]string{true: fmt.Sprintf(" (%d failed)", len(failures)), false: ""}[len(failures) > 0])
	s.refreshDriftReport(repo, branch)
	s.publish("save", repo.Key(), writeBranch)
	s.publish("drift", repo.Key(), branch)
	jsonOK(w, map[string]any{"created": created, "failures": failures, "branch": writeBranch})
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
	log.Printf("drift [%s@%s]: filed %s on %s → %s (created=%v, backlink=%v)",
		repo.ID, branch, finding.Fingerprint, target.name, itemURL, created, backlinked)
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

// askJSON runs a tool loop and decodes its JSON reply, re-asking ONCE with the
// parse error quoted back when the reply does not decode. A malformed reply is
// a sampling fluke — a trailing second object, a stray sentence — and without
// this it sinks a whole unit (an extraction area, a match batch) that costs a
// full model call to redo. Transport blips are handled a layer down, in
// ai.Client's own retry.
func (s *Server) askJSON(ctx context.Context, msgs []ai.Message, specs []ai.ToolSpec, exec ai.ToolExec, note func(string), out any) error {
	if note == nil {
		note = func(string) {}
	}
	for attempt := 0; ; attempt++ {
		var reply strings.Builder
		if _, _, err := s.ai.StreamTools(ctx, msgs, specs, exec,
			func(delta string) error { reply.WriteString(delta); return nil },
			func(tc ai.ToolCall, _ string, execErr error) error { note(toolNote(tc, execErr)); return nil },
		); err != nil {
			return err
		}
		err := ai.ExtractJSON(reply.String(), out)
		if err == nil {
			return nil
		}
		if attempt > 0 {
			return fmt.Errorf("model reply was not JSON: %w", err)
		}
		note("    · reply was not valid JSON (" + err.Error() + ") — asking again")
		log.Printf("ai: reply did not parse (%v) — re-asking once", err)
		msgs = append(append([]ai.Message{}, msgs...), repairMessages(reply.String(), err)...)
	}
}

// completeJSON is askJSON for the one-shot actions (draft, remedy, plan,
// create): no tool loop, same corrective re-ask when the reply does not parse.
func (s *Server) completeJSON(ctx context.Context, msgs []ai.Message, out any) error {
	for attempt := 0; ; attempt++ {
		reply, err := s.ai.Complete(ctx, msgs)
		if err != nil {
			return err
		}
		err = ai.ExtractJSON(reply, out)
		if err == nil {
			return nil
		}
		if attempt > 0 {
			return fmt.Errorf("model reply was not JSON: %w", err)
		}
		log.Printf("ai: reply did not parse (%v) — re-asking once", err)
		msgs = append(append([]ai.Message{}, msgs...), repairMessages(reply, err)...)
	}
}

// repairMessages quote the parse failure back to the model. Handing it its own
// reply plus the error beats re-rolling the same prompt: the model can see what
// it did wrong.
func repairMessages(reply string, err error) []ai.Message {
	return []ai.Message{
		{Role: "assistant", Content: reply},
		{Role: "user", Content: "That reply was not valid JSON: " + err.Error() +
			". Reply again with ONLY the JSON object — no prose, no code fence, and nothing after the closing brace."},
	}
}
