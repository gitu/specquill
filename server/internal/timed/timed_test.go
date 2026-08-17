package timed

import (
	"strings"
	"testing"
	"time"
)

var today = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

var files = map[string]string{
	// pending, its only spec still draft → at risk
	"requirements/REQ-042.md": "---\nid: REQ-042\ntitle: Transaction reporting\nstatus: in_review\nstarts: 2026-09-01\n---\n",
	"specs/txn.md":            "---\ntitle: Txn spec\nstatus: draft\nimplements:\n  - requirements/REQ-042.md\n---\n",
	// pending with everything ready
	"requirements/REQ-050.md": "---\nid: REQ-050\ntitle: Ready\nstatus: approved\nstarts: 2026-08-10\n---\n",
	"specs/ready.md":          "---\ntitle: Ready spec\nstatus: approved\nimplements: [requirements/REQ-050.md]\n---\n",
	// regulatory wording, in force with no end → active
	"regulations/mifid.md": "---\nid: REG-1\ntitle: MiFID II\nstatus: active\neffective_from: 2026-01-03\n---\n",
	// expiring and expired
	"requirements/REQ-090.md": "---\nid: REQ-090\ntitle: Retention\nstatus: approved\nends: 2026-09-10\n---\n",
	"requirements/REQ-070.md": "---\nid: REQ-070\ntitle: Venue\nstatus: approved\nends: 2026-07-20\n---\n",
	// no window at all
	"requirements/REQ-099.md": "---\nid: REQ-099\ntitle: Untimed\nstatus: draft\n---\n",
	// not markdown / no frontmatter — must not blow up
	"notes.txt":    "hello",
	"stray.md":     "no frontmatter here\n",
	"index.md":     "---\nokf_version: \"0.1\"\n---\n\n# Index\n",
	".specquill/x": "",
}

func byPath(items []Item) map[string]Item {
	m := map[string]Item{}
	for _, i := range items {
		m[i.Path] = i
	}
	return m
}

func TestBuildBucketsWindowsAndRollsUpDependents(t *testing.T) {
	items := Build(files, "", today)
	if len(items) != 5 {
		t.Fatalf("want the 5 documents with a window, got %d: %+v", len(items), items)
	}
	m := byPath(items)

	req := m["requirements/REQ-042.md"]
	if req.State != "pending" || req.Days != 31 || req.StartKey != "starts" {
		t.Fatalf("pending: %+v", req)
	}
	if len(req.Deps) != 1 || req.Deps[0].Path != "specs/txn.md" || req.Deps[0].Ready {
		t.Fatalf("dependents: %+v", req.Deps)
	}
	if !req.AtRisk {
		t.Fatal("a window opening in 31d behind a draft spec is at risk")
	}
	if ready := m["requirements/REQ-050.md"]; ready.AtRisk || ready.ReadyCount != 1 {
		t.Fatalf("everything ready: %+v", ready)
	}
	if reg := m["regulations/mifid.md"]; reg.State != "active" || reg.StartKey != "effective_from" {
		t.Fatalf("regulatory wording: %+v", reg)
	}
	if exp := m["requirements/REQ-090.md"]; exp.State != "expiring" || exp.Days != 40 {
		t.Fatalf("expiring: %+v", exp)
	}
	if gone := m["requirements/REQ-070.md"]; gone.State != "expired" || gone.Days != -12 {
		t.Fatalf("expired: %+v", gone)
	}
	// pending first, soonest first
	if items[0].Path != "requirements/REQ-050.md" || items[1].Path != "requirements/REQ-042.md" {
		t.Fatalf("order: %s %s", items[0].Path, items[1].Path)
	}
}

func TestConfigOverridesKeysStatusesHorizonAndKinds(t *testing.T) {
	cfg := strings.Join([]string{
		"timed:",
		"  start: [go_live]",
		"  ready_statuses: [shipped]",
		"  horizon_days: 10",
		"  kinds: [requirement]",
	}, "\n")
	tf := map[string]string{
		"requirements/R1.md": "---\nid: R1\ntitle: Custom\nstatus: draft\ngo_live: 2026-08-05\n---\n",
		"specs/s1.md":        "---\ntitle: Impl\nstatus: shipped\nimplements: [requirements/R1.md]\n---\n",
		// out of `kinds`, and its key is not configured as a start any more
		"specs/s2.md": "---\ntitle: Other\nstatus: draft\nstarts: 2026-08-02\n---\n",
	}
	items := Build(tf, cfg, today)
	if len(items) != 1 || items[0].Path != "requirements/R1.md" {
		t.Fatalf("kinds/keys not honored: %+v", items)
	}
	it := items[0]
	if it.StartKey != "go_live" || it.ReadyCount != 1 || !it.Deps[0].Ready {
		t.Fatalf("custom ready status: %+v", it)
	}
	if !it.AtRisk { // the document itself is draft
		t.Fatal("want at risk: the timed document is not ready itself")
	}
	// outside the 10-day horizon nothing is at risk yet
	if early := Build(tf, cfg, today.AddDate(0, 0, -30)); early[0].AtRisk {
		t.Fatalf("horizon ignored: %+v", early[0])
	}
	// a config naming only horizon_days keeps the built-in key lists
	def := ParseDef("timed:\n  horizon_days: 5\n")
	if def.HorizonDays != 5 || len(def.Start) != 3 || def.ReadyStatuses[0] != "approved" {
		t.Fatalf("partial config: %+v", def)
	}
}

func TestTextNamesUnfinishedDependentsAndEmptyCase(t *testing.T) {
	out := Text(Build(files, "", today), Defaults(), today)
	for _, want := range []string{
		"PENDING", "requirements/REQ-042.md", "AT RISK", "not ready: specs/txn.md (draft)", "EXPIRED",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	empty := Text(Build(map[string]string{"requirements/R.md": "---\ntitle: X\n---\n"}, "", today), Defaults(), today)
	if !strings.Contains(empty, "starts/effective_from/valid_from") {
		t.Fatalf("empty case must name the keys: %s", empty)
	}
}

// The SPA counts untyped body links as backlinks ("in text"); the assistant's
// reading must agree, or it under-reports what is waiting on a deadline.
func TestBodyLinksCountAsDependents(t *testing.T) {
	tf := map[string]string{
		"requirements/REQ-095.md": "---\nid: REQ-095\ntitle: Incidents\nstatus: draft\nstarts: 2026-08-20\n---\n",
		// prose-only citation, relative path, plus an anchor
		"specs/incident.md": "---\ntitle: Incident spec\nstatus: draft\n---\n\nSee [REQ-095](../requirements/REQ-095.md#windows).\n",
		// a fenced code sample must NOT fabricate a dependent
		"specs/sample.md": "---\ntitle: Sample\nstatus: draft\n---\n\n```md\n[x](../requirements/REQ-095.md)\n```\n",
		// generated listings are not dependents
		"index.md": "---\nokf_version: \"0.1\"\n---\n\n- [Incidents](requirements/REQ-095.md)\n",
	}
	items := Build(tf, "", today)
	if len(items) != 1 {
		t.Fatalf("items: %+v", items)
	}
	if len(items[0].Deps) != 1 || items[0].Deps[0].Path != "specs/incident.md" {
		t.Fatalf("body-link dependents: %+v", items[0].Deps)
	}
	if !items[0].AtRisk {
		t.Fatal("draft dependent inside the horizon is at risk")
	}
}
