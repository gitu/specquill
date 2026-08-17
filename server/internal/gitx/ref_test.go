package gitx

import "testing"

func TestValidRef(t *testing.T) {
	ok := []string{"main", "ws/flo", "feature/mifid-update", "release-1.2", "a"}
	for _, n := range ok {
		if !ValidRef(n) {
			t.Errorf("%q should be a valid ref", n)
		}
	}
	bad := map[string]string{
		"":                "empty",
		"-":               "bare dash",
		"--output=/tmp/x": "argument injection: git would read it as an option",
		"-upload-pack=id": "argument injection",
		"..":              "traversal — slug() maps refs onto worktree paths",
		"ws/../../etc":    "traversal",
		"ws/flo\nrm -rf":  "control character",
		"ws flo":          "space",
		"ws/flo:x":        "colon",
		"ws/flo^":         "caret",
		"ws/flo~1":        "tilde",
		"ws/flo?":         "glob",
		"ws/flo*":         "glob",
		"ws/flo[":         "glob",
		"ws/flo\\x":       "backslash",
		"ws/flo.lock":     ".lock suffix",
		"ws/flo.":         "trailing dot",
		"/ws/flo":         "leading slash",
		"ws/flo/":         "trailing slash",
		"ws//flo":         "empty component",
		"ws/.hidden":      "component starting with a dot",
		"@":               "bare @",
		"ws/flo@{1}":      "reflog syntax",
	}
	for n, why := range bad {
		if ValidRef(n) {
			t.Errorf("%q should be rejected (%s)", n, why)
		}
	}
}
