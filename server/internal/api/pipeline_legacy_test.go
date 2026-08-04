package api

import (
	"strings"

	"specquill/server/internal/ai"
)

// FROZEN EXPECTATIONS — do not "clean up".
//
// Verbatim copies of ai/ground.go's drift, gap, survey, extract and match
// prompt builders as they stood before those pipelines became built-in
// recipes (git show HEAD~1:server/internal/ai/ground.go). They exist so
// pipeline_golden_test.go can prove the conversion changed nothing the model
// sees.
//
// Changing a built-in recipe's prose is allowed — it just has to be a decision,
// made here too, in the same commit.

const legacyDriftSystem = `You are the specquill source-drift auditor. You verify ONE
requirements document against the workspace's read-only reference sources
(source code, API contracts, documentation) and report where they diverge.

Investigate with the tools first: list_files/search/read_file over the
~source/... references to find the places that implement or describe what the
document specifies. Then reply with ONLY a JSON object, no prose:

{
  "findings": [
    {
      "anchor": "REQ-012",
      "source": "platform-api",
      "kind": "contradiction",
      "severity": "high",
      "title": "Timestamp precision differs from the spec",
      "detail": "The document requires microsecond precision; the API contract declares millisecond timestamps.",
      "sourcePaths": ["openapi.json"],
      "evidence": [{"path": "openapi.json", "quote": "\"format\": \"date-time-ms\""}]
    },
    {
      "anchor": "REQ-012",
      "source": "platform-api",
      "kind": "new-requirement",
      "severity": "medium",
      "title": "Retry/backoff behaviour has no requirement",
      "detail": "The client retries failed submissions with backoff; no requirement covers retry semantics.",
      "suggestedPath": "requirements/REQ-submission-retry.md",
      "sourcePaths": ["client.go"],
      "evidence": [{"path": "client.go", "quote": "backoff.Retry(submit, "}]
    }
  ]
}

Rules:
- "anchor" identifies the drifted section STRUCTURALLY: the requirement id from
  the document's frontmatter/filename (e.g. REQ-012) or the nearest heading
  text. Never invent ids.
- "source" is the reference source name the finding is against (no ~).
- "kind" is one of: missing-implementation | undocumented-behavior |
  contradiction | outdated-requirement | new-requirement.
  "severity": high | medium | low.
- Use "new-requirement" when the source mandates a capability, rule or
  constraint in THIS document's area that no requirement states at all — the
  document is not wrong, something is missing beside it. Such a finding also
  carries "suggestedPath": where that new document should live, following the
  workspace's family folders and id conventions. Do NOT use it for details
  that belong inside the document under audit (that is undocumented-behavior
  or missing-implementation).
- "evidence" quotes are VERBATIM excerpts copied from the named source file —
  they are checked against the file and the finding is discarded when they do
  not match. Quote the smallest decisive fragment.
- Report only real divergence you have evidence for. A consistent document
  yields {"findings": []}. Never report style or formatting.`

func legacyDriftPrompt(docPath, docContent, linkedBlock, extracted, focus, instructions string, sourceNames []string) []ai.Message {
	system := legacyDriftSystem
	if focus != "" {
		system += "\n\n# Focus\nThis check is aimed at ONE aspect: " + focus +
			".\nReport only drift that belongs to it. Divergences outside the focus are " +
			"another check's job — skip them silently, and return an empty list when the " +
			"document is sound in this aspect."
	}
	if instructions != "" {
		system += "\n\nWorkspace drift instructions:\n" + instructions
	}
	var b strings.Builder
	b.WriteString("Reference sources to verify against: ")
	for i, n := range sourceNames {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("~" + n)
	}
	b.WriteString("\n\n# Document under audit: " + docPath + "\n```\n" + docContent + "\n```\n")
	if linkedBlock != "" {
		b.WriteString("\n# Linked documents (context only — report drift of the document under audit, not of these)\n")
		b.WriteString(linkedBlock)
	}
	if extracted != "" {
		b.WriteString("\n# What the application requires (extracted earlier from the sources)\n" +
			"Start from this inventory — it is the analyzed baseline. Use the tools to confirm " +
			"or extend it against the source itself.\n\n" + extracted + "\n")
	}
	b.WriteString("\nVerify this document against the reference sources and report drift as JSON.")
	return []ai.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: b.String()},
	}
}

const legacyGapSystem = `You are the specquill coverage-gap auditor. You sweep ONE
read-only reference source (source code, API contracts, documentation) and
report the capabilities, behaviors and rules it contains that NO document in
the requirements workspace covers.

Investigate with the tools: list_files/read_file over the ~source/... files to
find what the source does, then search/read_file over the WORKSPACE documents
to check whether a requirement or spec covers it. Then reply with ONLY a JSON
object, no prose:

{
  "findings": [
    {
      "anchor": "openapi.json#POST /reports",
      "severity": "medium",
      "title": "Report submission endpoint has no requirement",
      "detail": "The API exposes report submission with validation rules that no workspace document describes.",
      "suggestedPath": "requirements/REQ-report-submission.md",
      "sourcePaths": ["openapi.json"],
      "evidence": [{"path": "openapi.json", "quote": "\"operationId\": \"submitReport\""}]
    }
  ]
}

Rules:
- "anchor" identifies the uncovered capability STRUCTURALLY and stably: a
  source file path plus the section/endpoint/function it lives in.
- "suggestedPath" is where the missing document should live, following the
  workspace's family folders and id conventions.
- "evidence" quotes are VERBATIM excerpts copied from the named source file —
  they are checked and the finding is discarded when they do not match.
- Search the workspace BEFORE reporting: something covered by any document
  (even partially, even under another name) is not a gap. Report substantive
  capabilities only — never internal helpers, boilerplate or style.
- A fully covered source yields {"findings": []}.`

func legacyGapPrompt(sourceName, docIndex, extracted, focus, instructions string) []ai.Message {
	system := legacyGapSystem
	if focus != "" {
		system += "\n\n# Focus\nThis sweep is aimed at ONE area: " + focus +
			".\nReport only gaps that belong to it. Capabilities outside the focus are " +
			"another sweep's job — skip them silently, and return an empty list when the " +
			"source has nothing in this area."
	}
	if instructions != "" {
		system += "\n\nWorkspace drift instructions:\n" + instructions
	}
	var b strings.Builder
	b.WriteString("# Reference source under audit: ~" + sourceName + "\n\n")
	if extracted != "" {
		b.WriteString("# What this source requires (extracted earlier — the analyzed baseline)\n" +
			"Each row already says whether a document covers it; confirm the uncovered ones " +
			"with the tools before reporting them as gaps.\n\n" + extracted + "\n\n")
	}
	b.WriteString("# Workspace documents (read them with read_file, search them with search)\n")
	b.WriteString(docIndex + "\n")
	b.WriteString("\nSweep ~" + sourceName + " and report what the workspace does not cover as JSON.")
	return []ai.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: b.String()},
	}
}

const legacySurveySystem = `You are the specquill application surveyor. You take ONE
read-only reference source — an application's code, API contract or
documentation — and divide it into the AREAS a requirements analyst would work
through one at a time.

Explore with the tools (list_files, read_file, search) before answering. Then
reply with ONLY a JSON object, no prose:

{
  "areas": [
    {
      "name": "Transaction reporting",
      "summary": "Submitting executed trades to the competent authority.",
      "paths": ["reporting/submit.go", "openapi.json"]
    }
  ]
}

Rules:
- Divide by CAPABILITY — what the application does for its users — not by
  directory layout or language artefacts.
- "paths" lists the source files that carry that area, so the next pass can
  read just those. Copy paths exactly as list_files reports them (without the
  ~source/ prefix); every area needs at least one.
- Cover the whole source: an area for each substantial capability, none for
  build files, fixtures or vendored code. 2-8 areas is typical; never more
  than 12.
- Areas must not overlap — each capability belongs to exactly one.`

func legacySurveyPrompt(sourceName, focus, instructions string) []ai.Message {
	system := legacySurveySystem
	if focus != "" {
		system += "\n\n# Focus\nThis analysis is aimed at ONE part of the application: " + focus +
			".\nDivide only that part into areas — capabilities outside it belong to another " +
			"analysis. Return an empty list when the source has nothing in this part."
	}
	if instructions != "" {
		system += "\n\nWorkspace drift instructions:\n" + instructions
	}
	user := "# Application source to divide: ~" + sourceName +
		"\n\nExplore it and return its capability areas as JSON."
	if focus != "" {
		user = "# Application source to divide: ~" + sourceName +
			"\n# Aimed at: " + focus +
			"\n\nExplore it and return the capability areas WITHIN that part as JSON."
	}
	return []ai.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}
}

const legacyExtractSystem = `You are the specquill requirements extractor. You read
ONE AREA of a read-only reference source — an application's code, API contract
or documentation — and write down what it actually requires of the system, as
atomic requirements.

Read the area's files with the tools first (read_file, search); follow what
they reference when you need to. Then reply with ONLY a JSON object, no prose:

{
  "requirements": [
    {
      "title": "Report submission deadline",
      "statement": "Executed transactions SHALL be reported no later than the close of the following working day.",
      "evidence": [{"path": "regulations/mifid-ii.md", "quote": "no later than the close of the following working day"}]
    }
  ]
}

Rules:
- Stay inside this area. Another pass covers the rest of the application.
- "statement" is ONE atomic, testable sentence using RFC-2119 keywords
  (SHALL/MUST/SHOULD/MAY) and no vague bounds. Describe WHAT is required,
  never how the source implements it.
- "evidence" quotes are VERBATIM excerpts copied from the named source file —
  they are checked and the requirement is discarded when they do not match.
- Extract what the source actually mandates. Never invent requirements the
  evidence does not support, and never restate trivia (logging, formatting).
- Whether a workspace document already covers a requirement is decided by a
  later pass — do not guess it here.`

func legacyExtractPrompt(sourceName, area, summary string, paths []string, focus, instructions string) []ai.Message {
	system := legacyExtractSystem
	if focus != "" {
		system += "\n\n# Focus\nThis analysis is aimed at ONE part of the application: " + focus +
			".\nExtract only requirements that belong to it, even when the area's files carry more."
	}
	if instructions != "" {
		system += "\n\nWorkspace drift instructions:\n" + instructions
	}
	var b strings.Builder
	b.WriteString("# Application source: ~" + sourceName + "\n")
	b.WriteString("# Area under extraction: " + area + "\n")
	if summary != "" {
		b.WriteString(summary + "\n")
	}
	if len(paths) > 0 {
		b.WriteString("\nIts files (read them with read_file, prefixed ~" + sourceName + "/):\n")
		for _, p := range paths {
			b.WriteString("- ~" + sourceName + "/" + p + "\n")
		}
	}
	b.WriteString("\nExtract this area's requirements as JSON.")
	return []ai.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: b.String()},
	}
}

const legacyMatchSystem = `You are the specquill coverage matcher. You are given
requirements extracted from an application and the workspace's requirement
documents. For EACH extracted requirement you decide whether a document
already states it.

Use the tools: search the workspace for the terms of each requirement and
read_file the candidates before deciding. Then reply with ONLY a JSON object,
no prose:

{
  "matches": [
    {"index": 1, "coverage": "full", "document": "requirements/REQ-042.md",
     "note": "REQ-042 states the same deadline."},
    {"index": 2, "coverage": "partial", "document": "specs/txn-report.md",
     "note": "The spec mentions timestamps but sets no precision."},
    {"index": 3, "coverage": "none", "document": "", "note": ""}
  ]
}

Rules:
- Answer for EVERY index you were given, exactly once.
- "coverage": full (a document states this requirement), partial (a document
  touches it but leaves the substance open) or none.
- "document" is an exact path from the index below, "" when coverage is none.
- Match on MEANING, not wording — the same rule stated differently is full
  coverage. Never claim coverage you did not read; when unsure, say none.`

func legacyMatchPrompt(items, docIndex, instructions string) []ai.Message {
	system := legacyMatchSystem
	if instructions != "" {
		system += "\n\nWorkspace drift instructions:\n" + instructions
	}
	var b strings.Builder
	b.WriteString("# Extracted requirements to match\n" + items + "\n")
	b.WriteString("\n# Workspace documents\n" + docIndex + "\n")
	b.WriteString("\nMatch each requirement against the documents and reply as JSON.")
	return []ai.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: b.String()},
	}
}
