package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"specquill/server/internal/project"
	"specquill/server/internal/recipe"
)

// The alignment recipe endpoints: what pipelines this branch offers, and what
// one would cost before you set it going.
//
// The dry run matters more than it looks. A recipe multiplies stages by items
// by units, so the difference between a good one and a typo is measured in
// wall-clock and money — and the author cannot see it by reading the YAML. It
// resolves the file filters for real (the glob half is free; the describe half
// costs one model call per source) and reports the projected call count against
// the deployment's ceiling.

// recipeWire is one row of the recipe picker.
func recipeWire(rec *recipe.Recipe, warnings []string) map[string]any {
	stages := make([]map[string]any, 0, len(rec.Stages))
	for _, st := range rec.Stages {
		stages = append(stages, map[string]any{
			"id": st.ID, "label": st.Label, "over": st.Over, "produces": st.Produces,
			"batch": st.Batch, "model": st.Model,
		})
	}
	kinds := make([]map[string]any, 0, len(rec.Findings))
	for _, k := range rec.Findings {
		kinds = append(kinds, map[string]any{
			"kind": k.Kind, "label": k.Label, "severity": k.Severity, "draftable": k.Draftable,
		})
	}
	if warnings == nil {
		warnings = []string{}
	}
	return map[string]any{
		"slug": rec.Slug, "name": rec.Name, "description": rec.Description,
		"builtin": rec.Builtin, "path": rec.Path, "units": rec.Units, "output": rec.Output,
		"model": rec.Model, "sources": rec.Sources, "paths": rec.Paths,
		"stages": stages, "findings": kinds, "warnings": warnings,
		"files": map[string]any{
			"include": rec.Files.Include, "exclude": rec.Files.Exclude, "describe": rec.Files.Describe,
		},
	}
}

// GET /api/repos/{repo}/alignment/recipes?branch= — the pipelines this branch
// offers: the shipped ones plus the project's own, with whatever failed to load
// reported rather than silently missing.
func (s *Server) getRecipes(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	out := map[string]any{"dir": recipe.Dir}
	if s.ai != nil {
		out["models"] = s.ai.Models()
		out["maxCallsPerRun"] = s.ai.MaxCallsPerRun()
	} else {
		out["models"] = []string{}
		out["maxCallsPerRun"] = 0
	}
	list := make([]map[string]any, 0, 8)
	for _, rec := range recipe.Builtins() {
		list = append(list, recipeWire(rec, nil))
	}
	errs := map[string]string{}
	if files, err := repo.Snapshot(branch); err == nil {
		recipes, warnings, loadErrs := recipe.LoadAll(files)
		for _, rec := range recipes {
			list = append(list, recipeWire(rec, warnings[rec.Slug]))
		}
		errs = loadErrs
	}
	out["recipes"] = list
	out["errors"] = errs
	jsonOK(w, out)
}

// POST /api/repos/{repo}/alignment/recipes/validate?branch=
// {recipe?, content?} — parse, validate and PROJECT the cost of a run without
// starting one. No findings, no writes.
func (s *Server) postRecipeValidate(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	var body struct {
		Recipe string `json:"recipe"`
		// Content validates an UNSAVED draft — the editor can check a recipe
		// before committing it.
		Content string   `json:"content"`
		Sources []string `json:"sources"`
		Paths   []string `json:"paths"`
		Focus   string   `json:"focus"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	files, err := repo.Snapshot(branch)
	if err != nil {
		gitFail(w, err)
		return
	}
	var rec *recipe.Recipe
	var warnings []string
	if strings.TrimSpace(body.Content) != "" {
		slug := body.Recipe
		if slug == "" {
			slug = "draft"
		}
		rec, warnings, err = recipe.Parse(slug, body.Content)
	} else {
		rec, err = s.resolveRecipe(body.Recipe, files)
	}
	if err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error(), "warnings": warnings})
		return
	}
	if s.ai != nil {
		if err := rec.ValidateModels(s.ai.AllowedModels()); err != nil {
			jsonOK(w, map[string]any{"ok": false, "error": err.Error(), "warnings": warnings})
			return
		}
	}

	var driftCfg project.DriftConfig
	if cfg := inRepoConfig(repo, branch); cfg != nil {
		driftCfg = cfg.Drift
	}
	sources := s.driftSources(r, repo, branch, driftCfg)
	pick := body.Sources
	if len(pick) == 0 {
		pick = rec.Sources
	}
	if len(pick) > 0 {
		want := map[string]bool{}
		for _, n := range pick {
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

	// units: the run's outer loop, exactly as postDriftRun resolves it
	var units []string
	if rec.Units == recipe.UnitsSources {
		for _, src := range sources {
			units = append(units, src.Name)
		}
		sort.Strings(units)
	} else {
		scope := body.Paths
		if len(scope) == 0 {
			scope = rec.Paths
		}
		units = resolveDriftScope(files, scope, driftCfg.Paths)
	}

	// what each stage's filter actually keeps — the glob half, which is free.
	// The describe half needs a model call per source and is reported as an
	// estimate rather than resolved here.
	perSource := map[string]map[string]int{}
	for i := range rec.Stages {
		st := &rec.Stages[i]
		filter := rec.FilterFor(st)
		if filter.Empty() {
			continue
		}
		counts := map[string]int{}
		for _, src := range sources {
			counts[src.Name] = len(filter.Apply(src.Files))
		}
		perSource[st.ID] = counts
	}

	// the projection: one call per unit for a `over: unit` stage, and for a
	// fan-out stage one per upstream item — which nobody can know before the
	// run, so the estimate assumes a typical fan-out and SAYS it is an estimate.
	const assumedFanOut = 6
	stages := make([]map[string]any, 0, len(rec.Stages))
	calls, estimated := 0, false
	perUnit := map[string]int{}
	for i := range rec.Stages {
		st := &rec.Stages[i]
		n := 1
		switch {
		case st.Over == "unit":
			n = 1
		case st.Batch > 0:
			n = 1 // a batch stage folds its upstream items into few calls
			if up := perUnit[st.Over]; up > 0 {
				n = (up*assumedFanOut + st.Batch - 1) / st.Batch
			}
			estimated = true
		default:
			n = max(perUnit[st.Over], 1) * assumedFanOut
			estimated = true
		}
		if st.Max > 0 && n > st.Max {
			n = st.Max
		}
		perUnit[st.ID] = n
		calls += n * len(units)
		row := map[string]any{
			"id": st.ID, "label": st.Label, "over": st.Over, "produces": st.Produces,
			"callsPerUnit": n, "calls": n * len(units),
		}
		if counts, ok := perSource[st.ID]; ok {
			row["files"] = counts
		}
		if rec.FilterFor(st).Describe != "" {
			calls += len(sources) // one selection pass per source
			row["describeCalls"] = len(sources)
		}
		stages = append(stages, row)
	}

	ceiling := 0
	if s.ai != nil {
		ceiling = s.ai.MaxCallsPerRun()
	}
	res := map[string]any{
		"ok": true, "recipe": recipeWire(rec, warnings), "warnings": warnings,
		"units": len(units), "unitKind": rec.Units, "unitList": firstN(units, 20),
		"sources": len(sources), "stages": stages,
		"estimatedCalls": calls, "estimated": estimated, "maxCallsPerRun": ceiling,
	}
	switch {
	case len(units) == 0:
		res["ok"] = false
		res["error"] = "nothing in scope — no " + rec.Units + " match this recipe"
	case ceiling > 0 && calls > ceiling:
		res["overCeiling"] = true
		res["note"] = fmt.Sprintf("about %d model calls, over the %d-call ceiling — "+
			"the run would stop early and need resuming", calls, ceiling)
	}
	jsonOK(w, res)
}

func firstN(list []string, n int) []string {
	if len(list) <= n {
		return list
	}
	return list[:n]
}
