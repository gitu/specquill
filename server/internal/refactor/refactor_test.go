package refactor

import (
	"reflect"
	"strings"
	"testing"
)

func TestRelLink(t *testing.T) {
	cases := []struct{ fromDir, target, want string }{
		{"specs", "specs/y.md", "y.md"},
		{"", "specs/x.md", "specs/x.md"},
		{"requirements", "specs/x.md", "../specs/x.md"},
		{"a/b", "a/c/x.md", "../c/x.md"},
	}
	for _, c := range cases {
		if got := relLink(c.fromDir, c.target); got != c.want {
			t.Errorf("relLink(%q, %q) = %q, want %q", c.fromDir, c.target, got, c.want)
		}
	}
}

func TestRewriteRefs(t *testing.T) {
	const oldP = "specs/venue.md"
	const newP = "specs/venues/venue-ids.md"

	cases := []struct {
		name    string
		docPath string
		doc     string
		changed bool
		want    []string // substrings the result must contain
		wantNot []string // substrings the result must NOT contain
	}{
		{
			name:    "body links: every style becomes relative, anchors kept, externals untouched",
			docPath: "requirements/REQ-001.md",
			doc: strings.Join([]string{
				"# Doc",
				"[abs](/specs/venue.md)",
				"[rel](../specs/venue.md#rules)",
				"[other](other.md)",
				"[ext](https://example.com/specs/venue.md)",
			}, "\n"),
			changed: true,
			want: []string{
				"[abs](../specs/venues/venue-ids.md)",
				"[rel](../specs/venues/venue-ids.md#rules)",
				"[other](other.md)",
				"https://example.com/specs/venue.md",
			},
		},
		{
			name:    "bare relative link from a root document",
			docPath: "notes.md",
			doc:     "[v](specs/venue.md)",
			changed: true,
			want:    []string{"[v](specs/venues/venue-ids.md)"},
		},
		{
			name:    "typed frontmatter links, inline and block lists",
			docPath: "requirements/REQ-001.md",
			doc:     "---\nimplements: [specs/venue.md, specs/other.md]\nsatisfies:\n  - specs/venue.md\n---\n\nbody\n",
			changed: true,
			want: []string{
				"implements: [specs/venues/venue-ids.md, specs/other.md]",
				"- specs/venues/venue-ids.md",
			},
		},
		{
			name:    "frontmatter value with anchor keeps the anchor",
			docPath: "requirements/REQ-001.md",
			doc:     "---\nimplements: [specs/venue.md#rules]\n---\n\nbody\n",
			changed: true,
			want:    []string{"implements: [specs/venues/venue-ids.md#rules]"},
		},
		{
			name:    "legacy driver-map ref: values",
			docPath: "requirements/REQ-001.md",
			doc:     "---\ndrivers:\n  - type: regulation\n    ref: specs/venue.md\n---\n\nbody\n",
			changed: true,
			want:    []string{"ref: specs/venues/venue-ids.md"},
		},
		{
			name:    "longer paths sharing the prefix are untouched",
			docPath: "requirements/REQ-001.md",
			doc:     "---\nimplements: [specs/venue-extra.md]\n---\n\n[x](specs/venue-extra.md)\n",
			changed: false,
			wantNot: []string{newP},
		},
		{
			name:    "doc-relative and leading-slash frontmatter spellings normalize to canonical",
			docPath: "requirements/REQ-001.md",
			doc:     "---\nimplements:\n  - ../specs/venue.md\nmaps_to: [/specs/venue.md#f]\n---\n\nbody\n",
			changed: true,
			want: []string{
				"- specs/venues/venue-ids.md",
				"maps_to: [specs/venues/venue-ids.md#f]",
			},
		},
		{
			name:    "sibling frontmatter spelling matches only from the moved file's folder",
			docPath: "specs/txn.md",
			doc:     "---\nimplements: [venue.md]\n---\n\nbody\n",
			changed: true,
			want:    []string{"implements: [specs/venues/venue-ids.md]"},
		},
		{
			name:    "sibling spelling from another folder stays untouched",
			docPath: "requirements/REQ-001.md",
			doc:     "---\nimplements: [venue.md]\n---\n\nbody\n",
			changed: false,
			wantNot: []string{newP},
		},
		{
			name:    "sibling links resolve from the linking document dir",
			docPath: "specs/txn.md",
			doc:     "[sibling](venue.md)",
			changed: true,
			want:    []string{"[sibling](venues/venue-ids.md)"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := RewriteRefs(c.doc, c.docPath, oldP, newP)
			if changed != c.changed {
				t.Fatalf("changed = %v, want %v (got %q)", changed, c.changed, got)
			}
			if !changed && got != c.doc {
				t.Fatalf("unchanged doc must come back byte-identical, got %q", got)
			}
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("result missing %q:\n%s", w, got)
				}
			}
			for _, w := range c.wantNot {
				if strings.Contains(got, w) {
					t.Errorf("result unexpectedly contains %q:\n%s", w, got)
				}
			}
		})
	}
}

func TestRebaseLinks(t *testing.T) {
	files := map[string]bool{
		"diagrams/flow.excalidraw.png": true,
		"diagrams/arch.mermaid":        true,
		"specs/sibling.md":             true,
		"requirements/REQ-1.md":        true,
	}
	exists := func(rel string) bool { return files[rel] }

	cases := []struct {
		name     string
		from, to string
		doc      string
		changed  bool
		want     []string
		wantNot  []string
	}{
		{
			name: "embedded diagrams re-relativize on a cross-folder move",
			from: "specs/report.md", to: "archive/specs/report.md",
			doc:     "---\ntitle: R\n---\n\n![flow](../diagrams/flow.excalidraw.png)\n[d](../diagrams/arch.mermaid)\n[sib](sibling.md)\n",
			changed: true,
			want: []string{
				"![flow](../../diagrams/flow.excalidraw.png)",
				"[d](../../diagrams/arch.mermaid)",
				"[sib](../../specs/sibling.md)",
			},
		},
		{
			name: "rename in place changes nothing",
			from: "specs/report.md", to: "specs/report-v2.md",
			doc:     "![flow](../diagrams/flow.excalidraw.png)\n",
			changed: false,
		},
		{
			name: "root-relative, external and unresolvable links stay untouched",
			from: "specs/report.md", to: "archive/report.md",
			doc:     "[abs](/requirements/REQ-1.md) [ext](https://x.test/a.png) ![gone](../diagrams/missing.png)\n",
			changed: false,
			want:    []string{"(/requirements/REQ-1.md)", "(https://x.test/a.png)", "(../diagrams/missing.png)"},
		},
		{
			name: "relative frontmatter values normalize to root-relative",
			from: "specs/report.md", to: "archive/specs/report.md",
			doc:     "---\ndiagrams:\n  - ../diagrams/arch.mermaid\nimplements:\n  - requirements/REQ-1.md\n---\n\nx\n",
			changed: true,
			want:    []string{"- diagrams/arch.mermaid", "- requirements/REQ-1.md"},
			wantNot: []string{"../diagrams/arch.mermaid"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := RebaseLinks(c.doc, c.from, c.to, exists)
			if changed != c.changed {
				t.Fatalf("changed = %v, want %v (got %q)", changed, c.changed, got)
			}
			if !changed && got != c.doc {
				t.Fatalf("unchanged doc must come back byte-identical")
			}
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
			for _, w := range c.wantNot {
				if strings.Contains(got, w) {
					t.Errorf("unexpectedly contains %q:\n%s", w, got)
				}
			}
		})
	}
}

func TestReferencingDocs(t *testing.T) {
	files := map[string]string{
		"specs/venue.md":          "# self",
		"requirements/REQ-001.md": "---\nimplements: [specs/venue.md]\n---\n\nx\n",
		"specs/txn.md":            "[v](venue.md)",
		"specs/unrelated.md":      "nothing here",
		"index.md":                "- [Venue](specs/venue.md)",
		"assets/shot.png":         "specs/venue.md", // not markdown
	}
	want := []string{"index.md", "requirements/REQ-001.md", "specs/txn.md"}
	if got := ReferencingDocs(files, "specs/venue.md"); !reflect.DeepEqual(got, want) {
		t.Fatalf("ReferencingDocs = %v, want %v", got, want)
	}
}
