package recipe

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		// a pattern without a slash matches the basename anywhere
		{"*.kt", "Model.kt", true},
		{"*.kt", "app/model/Model.kt", true},
		{"*.kt", "app/model/Model.java", false},
		{"*Model.kt", "app/UserModel.kt", true},
		{"*Model.kt", "app/User.kt", false},

		// ** spans whole segments, including none
		{"**/model/**", "model/User.kt", true},
		{"**/model/**", "app/src/model/User.kt", true},
		{"**/model/**", "app/src/model/nested/User.kt", true},
		{"**/model/**", "app/src/models/User.kt", false},
		{"app/**", "app/User.kt", true},
		{"app/**", "app/a/b/c/User.kt", true},
		{"app/**", "lib/User.kt", false},
		{"**/*.kt", "User.kt", true},
		{"**/model/**/*.kt", "app/model/x/User.kt", true},
		{"**/model/**/*.kt", "app/model/User.kt", true},
		{"**/model/**/*.kt", "app/model/User.java", false},

		// * does NOT cross a separator
		{"app/*.kt", "app/User.kt", true},
		{"app/*.kt", "app/model/User.kt", false},
		{"app/*/User.kt", "app/model/User.kt", true},
		{"app/*/User.kt", "app/a/b/User.kt", false},

		// ? is exactly one character, within one segment
		{"app/User?.kt", "app/User1.kt", true},
		{"app/User?.kt", "app/User.kt", false},
		{"app/User?.kt", "app/User12.kt", false},
		{"a?c/x.kt", "a/c/x.kt", false},

		// case-sensitive: git paths are
		{"**/Model.kt", "app/model.kt", false},

		// degenerate
		{"", "anything", false},
		{"**", "a/b/c", true},
		{"**/", "a/b/c", true},
	}
	for _, c := range cases {
		if got := match(c.pattern, c.path); got != c.want {
			t.Errorf("match(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

// matchSegment is the inner two-pointer walk; a pattern of many stars against
// a long non-matching name must still terminate promptly.
func TestMatchSegmentBacktracking(t *testing.T) {
	if matchSegment("*a*a*a*a*a*a*b", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaac") {
		t.Fatal("expected no match")
	}
	if !matchSegment("*a*b*c*", "xxaxxbxxcxx") {
		t.Fatal("expected a match")
	}
}

func TestFileFilterMatchAndApply(t *testing.T) {
	f := FileFilter{
		Include: []string{"**/model/**/*.kt", "**/*Model.kt"},
		Exclude: []string{"**/test/**", "**/generated/**"},
	}
	keep := []string{"app/model/User.kt", "app/UserModel.kt", "app/model/deep/Order.kt"}
	drop := []string{
		"app/test/model/UserTest.kt", // excluded wins over included
		"app/generated/Model.kt",
		"app/service/Service.kt", // matches no include
		"app/model/User.java",
	}
	for _, p := range keep {
		if !f.Match(p) {
			t.Errorf("expected %q to be kept", p)
		}
	}
	for _, p := range drop {
		if f.Match(p) {
			t.Errorf("expected %q to be dropped", p)
		}
	}

	files := map[string]string{}
	for _, p := range append(append([]string{}, keep...), drop...) {
		files[p] = "content of " + p
	}
	out := f.Apply(files)
	if len(out) != len(keep) {
		t.Fatalf("Apply kept %d files, want %d: %v", len(out), len(keep), out)
	}
	// the input snapshot is shared and must never be mutated
	if len(files) != len(keep)+len(drop) {
		t.Fatalf("Apply mutated its input: %d entries left", len(files))
	}
}

// No include patterns means "everything not excluded" — an exclude-only
// filter is the common case ("audit everything except the tests").
func TestFileFilterExcludeOnly(t *testing.T) {
	f := FileFilter{Exclude: []string{"**/test/**"}}
	if !f.Match("app/model/User.kt") {
		t.Error("exclude-only filter should keep unmatched files")
	}
	if f.Match("app/test/UserTest.kt") {
		t.Error("exclude-only filter should drop excluded files")
	}
}

// An empty filter must return the SAME map, not a copy: every unit of every
// run passes through here and most recipes filter nothing.
func TestFileFilterEmptyPassesThrough(t *testing.T) {
	var f FileFilter
	if !f.Empty() {
		t.Fatal("zero FileFilter should report Empty")
	}
	files := map[string]string{"a.kt": "x"}
	if got := f.Apply(files); len(got) != 1 {
		t.Fatalf("empty filter changed the snapshot: %v", got)
	}
	if (FileFilter{Describe: "anything"}).Empty() {
		t.Error("a describe-only filter is not empty")
	}
}

func TestFileFilterMerge(t *testing.T) {
	base := FileFilter{
		Include:  []string{"**/*.kt"},
		Exclude:  []string{"**/test/**"},
		Describe: "entities",
	}
	// a stage naming only include inherits the rest
	got := base.merge(&FileFilter{Include: []string{"**/model/**"}})
	if len(got.Include) != 1 || got.Include[0] != "**/model/**" {
		t.Errorf("include not overridden: %v", got.Include)
	}
	if len(got.Exclude) != 1 || got.Exclude[0] != "**/test/**" {
		t.Errorf("exclude not inherited: %v", got.Exclude)
	}
	if got.Describe != "entities" {
		t.Errorf("describe not inherited: %q", got.Describe)
	}
	// nil stage filter = the recipe's own
	if got := base.merge(nil); got.Describe != "entities" || len(got.Include) != 1 {
		t.Errorf("nil override changed the filter: %+v", got)
	}
}
