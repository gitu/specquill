package main

// Workspace CLI: commands that operate on a plain checkout, no server needed.
//
//	specquill add <type> [name] [-dir .]   new document from the family starter
//	specquill validate [dir]               OKF conformance + link resolution
//	specquill graph [dir] [-format dot]    traceability graph (DOT)
//	specquill export [dir]                 workspace model as JSON

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"specquill/server/internal/docmodel"
	"specquill/server/internal/okf"
	"specquill/server/internal/scaffold"
)

func dirArg(args []string) string {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0]
	}
	return "."
}

func addCmd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	dir := fs.String("dir", ".", "workspace directory")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: specquill add <type> [name] [-dir <workspace>]")
		fmt.Fprintln(os.Stderr, "types: requirement, spec, regulation, data-mapping, change, decision, glossary")
		fs.PrintDefaults()
	}
	if len(args) == 0 {
		fs.Usage()
		return fmt.Errorf("document type required")
	}
	family := args[0]
	rest := args[1:]
	name := ""
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		name = rest[0]
		rest = rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	rel, err := scaffold.Add(*dir, family, name)
	if err != nil {
		return err
	}
	fmt.Println("created", rel)
	return nil
}

func validateCmd(args []string) error {
	root := dirArg(args)
	violations, err := okf.Validate(root)
	if err != nil {
		return err
	}
	docs, err := docmodel.Scan(root)
	if err != nil {
		return err
	}
	broken := docmodel.BrokenLinks(root, docs)

	for _, v := range violations {
		fmt.Println("okf:", v)
	}
	for _, b := range broken {
		fmt.Println("link:", b)
	}
	okfState := "not opted in (no okf_version in root index.md)"
	if okf.Enabled(root) {
		okfState = "opted in"
	}
	fmt.Printf("%d documents · OKF %s · %d conformance violation(s) · %d broken link(s)\n",
		len(docs), okfState, len(violations), len(broken))
	if len(violations)+len(broken) > 0 {
		return fmt.Errorf("validation failed")
	}
	return nil
}

func graphCmd(args []string) error {
	root := dirArg(args)
	docs, err := docmodel.Scan(root)
	if err != nil {
		return err
	}
	byPath := map[string]bool{}
	for _, d := range docs {
		byPath[d.Path] = true
	}
	label := func(d docmodel.Doc) string {
		if d.ID != "" && d.ID != d.Title {
			return d.ID + "\n" + d.Title // %q renders the newline as \n — a DOT line break
		}
		return d.Title
	}
	var b strings.Builder
	b.WriteString("digraph specquill {\n  rankdir=LR;\n  node [shape=box, fontname=\"Helvetica\", fontsize=10];\n")
	for _, d := range docs {
		fmt.Fprintf(&b, "  %q [label=%q];\n", d.Path, label(d))
	}
	edge := func(from, to, field string, dashed bool) {
		t := strings.SplitN(to, "#", 2)[0]
		if !byPath[t] {
			return // prose driver refs, external files
		}
		style := ""
		if dashed {
			style = ", style=dashed"
		}
		fmt.Fprintf(&b, "  %q -> %q [label=%q, fontsize=8%s];\n", from, t, field, style)
	}
	for _, d := range docs {
		for field, targets := range d.Links {
			for _, t := range targets {
				edge(d.Path, t, field, false)
			}
		}
		for _, r := range d.References {
			edge(d.Path, r, "ref", true)
		}
	}
	b.WriteString("}\n")
	fmt.Print(b.String())
	return nil
}

func exportCmd(args []string) error {
	root := dirArg(args)
	docs, err := docmodel.Scan(root)
	if err != nil {
		return err
	}
	out := struct {
		Generator  string         `json:"generator"`
		OKFVersion string         `json:"okf_version,omitempty"`
		Docs       []docmodel.Doc `json:"documents"`
	}{Generator: "specquill", Docs: docs}
	if okf.Enabled(root) {
		out.OKFVersion = okf.Version
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
