// Package recipe is the alignment engine's pipeline description: what a run
// reads, which prompts it asks in what order, and what it calls a finding.
//
// It exists because drift, gaps and extraction were three hardcoded pipelines
// doing the same shape of work — fan out over units, ask the model, verify the
// evidence, persist. Those three now ship as BUILT-IN recipes (see builtin/),
// and a project adds its own under `.specquill/alignment/*.md`, read from the
// request's branch like every other in-repo config.
//
// A recipe is a markdown document: frontmatter carries the structure, the body
// carries the prompts as prose. That is deliberate — a recipe opens in the
// ordinary document editor, diffs readably in a PR, and needs no bespoke
// editing surface.
//
// This package is pure: parsing, validation and template rendering only. It
// never calls a model and never touches the store — the runner lives in the
// api package, where the tools and the JSON-repair helpers are.
package recipe

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"specquill/server/internal/mdfm"
)

// Dir is where a project's own recipes live, project-relative.
const Dir = ".specquill/alignment/"

// Unit kinds: what ONE iteration of the run's outer loop is. This is also the
// run's resume granularity — the worker is sequential over units and
// drift_runs.docs_done counts them.
const (
	UnitsDocs    = "docs"    // one unit per workspace document (drift)
	UnitsSources = "sources" // one unit per reference source (gaps, extract)
)

// Recipe outputs: what the run produces overall.
const (
	OutputFindings   = "findings"   // rows in drift_findings
	OutputExtraction = "extraction" // an extracted-<source>.md inventory document
)

// Stage products: what ONE stage hands to the next.
const (
	ProducesItems       = "items"       // a list the next stage fans out over
	ProducesFindings    = "findings"    // verified divergences
	ProducesAnnotations = "annotations" // fields stamped onto an upstream stage's items by index
)

// Vars are the substitutions the runner supplies to a stage's `### user`
// template. A stage asks for context by NAMING it — there is no separate
// declaration to keep in sync — and the whitespace around each block belongs
// to the recipe, not to the engine. Anything else a template mentions is left
// verbatim (Render), so prompts can talk about JSON braces freely.
const (
	VarSourceList  = "sourceList"  // the run's reference sources, "~a, ~b"
	VarSource      = "source"      // the source under audit (units: sources)
	VarDoc         = "doc"         // the document path under audit (units: docs)
	VarDocContent  = "docContent"  // that document's content
	VarLinked      = "linked"      // its linked documents, both directions
	VarDocIndex    = "docIndex"    // the workspace's document paths, one per line
	VarExtracted   = "extracted"   // the persisted extraction block, when present
	VarFocus       = "focus"       // the area this run was aimed at
	VarItems       = "items"       // a batch stage's upstream items, numbered
	VarInstruction = "instructions"

	// per-item vars: {{item}} plus {{item.<field>}} of whatever the upstream
	// stage returned (see ItemVars)
	VarItemPrefix = "item"
)

// knownVars gates what a stage template may name — a typo is caught at parse
// time rather than silently rendering an empty block into a prompt.
var knownVars = map[string]bool{
	VarSourceList: true, VarSource: true, VarDoc: true, VarDocContent: true,
	VarLinked: true, VarDocIndex: true, VarExtracted: true, VarFocus: true,
	VarItems: true, VarInstruction: true, VarItemPrefix: true,
	// an area's own fields, supplied by the runner for readability
	"areaPaths": true, "index": true, "unit": true,
	// narration-only counts
	"count": true, "total": true, "from": true, "to": true, "done": true,
	"noun": true, "nouns": true, "name": true, "label": true,
}

// Nouns is the stage's item noun, pluralized for a count.
func (st *Stage) Nouns(n int) string {
	noun := st.Noun
	if noun == "" {
		noun = strings.TrimSuffix(st.Key, "s")
	}
	if n == 1 {
		return noun
	}
	return noun + "s"
}

// Line renders one of the stage's activity-feed templates, falling back to a
// generic phrasing when the recipe declares none.
func (st *Stage) Line(key, fallback string, vars map[string]string) string {
	tpl := st.Narrate[key]
	if tpl == "" {
		tpl = fallback
	}
	if tpl == "" {
		return ""
	}
	return Render(tpl, vars)
}

// KnownVar reports whether the runner supplies a template name.
func KnownVar(name string) bool {
	if knownVars[name] {
		return true
	}
	// {{item.anything}} — the upstream stage decides its own fields
	return strings.HasPrefix(name, VarItemPrefix+".")
}

// FindingKind is what THIS recipe calls a finding. The built-ins declare the
// six kinds the product has always had; a custom recipe declares its own, and
// they flow through the fingerprint, the report and the UI unchanged.
type FindingKind struct {
	Kind  string `yaml:"kind"`
	Label string `yaml:"label"`
	// Severity is the default when the model omits one (high | medium | low).
	Severity string `yaml:"severity"`
	// Draftable enables the reverse-engineer / plan / create actions: the
	// finding's remedy is a NEW document rather than an edit to an existing one.
	Draftable bool `yaml:"draftable"`
	// SuggestedPath is a fallback for where that new document should live when
	// the model proposes none.
	SuggestedPath string `yaml:"suggested_path"`
}

// ReportSpec overrides where this recipe's run reports land. Empty = the
// project's own drift.report:, which stays the default.
type ReportSpec struct {
	Path    string `yaml:"path"`
	Heading string `yaml:"heading"`
}

// Stage is one prompt in the pipeline, run once per item of whatever it fans
// out over.
type Stage struct {
	ID    string `yaml:"id"`
	Label string `yaml:"label"`
	// Over is `unit` (once for the whole unit) or an EARLIER stage's id (once
	// per item that stage produced).
	Over string `yaml:"over"`
	// Produces is items | findings | annotations.
	Produces string `yaml:"produces"`
	// Key is the JSON key the model replies under ("findings", "areas",
	// "requirements", "matches"). Prompts name it in their own words, so the
	// recipe has to say which one to read.
	Key string `yaml:"key"`
	// Batch > 1 groups upstream items into one call instead of one call each
	// (the coverage matcher walks requirements 8 at a time).
	Batch int `yaml:"batch"`
	// Annotates names the stage whose items an `annotations` stage stamps.
	// Defaults to Over.
	Annotates string `yaml:"annotates"`
	// OnError skip = a failed item is noted and skipped, never sinking the
	// unit (how extraction has always treated a bad area). Default fail.
	OnError string      `yaml:"on_error"`
	Model   string      `yaml:"model"`
	Files   *FileFilter `yaml:"files"`
	// Noun names ONE of this stage's items ("area", "requirement"), for the
	// activity feed. Pluralized by adding s.
	Noun string `yaml:"noun"`
	// Require is a field an item must carry to count — the surveyor
	// occasionally returns a nameless area, and an item with no identity is
	// noise, not work.
	Require string `yaml:"require"`
	// Max caps how many items this stage may hand downstream (0 = uncapped).
	// The bound that keeps divide-and-conquer from fanning out forever.
	Max int `yaml:"max"`
	// Verify marks a stage whose items must be backed by VERBATIM evidence
	// from the source. The engine checks every quote against the source
	// snapshot and drops the ones that do not match — a recipe can choose not
	// to ask for evidence, but it can never ask for it and skip the check.
	//
	// It happens as the stage produces items, not at the end, so a later stage
	// never spends a model call on something that was going to be discarded.
	Verify bool `yaml:"verify"`
	// Narrate holds this stage's activity-feed lines. Prose belongs to the
	// recipe like every other sentence it owns — the engine supplies the
	// counts. Keys: produced, each, batch, done. Absent = a generic line.
	Narrate map[string]string `yaml:"narrate"`

	// from the body, not the frontmatter
	Prompt    string `yaml:"-"` // `## stage: <id>` — the system prompt
	FocusNote string `yaml:"-"` // `### focus` — appended to the system prompt when the run is aimed
	User      string `yaml:"-"` // `### user` — the user message template
}

// Recipe is one alignment pipeline.
type Recipe struct {
	Slug        string `yaml:"-"`
	Path        string `yaml:"-"` // where it was read from ("" for built-ins)
	Builtin     bool   `yaml:"-"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	// Units is what the run iterates: docs | sources.
	Units string `yaml:"units"`
	// Output is findings | extraction.
	Output   string        `yaml:"output"`
	Model    string        `yaml:"model"`
	Sources  []string      `yaml:"sources"`
	Paths    []string      `yaml:"paths"`
	Files    FileFilter    `yaml:"files"`
	Findings []FindingKind `yaml:"findings"`
	Report   ReportSpec    `yaml:"report"`
	Stages   []Stage       `yaml:"stages"`

	Instructions string `yaml:"-"` // the `## instructions` body section
}

// Limits. Generous enough that no reasonable recipe hits them, tight enough
// that a runaway one fails at parse time instead of at call 900.
const (
	MaxStages   = 8
	MaxKinds    = 24
	MaxBatch    = 64
	maxPromptKB = 64
)

// Parse reads a recipe document. Warnings are non-fatal problems worth showing
// the author (an unused body section, a stage with no prompt) — the run still
// starts. A returned error means the recipe cannot run at all.
func Parse(slug, content string) (*Recipe, []string, error) {
	fm, body, has := mdfm.Split(content)
	if !has {
		return nil, nil, fmt.Errorf("recipe %s has no frontmatter block", slug)
	}
	var r Recipe
	if err := yaml.Unmarshal([]byte(fm), &r); err != nil {
		return nil, nil, fmt.Errorf("recipe %s: frontmatter does not parse as YAML: %w", slug, err)
	}
	r.Slug = slug
	sections, warnings := parseBody(body)
	r.Instructions = strings.TrimSpace(sections["instructions"])
	for i := range r.Stages {
		st := &r.Stages[i]
		sec, ok := sections["stage: "+st.ID]
		if !ok {
			warnings = append(warnings,
				fmt.Sprintf("stage %q has no `## stage: %s` section — it will run with an empty prompt", st.ID, st.ID))
			continue
		}
		st.Prompt, st.FocusNote, st.User = splitStageSection(sec)
	}
	// a body section naming a stage that does not exist is almost always a
	// rename that missed the frontmatter
	for name := range sections {
		if !strings.HasPrefix(name, "stage: ") {
			continue
		}
		id := strings.TrimPrefix(name, "stage: ")
		if !hasStage(r.Stages, id) {
			warnings = append(warnings, fmt.Sprintf("body section `## stage: %s` matches no declared stage", id))
		}
	}
	if err := r.Validate(); err != nil {
		return nil, warnings, err
	}
	return &r, warnings, nil
}

func hasStage(stages []Stage, id string) bool {
	for _, s := range stages {
		if s.ID == id {
			return true
		}
	}
	return false
}

// parseBody splits the markdown body into its `## ` sections. Everything
// before the first heading is ignored (recipes open with a sentence about
// themselves and that is not a prompt).
func parseBody(body string) (map[string]string, []string) {
	sections := map[string]string{}
	var warnings []string
	var name string
	var buf []string
	flush := func() {
		if name == "" {
			return
		}
		if _, dup := sections[name]; dup {
			warnings = append(warnings, fmt.Sprintf("duplicate body section `## %s` — the last one wins", name))
		}
		sections[name] = strings.Join(buf, "\n")
		buf = nil
	}
	for _, line := range strings.Split(body, "\n") {
		if h, ok := heading(line, "## "); ok {
			flush()
			name = h
			continue
		}
		if name != "" {
			buf = append(buf, line)
		}
	}
	flush()
	return sections, warnings
}

// splitStageSection pulls the `### focus` and `### user` subsections out of a
// stage's body; whatever is left is the system prompt.
func splitStageSection(sec string) (prompt, focus, user string) {
	var current string
	parts := map[string][]string{}
	for _, line := range strings.Split(sec, "\n") {
		if h, ok := heading(line, "### "); ok {
			current = strings.ToLower(h)
			continue
		}
		parts[current] = append(parts[current], line)
	}
	get := func(k string) string { return strings.TrimSpace(strings.Join(parts[k], "\n")) }
	return get(""), get("focus"), get("user")
}

func heading(line, prefix string) (string, bool) {
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	// `### ` must not be read as a `## ` heading
	if prefix == "## " && strings.HasPrefix(line, "### ") {
		return "", false
	}
	return strings.TrimSpace(line[len(prefix):]), true
}

// Validate rejects a recipe that cannot run. Everything it checks is something
// that would otherwise fail deep inside a long run, or — worse — quietly
// produce nothing.
func (r *Recipe) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("recipe %s: name is required", r.Slug)
	}
	switch r.Units {
	case UnitsDocs, UnitsSources:
	case "":
		return fmt.Errorf("recipe %s: units is required (docs or sources)", r.Slug)
	default:
		return fmt.Errorf("recipe %s: units must be %q or %q, not %q", r.Slug, UnitsDocs, UnitsSources, r.Units)
	}
	switch r.Output {
	case OutputFindings, OutputExtraction:
	case "":
		return fmt.Errorf("recipe %s: output is required (findings or extraction)", r.Slug)
	default:
		return fmt.Errorf("recipe %s: output must be %q or %q, not %q", r.Slug, OutputFindings, OutputExtraction, r.Output)
	}
	if len(r.Stages) == 0 {
		return fmt.Errorf("recipe %s: at least one stage is required", r.Slug)
	}
	if len(r.Stages) > MaxStages {
		return fmt.Errorf("recipe %s: %d stages exceeds the limit of %d", r.Slug, len(r.Stages), MaxStages)
	}
	if len(r.Findings) > MaxKinds {
		return fmt.Errorf("recipe %s: %d finding kinds exceeds the limit of %d", r.Slug, len(r.Findings), MaxKinds)
	}

	kinds := map[string]bool{}
	for i, k := range r.Findings {
		if k.Kind == "" {
			return fmt.Errorf("recipe %s: findings[%d] has no kind", r.Slug, i)
		}
		if k.Kind != kebab(k.Kind) {
			return fmt.Errorf("recipe %s: finding kind %q must be lowercase-kebab-case", r.Slug, k.Kind)
		}
		if kinds[k.Kind] {
			return fmt.Errorf("recipe %s: duplicate finding kind %q", r.Slug, k.Kind)
		}
		kinds[k.Kind] = true
		switch k.Severity {
		case "", "high", "medium", "low":
		default:
			return fmt.Errorf("recipe %s: finding kind %q has severity %q (high, medium or low)", r.Slug, k.Kind, k.Severity)
		}
	}
	if r.Output == OutputFindings && len(r.Findings) == 0 {
		return fmt.Errorf("recipe %s: output is findings but no finding kinds are declared", r.Slug)
	}

	seen := map[string]bool{}
	terminal := false
	for i := range r.Stages {
		st := &r.Stages[i]
		if st.ID == "" {
			return fmt.Errorf("recipe %s: stages[%d] has no id", r.Slug, i)
		}
		if st.ID != kebab(st.ID) {
			return fmt.Errorf("recipe %s: stage id %q must be lowercase-kebab-case", r.Slug, st.ID)
		}
		if seen[st.ID] {
			return fmt.Errorf("recipe %s: duplicate stage id %q", r.Slug, st.ID)
		}
		// `over` may only look BACKWARDS — no cycles, no forward references
		switch st.Over {
		case "unit":
		case "":
			return fmt.Errorf("recipe %s: stage %q has no `over` (unit, or an earlier stage id)", r.Slug, st.ID)
		default:
			if !seen[st.Over] {
				return fmt.Errorf("recipe %s: stage %q runs over %q, which is not an earlier stage", r.Slug, st.ID, st.Over)
			}
		}
		seen[st.ID] = true

		switch st.Produces {
		case ProducesItems:
		case ProducesFindings:
			terminal = true
		case ProducesAnnotations:
			target := st.Annotates
			if target == "" {
				target = st.Over
			}
			if target == "unit" || !seen[target] {
				return fmt.Errorf("recipe %s: stage %q annotates %q, which is not an earlier stage", r.Slug, st.ID, target)
			}
		case "":
			return fmt.Errorf("recipe %s: stage %q has no `produces`", r.Slug, st.ID)
		default:
			return fmt.Errorf("recipe %s: stage %q produces %q (items, findings or annotations)", r.Slug, st.ID, st.Produces)
		}
		if st.Key == "" {
			return fmt.Errorf("recipe %s: stage %q has no `key` (the JSON key its reply uses)", r.Slug, st.ID)
		}
		if st.Batch < 0 || st.Batch > MaxBatch {
			return fmt.Errorf("recipe %s: stage %q has batch %d (0-%d)", r.Slug, st.ID, st.Batch, MaxBatch)
		}
		if st.Batch > 0 && st.Over == "unit" {
			return fmt.Errorf("recipe %s: stage %q batches, but runs over the unit (there is nothing to batch)", r.Slug, st.ID)
		}
		switch st.OnError {
		case "", "fail", "skip":
		default:
			return fmt.Errorf("recipe %s: stage %q has on_error %q (fail or skip)", r.Slug, st.ID, st.OnError)
		}
		// a template naming something the runner never supplies would render
		// an empty block into the prompt — catch it here, not in a log
		for _, name := range Placeholders(st.User + "\n" + st.FocusNote) {
			if !KnownVar(name) {
				return fmt.Errorf("recipe %s: stage %q asks for unknown context %q", r.Slug, st.ID, name)
			}
		}
		if len(st.Prompt) > maxPromptKB*1024 {
			return fmt.Errorf("recipe %s: stage %q prompt exceeds %d KB", r.Slug, st.ID, maxPromptKB)
		}
	}
	if r.Output == OutputFindings && !terminal {
		return fmt.Errorf("recipe %s: output is findings but no stage produces them", r.Slug)
	}
	return nil
}

// ValidateModels checks the recipe's model references against the deployment's
// allowlist. Separate from Validate because the allowlist is server config and
// this package stays pure — the api layer supplies it.
func (r *Recipe) ValidateModels(allowed map[string]bool) error {
	check := func(where, m string) error {
		if m == "" || m == "default" || m == "quick" {
			return nil
		}
		if !allowed[m] {
			return fmt.Errorf("recipe %s: %s names model %q, which is not configured "+
				"(add it to ai.models in the server config, or use default/quick)", r.Slug, where, m)
		}
		return nil
	}
	if err := check("model", r.Model); err != nil {
		return err
	}
	for _, st := range r.Stages {
		if err := check("stage "+st.ID, st.Model); err != nil {
			return err
		}
	}
	return nil
}

// Kind looks up a declared finding kind.
func (r *Recipe) Kind(name string) (FindingKind, bool) {
	for _, k := range r.Findings {
		if k.Kind == name {
			return k, true
		}
	}
	return FindingKind{}, false
}

// NormKind maps a model-supplied kind onto one this recipe declares. An
// unrecognised kind degrades to the FIRST declared kind rather than being
// dropped: the finding itself is evidence-verified and real, only its label is
// in doubt.
func (r *Recipe) NormKind(k string) string {
	k = kebab(k)
	if _, ok := r.Kind(k); ok {
		return k
	}
	if len(r.Findings) > 0 {
		return r.Findings[0].Kind
	}
	return k
}

// Draftable reports whether a finding of this kind gets the create-a-document
// actions. Gate the UI on THIS, never on an empty doc_path.
func (r *Recipe) Draftable(kind string) bool {
	k, ok := r.Kind(kind)
	return ok && k.Draftable
}

// Stage looks up a stage by id.
func (r *Recipe) Stage(id string) (*Stage, bool) {
	for i := range r.Stages {
		if r.Stages[i].ID == id {
			return &r.Stages[i], true
		}
	}
	return nil, false
}

// FilterFor resolves the file filter in force for one stage: the recipe's,
// with the stage's own layered over it.
func (r *Recipe) FilterFor(st *Stage) FileFilter {
	return r.Files.merge(st.Files)
}

// LoadAll reads every project recipe out of a branch snapshot, newest parse
// errors and warnings reported per slug rather than failing the lot — one
// broken recipe must not hide the others from the picker.
func LoadAll(files map[string]string) (recipes []*Recipe, warnings map[string][]string, errs map[string]string) {
	warnings, errs = map[string][]string{}, map[string]string{}
	var paths []string
	for p := range files {
		if strings.HasPrefix(p, Dir) && strings.HasSuffix(p, ".md") &&
			!strings.Contains(strings.TrimPrefix(p, Dir), "/") {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		slug := strings.TrimSuffix(strings.TrimPrefix(p, Dir), ".md")
		r, warn, err := Parse(slug, files[p])
		if len(warn) > 0 {
			warnings[slug] = warn
		}
		if err != nil {
			errs[slug] = err.Error()
			continue
		}
		if IsBuiltin(slug) {
			errs[slug] = fmt.Sprintf("recipe %s shadows a built-in — rename it", slug)
			continue
		}
		r.Path = p
		recipes = append(recipes, r)
	}
	return recipes, warnings, errs
}

// kebab normalizes a model-supplied or authored identifier.
func kebab(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}
