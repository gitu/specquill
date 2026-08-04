package recipe

import (
	"embed"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// The three pipelines the product has always shipped, now expressed in the
// same form a project's own recipes take. They are embedded rather than read
// from disk so a deployment cannot end up without them, and parsed once at
// first use — a parse failure here is a build-time mistake, so it panics
// rather than degrading a run.
//
//go:embed builtin/*.md
var builtinFS embed.FS

// starterDoc is what "New recipe" writes: a working two-stage pipeline with
// the prompts stubbed out. It lives OUTSIDE builtin/ on purpose — it is a
// template, not a recipe anyone can run, and must not be loaded as one.
//
//go:embed starter.md
var starterDoc string

// Starter returns the template for a new project recipe, named after the slug
// it is being saved as. It parses — a starter that did not would teach the
// wrong shape (TestStarterParses).
func Starter(slug, name string) string {
	if name == "" {
		name = slug
	}
	r := strings.NewReplacer("RECIPE_NAME", name, "RECIPE_SLUG", slug)
	return r.Replace(starterDoc)
}

// BuiltinSlugs are the modes the run API accepts as `mode:`, in the order the
// UI shows them.
var BuiltinSlugs = []string{"drift", "gaps", "extract"}

var (
	builtinOnce sync.Once
	builtins    map[string]*Recipe
	builtinErr  error
)

func loadBuiltins() {
	builtins = map[string]*Recipe{}
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		builtinErr = err
		return
	}
	for _, e := range entries {
		content, err := builtinFS.ReadFile("builtin/" + e.Name())
		if err != nil {
			builtinErr = err
			return
		}
		slug := e.Name()[:len(e.Name())-len(".md")]
		r, _, err := Parse(slug, string(content))
		if err != nil {
			builtinErr = fmt.Errorf("built-in recipe %s: %w", slug, err)
			return
		}
		r.Builtin = true
		builtins[slug] = r
	}
	for _, slug := range BuiltinSlugs {
		if _, ok := builtins[slug]; !ok {
			builtinErr = fmt.Errorf("built-in recipe %s is missing", slug)
			return
		}
	}
}

// Builtin returns one shipped recipe. The returned pointer is SHARED — the
// runner treats recipes as read-only and clones before overriding anything.
func Builtin(slug string) (*Recipe, bool) {
	builtinOnce.Do(loadBuiltins)
	if builtinErr != nil {
		panic("recipe: " + builtinErr.Error())
	}
	r, ok := builtins[slug]
	return r, ok
}

// Builtins returns every shipped recipe, in display order.
func Builtins() []*Recipe {
	builtinOnce.Do(loadBuiltins)
	if builtinErr != nil {
		panic("recipe: " + builtinErr.Error())
	}
	out := make([]*Recipe, 0, len(builtins))
	for _, slug := range BuiltinSlugs {
		out = append(out, builtins[slug])
	}
	// anything shipped but not listed still shows up, after the known ones
	var extra []string
	for slug := range builtins {
		if !IsBuiltin(slug) {
			extra = append(extra, slug)
		}
	}
	sort.Strings(extra)
	for _, slug := range extra {
		out = append(out, builtins[slug])
	}
	return out
}

// IsBuiltin reports whether a slug names a shipped recipe.
func IsBuiltin(slug string) bool {
	for _, s := range BuiltinSlugs {
		if s == slug {
			return true
		}
	}
	return false
}

// Clone returns a deep-enough copy for a run to override without touching the
// shared built-in: the slices a run replaces (sources, paths) are copied, the
// prompt strings are immutable and shared.
func (r *Recipe) Clone() *Recipe {
	out := *r
	out.Sources = append([]string(nil), r.Sources...)
	out.Paths = append([]string(nil), r.Paths...)
	out.Findings = append([]FindingKind(nil), r.Findings...)
	out.Stages = append([]Stage(nil), r.Stages...)
	out.Files.Include = append([]string(nil), r.Files.Include...)
	out.Files.Exclude = append([]string(nil), r.Files.Exclude...)
	return &out
}
