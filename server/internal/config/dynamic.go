package config

// Token-scoped dynamic projects (REQ-025): forge-PAT users may open any
// manifest-carrying repository on the deployment's forge that their own token
// can reach. This file holds the opt-in block and its size/duration knobs.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ByteSize parses human byte amounts from YAML: plain integers are bytes,
// strings accept KB/MB/GB/TB (decimal) and KiB/MiB/GiB/TiB (binary) suffixes.
type ByteSize int64

func (b *ByteSize) UnmarshalYAML(node *yaml.Node) error {
	s := strings.TrimSpace(node.Value)
	if s == "" {
		*b = 0
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*b = ByteSize(n)
		return nil
	}
	units := []struct {
		suffix string
		factor int64
	}{
		// binary suffixes first — "GiB" must not match the "B" pass
		{"KiB", 1 << 10}, {"MiB", 1 << 20}, {"GiB", 1 << 30}, {"TiB", 1 << 40},
		{"KB", 1e3}, {"MB", 1e6}, {"GB", 1e9}, {"TB", 1e12}, {"B", 1},
	}
	for _, u := range units {
		if strings.HasSuffix(strings.ToLower(s), strings.ToLower(u.suffix)) {
			num := strings.TrimSpace(s[:len(s)-len(u.suffix)])
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return fmt.Errorf("invalid byte size %q", s)
			}
			*b = ByteSize(f * float64(u.factor))
			return nil
		}
	}
	return fmt.Errorf("invalid byte size %q (use bytes or a KB/MB/GB/KiB/MiB/GiB suffix)", s)
}

// DynamicConfig is the per-deployment opt-in for dynamic projects. Disabled
// by default: without it the deployment behaves exactly like v1 and the
// configured projects are the entire editable surface (REQ-025.1).
type DynamicConfig struct {
	Enabled bool `yaml:"enabled"`
	// Search additionally lets the SERVER proxy forge repository search for
	// the picker (REQ-025.2). Open-by-name works without it — the switch is
	// server-exposure control, not secrecy.
	Search bool `yaml:"search"`
	// UserBudget bounds per-user server-side storage across ALL that user's
	// clones — dynamic projects and the reference clones they pull in alike
	// (REQ-025.5). Default 2GB.
	UserBudget ByteSize `yaml:"user_budget"`
	// IdleAfter is how long a clone may go untouched by any authenticated
	// request before automatic reclamation (REQ-025.6). Default 168h (7d).
	IdleAfter time.Duration `yaml:"idle_after"`
	// UnsyncedRetention is how much longer unsynced state (a dirty worktree,
	// unpushed commits) protects a clone from reclamation. Past it the clone
	// is reclaimed regardless — deliberate, bounded data loss (REQ-025.6).
	// Default 720h (30d) ON TOP of the idle period semantics: an unsynced
	// clone survives until idle exceeds this value.
	UnsyncedRetention time.Duration `yaml:"unsynced_retention"`
}

const (
	defaultUserBudget        = ByteSize(2e9) // 2GB
	defaultIdleAfter         = 168 * time.Hour
	defaultUnsyncedRetention = 720 * time.Hour
)

func (d *DynamicConfig) normalize() {
	if !d.Enabled {
		return
	}
	if d.UserBudget <= 0 {
		d.UserBudget = defaultUserBudget
	}
	if d.IdleAfter <= 0 {
		d.IdleAfter = defaultIdleAfter
	}
	if d.UnsyncedRetention <= 0 {
		d.UnsyncedRetention = defaultUnsyncedRetention
	}
	if d.UnsyncedRetention < d.IdleAfter {
		// the retention cap extends the idle window, never shortens it
		d.UnsyncedRetention = d.IdleAfter
	}
}
