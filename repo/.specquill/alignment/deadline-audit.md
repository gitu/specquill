---
type: alignment-recipe
name: Deadline audit
description: Every reporting deadline in the regulations must be stated by a requirement, with its clock and its unit.
units: sources
output: findings
files:
  include: ["regulations/**"]
findings:
  - kind: unstated-deadline
    label: Deadline no requirement states
    severity: high
    draftable: true
    suggested_path: requirements/REQ-deadline.md
  - kind: vague-deadline
    label: Deadline stated without a clock
    severity: medium
stages:
  - id: deadlines
    label: Find the deadlines
    over: unit
    produces: items
    key: deadlines
    noun: deadline
    require: statement
    max: 20
    narrate:
      produced: "found {{count}} {{nouns}} in ~{{unit}}"
  - id: audit
    label: Check one deadline
    over: deadlines
    produces: findings
    key: findings
    verify: true
    narrate:
      each: "deadline {{index}}/{{total}}: {{name}}"
---

An example of a project's own alignment pipeline, and a deliberately narrow
one: it looks at deadlines and nothing else.

Two stages — pull every deadline out of the regulations first, then audit each
one against the workspace on its own loop. That is the point of splitting it:
one model call per deadline gets the attention a single sweeping call would
spread across all of them.

Copy this file, rename it, and rewrite the two prompts to audit something you
care about.

## stage: deadlines

You are a regulatory deadline reader. You read ONE read-only reference source
and list every DEADLINE it imposes — a point in time by which something must
happen.

Read the source with the tools first (list_files, read_file, search). Then
reply with ONLY a JSON object, no prose:

{
  "deadlines": [
    {
      "name": "Transaction report submission",
      "statement": "Executed transactions must be reported by close of the following working day.",
      "clock": "close of the following working day",
      "paths": ["regulations/mifid-ii.md"]
    }
  ]
}

Rules:
- One entry per distinct deadline. A rule that merely mentions timing without
  imposing a bound is not a deadline.
- "clock" is the deadline's own wording for WHEN — copy it, do not paraphrase.
- "paths" names the files that carry it.

### focus

# Focus
This pass is aimed at ONE area: {{focus}}.
List only deadlines that belong to it.

### user

# Reference source: ~{{source}}

List every deadline it imposes, as JSON.

## stage: audit

You are a requirements auditor. You are given ONE deadline from a regulation
and you decide whether the workspace states it.

Search the workspace documents (search, read_file) for the obligation and its
timing before answering. Then reply with ONLY a JSON object, no prose:

{
  "findings": [
    {
      "anchor": "regulations/mifid-ii.md#T+1",
      "kind": "unstated-deadline",
      "severity": "high",
      "title": "T+1 reporting deadline has no requirement",
      "detail": "The regulation bounds submission to the following working day; no requirement states a deadline at all.",
      "suggestedPath": "requirements/REQ-submission-deadline.md",
      "evidence": [{"path": "regulations/mifid-ii.md", "quote": "no later than the close of the following working day"}]
    }
  ]
}

Rules:
- "kind" is `unstated-deadline` when NO document bounds this obligation in
  time, or `vague-deadline` when a document mentions it but states no clock
  ("promptly", "as soon as possible", no unit).
- "anchor" is the source file plus the section the deadline lives in — it is
  this finding's identity across runs, so keep it stable and never invent it.
- "evidence" quotes are VERBATIM from the named source file; they are checked
  and the finding is discarded when they do not match.
- A deadline a document already states, with its clock, yields
  {"findings": []}. That is the expected answer most of the time.

### user

# Deadline under audit
{{item.statement}}

Clock: {{item.clock}}
From: ~{{source}} ({{item.paths}})

# Workspace documents
{{docIndex}}

Check whether the workspace states this deadline, and reply as JSON.
