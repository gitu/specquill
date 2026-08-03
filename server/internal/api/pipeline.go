package api

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"specquill/server/internal/ai"
	"specquill/server/internal/project"
	"specquill/server/internal/recipe"
)

// The alignment stage runner: ONE engine behind every mode.
//
// drift, gaps and extraction used to be three hardcoded pipelines in drift.go
// doing the same shape of work — fan out over units, ask the model, verify the
// evidence, persist. They now ship as built-in recipes (internal/recipe/builtin)
// and run through this file, exactly like a project's own recipe under
// .specquill/alignment/.
//
// The engine owns the machinery — tools, JSON repair, evidence verification,
// checkpoints, the call ceiling. It owns NO prose: every prompt, heading and
// instruction lives in the recipe document, which is why converting the
// built-ins could be checked byte-for-byte against the prompts they replaced
// (pipeline_golden_test.go).

// stageResult is one stage's output within a unit. Persisted verbatim as the
// resume checkpoint, so its shape is a storage format: keep it JSON-plain.
type stageResult struct {
	Done  bool             `json:"done"`
	Items []map[string]any `json:"items,omitempty"`
}

// unitState checkpoints ONE unit's progress through the recipe. Only the
// in-flight unit's state is kept — the runner drops it the moment the unit
// finishes, so this never grows with the size of the run.
type unitState struct {
	Unit   string                 `json:"unit"`
	Stages map[string]stageResult `json:"stages"`
	// Files is the resolved describe-filter per source, so a resumed run does
	// not pay for the selection pass again.
	Files map[string][]string `json:"files,omitempty"`
}

// maxStageStateBytes bounds the checkpoint. Overshooting costs a redo of the
// unit on resume, never a failure — so the cap can be blunt.
const maxStageStateBytes = 256 * 1024

// runContext is everything a unit's stages need that does not change between
// them. Assembled once per unit by the worker.
type runContext struct {
	repo     *project.Project
	branch   string
	rec      *recipe.Recipe
	files    map[string]string // workspace snapshot
	sources  []ai.GroundingSource
	idx      *linkIndex
	focus    string
	report   string
	docIndex string

	// Test seams: the prompt builders these stages replaced took the linked
	// block and the extraction block as arguments, so the golden tests inject
	// them at the same point instead of standing up a repo and a link graph.
	linkedOverride, extractedOverride string

	// dropped counts items discarded because their evidence did not check out
	// against the source — the run's droppedUnverified.
	dropped int

	note  func(string)             // activity feed
	spend func(int) error          // charge model calls against the run's ceiling
	state *unitState               // the resume checkpoint (never nil)
	save  func(*unitState) error   // persist the checkpoint
	tools func(context.Context, recipe.FileFilter) (*speccyToolbox, []ai.ToolSpec, error)
}

// errCallCeiling stops a run that has spent its budget. Its own error so the
// worker can finish with status `capped` — resumable, not failed.
var errCallCeiling = fmt.Errorf("this run reached the model-call ceiling")

// runUnit executes the whole recipe for ONE unit (a document, or a reference
// source), resuming from whatever the checkpoint already holds.
//
// Returns EVERY stage's output keyed by stage id. Interpreting it is the
// caller's job — this function knows only stages, not what a finding or an
// inventory is.
func (s *Server) runUnit(ctx context.Context, rc *runContext, unit string) (map[string][]stageItem, error) {
	if rc.state == nil || rc.state.Unit != unit {
		rc.state = &unitState{Unit: unit, Stages: map[string]stageResult{}}
	}
	if rc.state.Stages == nil {
		rc.state.Stages = map[string]stageResult{}
	}
	produced := map[string][]stageItem{}

	for i := range rc.rec.Stages {
		st := &rc.rec.Stages[i]
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// resume: a stage the checkpoint already completed is rehydrated, not re-run
		if done, ok := rc.state.Stages[st.ID]; ok && done.Done {
			produced[st.ID] = wrapItems(done.Items)
			if len(rc.rec.Stages) > 1 {
				rc.note(fmt.Sprintf("  ▸ %s: already done, picked up from the checkpoint", stageLabel(st)))
			}
			continue
		}
		items, err := s.runStage(ctx, rc, unit, st, produced)
		if err != nil {
			return nil, err
		}
		produced[st.ID] = items
		rc.state.Stages[st.ID] = stageResult{Done: true, Items: unwrapItems(items)}
		if err := rc.save(rc.state); err != nil {
			// a checkpoint that will not persist costs a redo on resume; it is
			// never worth failing the unit over
			rc.note("    · checkpoint not saved: " + err.Error())
		}
	}
	return produced, nil
}

// Findings returns the items of the last stage that produced findings.
func recipeFindings(rec *recipe.Recipe, produced map[string][]stageItem) []stageItem {
	for i := len(rec.Stages) - 1; i >= 0; i-- {
		if rec.Stages[i].Produces == recipe.ProducesFindings {
			return produced[rec.Stages[i].ID]
		}
	}
	return nil
}

// recipeGroups assembles an `output: extraction` recipe's product: the last
// items stage's output, regrouped under the upstream items it fanned out from
// (survey areas → their requirements). A single-stage extraction recipe gets
// one unnamed group, which the inventory renders just fine.
func recipeGroups(rec *recipe.Recipe, produced map[string][]stageItem) []extractedGroup {
	var last *recipe.Stage
	for i := len(rec.Stages) - 1; i >= 0; i-- {
		if rec.Stages[i].Produces == recipe.ProducesItems {
			last = &rec.Stages[i]
			break
		}
	}
	if last == nil {
		return nil
	}
	items := produced[last.ID]
	if len(items) == 0 {
		return nil
	}
	parents := produced[last.Over] // empty when `over: unit`
	byParent := map[int][]stageItem{}
	var order []int
	for _, it := range items {
		if _, seen := byParent[it.Parent]; !seen {
			order = append(order, it.Parent)
		}
		byParent[it.Parent] = append(byParent[it.Parent], it)
	}
	var groups []extractedGroup
	for _, p := range order {
		var name, summary string
		if p >= 0 && p < len(parents) {
			name, _ = parents[p].Fields["name"].(string)
			summary, _ = parents[p].Fields["summary"].(string)
		}
		g := extractedGroup{Name: strings.TrimSpace(name), Summary: strings.TrimSpace(summary)}
		for _, it := range byParent[p] {
			g.Requirements = append(g.Requirements, toRequirement(it.Fields))
		}
		groups = append(groups, g)
	}
	return groups
}

// toRequirement reads one extracted requirement out of a stage item, including
// whatever the matching stage stamped onto it.
func toRequirement(f map[string]any) extractedRequirement {
	raw, _ := json.Marshal(f)
	var r extractedRequirement
	_ = json.Unmarshal(raw, &r)
	// the matcher names the covering document `document` (that is what reads
	// naturally in its prompt); the inventory calls it coveredBy
	if r.CoveredBy == "" {
		r.CoveredBy, _ = f["document"].(string)
	}
	// a coverage claim without a real document is no claim (the caller checks
	// the document exists); default anything unmatched to none
	switch strings.ToLower(strings.TrimSpace(r.Coverage)) {
	case "full":
		r.Coverage = "full"
	case "partial":
		r.Coverage = "partial"
	default:
		r.Coverage, r.CoveredBy = "none", ""
	}
	return r
}

// stageItem is one thing a stage produced, with a pointer back to the upstream
// item it came from — extraction needs it to regroup requirements under their
// survey area.
type stageItem struct {
	Fields map[string]any
	Parent int
}

func wrapItems(raw []map[string]any) []stageItem {
	out := make([]stageItem, 0, len(raw))
	for _, m := range raw {
		parent := -1
		if p, ok := m["__parent"].(float64); ok {
			parent = int(p)
		}
		out = append(out, stageItem{Fields: m, Parent: parent})
	}
	return out
}

func unwrapItems(items []stageItem) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		m := make(map[string]any, len(it.Fields)+1)
		for k, v := range it.Fields {
			m[k] = v
		}
		if it.Parent >= 0 {
			m["__parent"] = float64(it.Parent)
		}
		out = append(out, m)
	}
	return out
}

func stageLabel(st *recipe.Stage) string {
	if st.Label != "" {
		return st.Label
	}
	return st.ID
}

// runStage fans one stage out over whatever it runs on: once for the unit, or
// once per item (or batch of items) an earlier stage produced.
func (s *Server) runStage(ctx context.Context, rc *runContext, unit string,
	st *recipe.Stage, produced map[string][]stageItem) ([]stageItem, error) {

	multi := len(rc.rec.Stages) > 1
	if st.Over == "unit" {
		if multi && st.Produces == recipe.ProducesItems {
			// nothing yet to report — the count line comes after the call
		} else if multi {
			rc.note("  ▸ " + stageLabel(st))
		}
		items, err := s.askStage(ctx, rc, unit, st, nil, -1)
		if err != nil {
			return nil, err
		}
		items = rc.keep(st, items, unit)
		if multi && st.Produces == recipe.ProducesItems {
			line := st.Line("produced", "{{label}}: {{count}} {{nouns}}", map[string]string{
				"unit": unit, "count": strconv.Itoa(len(items)),
				"noun": st.Nouns(1), "nouns": st.Nouns(len(items)), "label": stageLabel(st),
			})
			if line != "" {
				rc.note("  ▸ " + line)
			}
		}
		return items, nil
	}

	upstream := produced[st.Over]
	if len(upstream) == 0 {
		return nil, nil
	}
	if st.Produces == recipe.ProducesAnnotations {
		return nil, s.annotate(ctx, rc, unit, st, produced)
	}

	// the per-item line names what is being WALKED (the upstream stage's
	// items), not what this stage makes of them
	upNoun := stageLabel(st)
	if up, ok := rc.rec.Stage(st.Over); ok {
		upNoun = up.Nouns(1)
	}
	var out []stageItem
	for i, up := range upstream {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rc.note("  ▸ " + st.Line("each", "{{noun}} {{index}}/{{total}}: {{name}}", map[string]string{
			"noun": upNoun, "index": strconv.Itoa(i + 1),
			"total": strconv.Itoa(len(upstream)), "name": itemName(up, i), "unit": unit,
		}))
		items, err := s.askStage(ctx, rc, unit, st, []stageItem{up}, i)
		if err != nil {
			// on_error: skip is how extraction has always treated a bad area —
			// one failed item is noted, the unit carries on
			if st.OnError == "skip" && ctx.Err() == nil {
				rc.note("    ✗ " + itemName(up, i) + ": " + err.Error())
				continue
			}
			return nil, err
		}
		items = rc.keep(st, items, unit)
		for j := range items {
			items[j].Parent = i
			// carry the parent's name down, so a later batch stage can render
			// "[area] statement" without knowing the graph
			if name, ok := up.Fields["name"].(string); ok && name != "" {
				items[j].Fields["__group"] = name
			}
		}
		rc.note(fmt.Sprintf("    ✓ %d %s", len(items), st.Nouns(len(items))))
		out = append(out, items...)
	}
	return out, nil
}

// annotate runs a stage that stamps fields onto an EARLIER stage's items by
// index — the coverage matcher, which walks extracted requirements in batches
// and says which document already states each one. Best-effort per batch: a
// failed batch leaves its items unannotated rather than failing the unit.
func (s *Server) annotate(ctx context.Context, rc *runContext, unit string,
	st *recipe.Stage, produced map[string][]stageItem) error {

	targetID := st.Annotates
	if targetID == "" {
		targetID = st.Over
	}
	items := produced[targetID]
	if len(items) == 0 {
		return nil
	}
	// the feed counts what is being MATCHED (the annotated stage's items),
	// not the annotation stage's own replies
	nouns := st.Nouns(len(items))
	if target, ok := rc.rec.Stage(targetID); ok {
		nouns = target.Nouns(len(items))
	}
	size := st.Batch
	if size <= 0 {
		size = len(items)
	}
	annotated := 0
	for start := 0; start < len(items); start += size {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(start+size, len(items))
		rc.note("  ▸ " + st.Line("batch", "{{label}} {{from}}-{{to}} of {{total}}", map[string]string{
			"label": stageLabel(st), "from": strconv.Itoa(start + 1),
			"to": strconv.Itoa(end), "total": strconv.Itoa(len(items)), "nouns": nouns,
		}))
		out, err := s.askStage(ctx, rc, unit, st, items[start:end], start)
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			rc.note("    ✗ " + stageLabel(st) + " failed: " + err.Error())
			continue
		}
		// each reply names the 1-based index WITHIN the batch
		for _, ann := range out {
			idx, ok := ann.Fields["index"].(float64)
			if !ok || int(idx) < 1 || int(idx) > end-start {
				continue
			}
			t := items[start+int(idx)-1]
			for k, v := range ann.Fields {
				if k != "index" {
					t.Fields[k] = v
				}
			}
			// an annotation may not claim a document that does not exist —
			// the same rule the drafting and planning paths apply. A claim
			// without a real document is no claim, and does not count as a
			// match either.
			if raw, names := t.Fields["document"]; names {
				doc, _ := raw.(string)
				doc = cleanDocPath(doc)
				if _, exists := rc.files[doc]; !exists {
					doc = ""
				}
				t.Fields["document"] = doc
				if doc == "" {
					if _, graded := t.Fields["coverage"]; graded {
						t.Fields["coverage"] = "none"
					}
					continue
				}
			}
			annotated++
		}
	}
	rc.note("  ✓ " + st.Line("done", "{{label}}: {{done}} of {{total}}", map[string]string{
		"label": stageLabel(st), "done": strconv.Itoa(annotated),
		"total": strconv.Itoa(len(items)), "nouns": nouns,
	}))
	return nil
}

// keep filters a stage's fresh items: evidence that does not check out, items
// with no identity (the surveyor occasionally returns a nameless area), and
// anything past the stage's fan-out cap.
//
// Verification happens HERE, as the items are produced, rather than at the end
// of the unit — otherwise a later stage spends model calls matching
// requirements that were always going to be discarded, and the counts it
// narrates are about work that never existed.
func (rc *runContext) keep(st *recipe.Stage, items []stageItem, unit string) []stageItem {
	if st.Verify {
		kept := items[:0:0]
		for _, it := range items {
			f := toModelFinding(it.Fields)
			// a per-source recipe's items belong to the unit whatever the model
			// claims; a per-document one names its own source
			if rc.rec.Units == recipe.UnitsSources {
				f.Source = unit
			}
			if !verifyEvidence(f, rc.sources) {
				rc.dropped++
				continue
			}
			kept = append(kept, it)
		}
		items = kept
	}
	if st.Require != "" {
		kept := items[:0:0]
		for _, it := range items {
			if v, ok := it.Fields[st.Require].(string); ok && strings.TrimSpace(v) != "" {
				kept = append(kept, it)
			}
		}
		items = kept
	}
	if st.Max > 0 && len(items) > st.Max {
		rc.note(fmt.Sprintf("    · %d %s — capped at %d", len(items), st.Nouns(len(items)), st.Max))
		items = items[:st.Max]
	}
	return items
}

// askStage builds one stage's conversation and runs it through askJSON — the
// same JSON-repair path every other model call in the product uses.
func (s *Server) askStage(ctx context.Context, rc *runContext, unit string,
	st *recipe.Stage, items []stageItem, offset int) ([]stageItem, error) {

	if err := rc.spend(1); err != nil {
		return nil, err
	}
	// when the sources have been extracted, a stage that asks for that
	// inventory is starting from an analyzed baseline rather than raw source
	// text — worth saying, because it changes what the answer means
	if line := rc.contextNote(st, unit); line != "" {
		rc.note(line)
	}
	filter := rc.rec.FilterFor(st)
	tb, specs, err := rc.tools(ctx, filter)
	if err != nil {
		return nil, err
	}
	msgs := rc.buildMessages(unit, st, items, offset)

	client := s.ai
	if m := st.Model; m != "" {
		client = s.ai.WithModel(m)
	} else if rc.rec.Model != "" {
		client = s.ai.WithModel(rc.rec.Model)
	}

	var out map[string]json.RawMessage
	label := rc.rec.Slug + " " + st.ID + " " + unit
	if err := s.askJSONWith(ai.WithLabel(ctx, label), client, msgs, specs, tb.exec, rc.note, &out); err != nil {
		return nil, err
	}
	raw, ok := out[st.Key]
	if !ok {
		// a stage that legitimately found nothing replies with an empty list;
		// a missing key is a reply about something else entirely
		return nil, nil
	}
	var decoded []map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("stage %s: %q was not a list: %w", st.ID, st.Key, err)
	}
	res := make([]stageItem, 0, len(decoded))
	for _, m := range decoded {
		res = append(res, stageItem{Fields: m, Parent: -1})
	}
	return res, nil
}

// contextNote is the stage's line about the context it is working FROM, when
// that context is actually present. Only the recipe knows what is worth
// saying, so an undeclared `narrate: context` is silence.
func (rc *runContext) contextNote(st *recipe.Stage, unit string) string {
	if !strings.Contains(st.User, "{{extracted}}") && !strings.Contains(st.User, "{{#extracted}}") {
		return ""
	}
	vars := rc.vars(unit, nil, -1)
	if vars["extracted"] == "" {
		return ""
	}
	return st.Line("context", "", vars)
}

// buildMessages renders the stage's system prompt (plus the recipe's
// instructions and the run's focus note) and its `### user` template.
//
// Every heading, blank line and sentence here comes from the recipe document.
// The engine supplies only the VALUES — which is exactly why the built-ins
// could be converted without changing a byte of what the model sees.
func (rc *runContext) buildMessages(unit string, st *recipe.Stage, items []stageItem, offset int) []ai.Message {
	vars := rc.vars(unit, items, offset)

	system := st.Prompt
	if rc.focus != "" && st.FocusNote != "" {
		system += "\n\n" + recipe.Render(st.FocusNote, vars)
	}
	if rc.rec.Instructions != "" {
		system += "\n\nWorkspace drift instructions:\n" + rc.rec.Instructions
	}
	return []ai.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: recipe.Render(st.User, vars)},
	}
}

// vars assembles the substitutions a stage template may name. Everything is
// computed lazily-ish: the expensive ones (link graph, extraction block) are
// only built when the unit actually is a document / the recipe asks.
func (rc *runContext) vars(unit string, items []stageItem, offset int) map[string]string {
	names := make([]string, 0, len(rc.sources))
	for _, src := range rc.sources {
		names = append(names, src.Name)
	}
	sort.Strings(names)
	pretty := make([]string, 0, len(names))
	for _, n := range names {
		pretty = append(pretty, "~"+n)
	}

	vars := map[string]string{
		"sourceList":   strings.Join(pretty, ", "),
		"focus":        rc.focus,
		"docIndex":     rc.docIndex,
		"instructions": rc.rec.Instructions,
		"unit":         unit,
		"index":        strconv.Itoa(offset + 1),
	}
	switch rc.rec.Units {
	case recipe.UnitsDocs:
		vars["doc"] = unit
		vars["docContent"] = rc.files[unit]
		if rc.idx != nil {
			vars["linked"] = rc.idx.linkedBlock(rc.files, unit)
		}
		// the analyzed baseline, when the sources have been extracted
		var b strings.Builder
		for _, n := range names {
			if block := extractionContext(rc.files, rc.report, n); block != "" {
				b.WriteString("\n## ~" + n + "\n" + block + "\n")
			}
		}
		vars["extracted"] = b.String()
	case recipe.UnitsSources:
		vars["source"] = unit
		vars["extracted"] = extractionContext(rc.files, rc.report, unit)
	}
	if rc.linkedOverride != "" {
		vars["linked"] = rc.linkedOverride
	}
	if rc.extractedOverride != "" {
		vars["extracted"] = rc.extractedOverride
	}

	// one upstream item = its fields; several = a numbered list for a batch
	switch {
	case len(items) == 1:
		for k, v := range recipe.ItemVars(items[0].Fields) {
			vars[k] = v
		}
		vars["areaPaths"] = areaPathList(unit, items[0].Fields)
	case len(items) > 1:
		var b strings.Builder
		for i, it := range items {
			fmt.Fprintf(&b, "%d. %s\n", i+1, batchLine(it))
		}
		vars["items"] = b.String()
	}
	return vars
}

// areaPathList renders an item's `paths` as the read-me-first list the
// extraction prompt shows ("- ~source/path" per line).
func areaPathList(source string, fields map[string]any) string {
	raw, ok := fields["paths"].([]any)
	if !ok || len(raw) == 0 {
		return ""
	}
	lines := make([]string, 0, len(raw))
	for _, p := range raw {
		if s, ok := p.(string); ok && s != "" {
			lines = append(lines, "- ~"+source+"/"+s)
		}
	}
	return strings.Join(lines, "\n")
}

// batchLine renders one item of a batch the way the matcher expects it:
// "[group] statement". Falls back to compact JSON for an unfamiliar shape.
func batchLine(it stageItem) string {
	statement, _ := it.Fields["statement"].(string)
	group, _ := it.Fields["__group"].(string)
	if statement != "" {
		if group != "" {
			return "[" + group + "] " + statement
		}
		return statement
	}
	raw, _ := json.Marshal(it.Fields)
	return string(raw)
}

func itemName(it stageItem, i int) string {
	for _, k := range []string{"name", "title", "path", "id"} {
		if v, ok := it.Fields[k].(string); ok && v != "" {
			return v
		}
	}
	return "item " + strconv.Itoa(i+1)
}

