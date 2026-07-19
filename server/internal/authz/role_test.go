package authz

import "testing"

func TestLadderOrder(t *testing.T) {
	if !(None < Viewer && Viewer < Editor && Editor < Maintainer && Maintainer < Admin) {
		t.Fatal("ladder order broken")
	}
}

func TestParseString(t *testing.T) {
	for _, r := range []Role{Viewer, Editor, Maintainer, Admin} {
		if Parse(r.String()) != r {
			t.Fatalf("roundtrip %v", r)
		}
	}
	for _, s := range []string{"", "member", "owner", "ADMIN", "root"} {
		if Parse(s) != None {
			t.Fatalf("Parse(%q) must be None", s)
		}
	}
	if None.String() != "" {
		t.Fatalf("None must render empty, got %q", None.String())
	}
}

func TestFromGitHub(t *testing.T) {
	cases := map[string]Role{
		"admin":    Admin,
		"maintain": Maintainer,
		"write":    Editor,
		"push":     Editor,
		"triage":   Editor,
		"read":     Viewer,
		"pull":     Viewer,
		"":         None,
		"none":     None,
	}
	for perm, want := range cases {
		if got := FromGitHub(perm); got != want {
			t.Errorf("FromGitHub(%q) = %v, want %v", perm, got, want)
		}
	}
}

func TestMax(t *testing.T) {
	if Max(Viewer, Editor) != Editor || Max(Admin, None) != Admin || Max(Maintainer, Maintainer) != Maintainer {
		t.Fatal("Max broken")
	}
}

func TestValidGrant(t *testing.T) {
	for _, s := range []string{"viewer", "editor", "maintainer", "admin"} {
		if !ValidGrant(s) {
			t.Errorf("ValidGrant(%q) = false", s)
		}
	}
	for _, s := range []string{"", "member", "owner"} {
		if ValidGrant(s) {
			t.Errorf("ValidGrant(%q) = true", s)
		}
	}
}
