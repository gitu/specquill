package project

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// In-repo project config (v2), read from `<content_root>/.specquill/config.yml`
// on the DEFAULT branch only (D5: a feature branch cannot change reference
// selection until merged). This file is writable by anyone with push access —
// it is stage-3 SELECTION only and can never mint access: references name
// sources that must already be in the deployment's catalog (stage 1).

// Reference selects a granted source for the project.
type Reference struct {
	Source    string   `yaml:"source"`
	Paths     []string `yaml:"paths"`     // optional prefix filters (grounding + sidebar listing)
	Grounding bool     `yaml:"grounding"` // include in speccy context
}

// SourceDef DEFINES a read-only reference repo inside the workspace itself —
// forge-PAT mode only, where there is no server-side catalog. Defining a repo
// here never mints access: every user clones it with their own token, so the
// forge's permissions are the gate. Git repos only (importer kinds stay a
// catalog feature of local deployments).
type SourceDef struct {
	Name          string `yaml:"name"`
	Remote        string `yaml:"remote"`
	DefaultBranch string `yaml:"default_branch"` // default: main
}

// SpeccyConfig tunes the AI assistant per workspace. Instructions are
// appended to the speccy system prompt (structure/content rules);
// .specquill/instructions.md is the file-shaped companion for longer rules.
type SpeccyConfig struct {
	Instructions string `yaml:"instructions"`
}

// DriftConfig tunes source-drift detection per workspace. Everything is
// optional — an absent block means: all selected references, all documents,
// the implicit forge target, the built-in doc cap.
type DriftConfig struct {
	Instructions string   `yaml:"instructions"` // appended to the drift system prompt
	References   []string `yaml:"references"`   // restrict to these selected references (default: all)
	Paths        []string `yaml:"paths"`        // default run scope (folders or files; default: every doc)
	// Targets SELECTS work-item destinations from the server's
	// work_item_targets catalog (catalog mode) — it can never mint access. In
	// forge-PAT mode an entry may instead be an owner/repo path on the forge
	// host, filed with the caller's own PAT.
	Targets []string `yaml:"targets"`
	MaxDocs int      `yaml:"max_docs"` // optional hard cap per run (0 = uncapped, large scopes just loop)
	// Report is this project's standing alignment report document, written
	// PROJECT-relative (a monorepo project's report lands under its own
	// content_root, beside the .specquill/config.yml that declares it).
	// Default: reports/alignment-{date}.md (api.defaultDriftReportPath) —
	// the {date}/{yyyy}/{mm}/{dd} tokens expand at run time, so a day's runs
	// continue one report and the next day starts fresh; a path without them
	// is one standing report. A run may target another one.
	Report string `yaml:"report"`
}

// ManifestEntry declares one workspace in a repository's ROOT
// .specquill/config.yml — the REQ-025 manifest. Name is the stable identity
// component (the `owner/repo#name` spelling); Root the workspace subfolder,
// which need not exist yet (the declaration is the consent, the first
// proposed change creates the folder).
type ManifestEntry struct {
	Name string `yaml:"name"`
	Root string `yaml:"root"`
}

type Config struct {
	Version    int          `yaml:"version"`
	Project    string       `yaml:"project"`
	References []Reference  `yaml:"references"`
	Sources    []SourceDef  `yaml:"sources"` // forge-PAT mode source definitions
	Speccy     SpeccyConfig `yaml:"speccy"`
	Drift      DriftConfig  `yaml:"drift"`
	// Projects, when present, makes this file a workspace MANIFEST: each
	// entry is an openable subproject (REQ-025.1). Without it the repository
	// root is the single workspace and this file is its workspace config.
	Projects []ManifestEntry `yaml:"projects"`
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
// the in-repo selection and the deployment's catalog. It is a pure function — a
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
			warnings = append(warnings, "reference "+r.Source+" is not in the source catalog")
			continue
		}
		refs = append(refs, EffectiveReference{
			Source: r.Source, Kind: kind, Paths: r.Paths, Grounding: r.Grounding,
		})
	}
	return refs, warnings
}

// EffectiveReferencesInRepo resolves references against the config's OWN
// `sources:` definitions — the forge-PAT counterpart of EffectiveReferences,
// where the repo defines its reference set and each user's token bounds what
// they can actually clone. A `sources:` entry without a matching reference is
// still browsable; a reference without a definition becomes a warning.
func EffectiveReferencesInRepo(cfg *Config) (refs []EffectiveReference, warnings []string) {
	if cfg == nil {
		return nil, nil
	}
	defined := map[string]bool{}
	for _, sd := range cfg.Sources {
		if sd.Name != "" && sd.Remote != "" {
			defined[sd.Name] = true
		}
	}
	seen := map[string]bool{}
	for _, r := range cfg.References {
		if r.Source == "" || seen[r.Source] {
			continue
		}
		seen[r.Source] = true
		if !defined[r.Source] {
			warnings = append(warnings, "reference "+r.Source+" has no matching source definition in .specquill/config.yml")
			continue
		}
		refs = append(refs, EffectiveReference{
			Source: r.Source, Kind: "git", Paths: r.Paths, Grounding: r.Grounding,
		})
	}
	return refs, warnings
}
