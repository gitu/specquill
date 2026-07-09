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
