---
type: alignment-recipe
name: RECIPE_NAME
description: One sentence on what this pipeline checks, and why it is worth a run.
units: sources
output: findings
files:
  include: ["**/*.md"]
findings:
  - kind: RECIPE_SLUG-finding
    label: Something this audit found
    severity: medium
    draftable: true
stages:
  - id: collect
    label: Collect what to check
    over: unit
    produces: items
    key: items
    noun: item
    require: name
    max: 20
    narrate:
      produced: "found {{count}} {{nouns}} in ~{{unit}}"
  - id: audit
    label: Check one item
    over: collect
    produces: findings
    key: findings
    verify: true
    narrate:
      each: "item {{index}}/{{total}}: {{name}}"
---

Two stages: pull out what to check, then check each one on its own model call.
That split is the point — one call per item gets the attention a single
sweeping call would spread thin.

Rewrite both prompts below. The frontmatter above says how they are wired:
`over: unit` runs once per reference source, `over: collect` runs once per item
the first stage produced. `verify: true` means the engine checks every evidence
quote against the source and silently drops findings whose quotes do not match
— so ask for evidence and quote it verbatim.

## stage: collect

You are ... . Say what this stage reads and what it should return.

Explore with the tools first (list_files, read_file, search). Then reply with
ONLY a JSON object, no prose:

{
  "items": [
    {"name": "Something to check", "paths": ["path/to/file.md"]}
  ]
}

Rules:
- One entry per distinct thing worth checking on its own.
- "name" is short — it is how this item is named in the run's activity feed.
- "paths" names the files that carry it, so the next stage can read just those.

### focus

# Focus
This pass is aimed at ONE area: {{focus}}.
List only what belongs to it.

### user

# Reference source: ~{{source}}

List what should be checked, as JSON.

## stage: audit

You are ... . Say what makes something a finding HERE — that is the decision
this whole recipe exists to make.

Search the workspace (search, read_file) before answering. Then reply with ONLY
a JSON object, no prose:

{
  "findings": [
    {
      "anchor": "path/to/file.md#section",
      "kind": "RECIPE_SLUG-finding",
      "severity": "medium",
      "title": "Short statement of what is wrong",
      "detail": "What the source says and what the workspace does not.",
      "suggestedPath": "requirements/REQ-something.md",
      "evidence": [{"path": "path/to/file.md", "quote": "verbatim excerpt"}]
    }
  ]
}

Rules:
- "anchor" is this finding's identity across runs — a file path plus the
  section it lives in. Keep it stable and never invent it; dismissals are
  remembered by it.
- "evidence" quotes are VERBATIM from the named source file. They are checked,
  and the finding is discarded when they do not match.
- Nothing wrong yields {"findings": []}. That is the expected answer most of
  the time, and saying so keeps the model honest.

### user

# Item under audit
{{item.name}}

From: ~{{source}} ({{item.paths}})

# Workspace documents
{{docIndex}}

Check it against the workspace, and reply as JSON.
