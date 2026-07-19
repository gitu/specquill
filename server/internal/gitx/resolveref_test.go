package gitx

import (
	"testing"

	"specquill/server/internal/config"
)

// The exported ResolveRef is fail-safe: it never hands an injectable ref back
// to a caller. Empty and syntactically invalid refs both collapse to the
// default branch; only well-formed refs pass through unchanged.
func TestResolveRefFailSafe(t *testing.T) {
	r := &Repo{Cfg: config.RepoConfig{DefaultBranch: "main"}}
	cases := map[string]string{
		"":            "main", // empty → default
		"feature/x":   "feature/x",
		"main":        "main",
		"-D":          "main", // option lookalike → default, not verbatim
		"a..b":        "main", // ref-range traversal
		"../evil":     "main",
		"x; rm -rf /": "main", // metacharacters
	}
	for in, want := range cases {
		if got := r.ResolveRef(in); got != want {
			t.Errorf("ResolveRef(%q) = %q, want %q", in, got, want)
		}
	}
}
