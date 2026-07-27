package auth

import (
	"testing"
	"time"
)

func TestTokenVaultLifecycle(t *testing.T) {
	v := NewTokenVault()
	v.Put("s1", "tok-1")
	v.Put("s2", "tok-2")

	if got, ok := v.Get("s1"); !ok || got != "tok-1" {
		t.Fatalf("get s1: %q %v", got, ok)
	}
	if _, ok := v.Get("missing"); ok {
		t.Fatal("unknown session must miss")
	}
	v.Delete("s1")
	if _, ok := v.Get("s1"); ok {
		t.Fatal("deleted session must miss")
	}
	if v.Len() != 1 {
		t.Fatalf("len after delete: %d", v.Len())
	}
}

// Entries whose session has idled out are reaped; touching one keeps it.
func TestTokenVaultPruneIdle(t *testing.T) {
	v := NewTokenVault()
	v.Put("stale", "tok-stale")
	v.Put("fresh", "tok-fresh")
	v.backdate("stale", time.Hour)
	v.backdate("fresh", time.Hour)

	// a request on "fresh" refreshes its clock — only "stale" should go
	if _, ok := v.Get("fresh"); !ok {
		t.Fatal("fresh entry vanished")
	}
	if n := v.PruneIdle(30 * time.Minute); n != 1 {
		t.Fatalf("pruned %d, want 1", n)
	}
	if _, ok := v.Get("stale"); ok {
		t.Fatal("idle entry survived the sweep")
	}
	if _, ok := v.Get("fresh"); !ok {
		t.Fatal("active entry was swept")
	}
	// nothing idle left to take
	if n := v.PruneIdle(30 * time.Minute); n != 0 {
		t.Fatalf("second sweep took %d", n)
	}
}
