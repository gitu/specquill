package project

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// In-repo project config (v2), read from `<content_root>/.specquill/config.yml`
// on the DEFAULT branch only (D5: a feature branch cannot change reference
// selection until merged). This file is writable by anyone with push access —
// it is stage-3 SELECTION only and can never mint access: references name
// sources that must already be granted to the tenant (stages 1+2).

// Reference selects a granted source for the project.
type Reference struct {
	Source    string   `yaml:"source"`
	Paths     []string `yaml:"paths"`     // optional prefix filters (grounding only)
	Grounding bool     `yaml:"grounding"` // include in copilot context
}

type Config struct {
	Version    int         `yaml:"version"`
	Project    string      `yaml:"project"`
	References []Reference `yaml:"references"`
}

// ParseConfig parses the in-repo config. Unknown keys (the v1 taxonomy/ui
// keys the web client consumes) are ignored here on purpose.
func ParseConfig(yml string) (*Config, error) {
	cfg := &Config{}
	if err := yaml.Unmarshal([]byte(yml), cfg); err != nil {
		return nil, fmt.Errorf("parse .specquill/config.yml: %w", err)
	}
	return cfg, nil
}

// EffectiveReference is a reference that survived the grant intersection.
type EffectiveReference struct {
	Source    string   `json:"source"`
	Kind      string   `json:"kind"`
	OKF       bool     `json:"okf,omitempty"`
	Paths     []string `json:"paths,omitempty"`
	Grounding bool     `json:"grounding"`
}

// EffectiveReferences is THE stage-3 resolver (plan D5): the intersection of
// the in-repo selection and the tenant's grants. It is a pure function — a
// selection of an ungranted or unknown source becomes a warning, never
// access. kinds maps granted source names to their kind.
func EffectiveReferences(cfg *Config, kinds map[string]string) (refs []EffectiveReference, warnings []string) {
	if cfg == nil {
		return nil, nil
	}
	seen := map[string]bool{}
	for _, r := range cfg.References {
		if r.Source == "" || seen[r.Source] {
			continue
		}
		seen[r.Source] = true
		kind, granted := kinds[r.Source]
		if !granted {
			warnings = append(warnings, "reference "+r.Source+" is not granted to this tenant")
			continue
		}
		refs = append(refs, EffectiveReference{
			Source: r.Source, Kind: kind, Paths: r.Paths, Grounding: r.Grounding,
		})
	}
	return refs, warnings
}
