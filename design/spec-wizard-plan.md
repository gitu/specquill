---
type: Guide
title: Guided spec authoring (port of nova-spec-creator)
status: implemented
---

# Guided spec authoring — analysis & plan

> **Status: shipped.** Phases 0–3 are implemented — `/wizard` (rail: "Draft
> with Speccy"), `server/internal/{ai,api}/wizard.go`, `web/src/views/
> WizardView.tsx`. Decisions that changed during implementation, corrected in
> place below: the stages are gated on **editor** role, not viewer (§4); the
> wizard defaults to the **requirement** family rather than `entities[0]`;
> and §3.1/§3.2's bespoke JSON-turn helper was dropped in favour of the
> `askJSON` that main grew in the meantime — same contract, one
> implementation, and `ExtractJSON`'s field-matching supersedes the
> buffer-reset it was written to work around. The follow-ups in §5
> (test-case generation, compare-vs-implementation, multimodal intent) remain
> open; note that main's finding→documents planner (`/findings/{fp}/plan`)
> now covers the adjacent "propose a linked SET of documents" case from the
> alignment side.

Porting the *flow* of `nova-core/nova-spec-creator` ("Speccy", FastAPI + React POC)
into specquill as a guided document-authoring wizard: a rail entry that walks
intent → interview → draft, and hands the result to the existing markdown editor
as an uncommitted worktree draft.

## 1. What the source actually is

`nova-spec-creator` is a standalone 4-stage app (`frontend/src/App.tsx`,
`backend/app/{routes,agent,prompts,models}.py`):

| Stage | Endpoint | Mechanism |
| --- | --- | --- |
| **Intent** | — | free text + mode (`business`/`technical`) + pasted images |
| **Related** | `POST /api/specs/related` | AI classifies overlap with existing specs → `covers`/`overlaps`/`related` + a recommendation (`extend SPEC-x` or `new`); human decides |
| **Interview** ("grill") | `POST /api/converse` | agentic loop over read-only repo tools; **structured** result: `reply`, `open_questions[]`, `rubric[{criterion, met}]`, `ready_to_draft` |
| **Draft** | `POST /api/draft` | same loop → `title` + `blocks[]`, one block per section-template entry |
| **Workbench** | `POST /api/block` | per-section editor cards: redraft / tighten / free-text instruction; add / remove / reorder; preview; export or publish |

Everything else in that repo — GitLab publish, `SPEC-NNN` ids, `open/ →
in-progress/ → implemented/` status folders, MR sign-off, branch/MR work
tracking, the admin prompt editor, `.speccy/settings.json` — exists because it
has **no git-native workspace**. specquill *is* that workspace, so those parts
are not ported (§5).

The parts worth porting are exactly three ideas:

1. **A staged flow** instead of an open-ended chat — the user knows where they
   are and what is left.
2. **The readiness rubric** — a running checklist of "what this spec still
   needs", each criterion met/unmet, plus a `ready_to_draft` signal. This is the
   one genuinely new backend concept; it is what makes the grilling feel finite.
3. **Section-templated drafting with per-section refinement** — the model fills
   a named outline, and each section can be re-asked in isolation.

## 2. What specquill already has

The plumbing is largely done, and in most respects better than the source:

| Need | Already in specquill |
| --- | --- |
| Agentic tool loop over an LLM | `ai.StreamTools` (`internal/ai/tools.go`) — streaming, fragment accumulation, round/byte budgets, `halt` semantics |
| Read tools | `speccyToolbox` (`api/speccytools.go`): `read_file` / `list_files` / `search` across the workspace **and every selected reference source** (`~source/path`) — the source's `RepoTools` only reached two hard-coded local checkouts |
| Asking the user | `ask_user` tool + clickable options + stateless resume (`resume` messages replayed by the SPA) |
| Prompt/house rules | `ai.AuthoringRules`: `.specquill/skills/*.md`, `.specquill/instructions.md`, `speccy.instructions`, `.specquill/memory/*.md` — git-versioned, which is what the source's admin UI + `settings.json` were emulating |
| Grounding + budget | `ai.GroundingPrompt` (60% workspace floor, per-source shares) |
| Document families, ID schemes, frontmatter | `lib/entities.ts`, `lib/ids.ts`, `lib/newdoc.ts`, `NewDocDialog` |
| Writing the result | `PUT /api/repos/{repo}/files/{path}` → uncommitted worktree save; protected `main` auto-branches to `ws/<user>` |
| Editing the result | Milkdown editor at `/editor/<path>` |
| Sign-off | commit → `POST /propose` (MR/PR) or in-app merge — replaces the whole GitLab layer of the source |
| Transcript persistence | `state/chats.ts` — module store + localStorage per repo, capped |
| SSE + heartbeat + tool-activity events | `api/speccy.go` `speccyChat`, `api/speccy.ts` `streamChat` |

**Conclusion: this is a UI/flow port plus ~3 new prompt contracts, not an
infrastructure port.** No new storage, no DB migration, no new auth surface.

## 3. Structural mismatches that need decisions

### 3.1 Structured output (the only real technical gap)

The source uses the OpenAI **Responses API** with `responses.parse(text_format=
PydanticModel)` — guaranteed schema conformance. specquill's `ai.Client` is
`/chat/completions`-only and deliberately provider-agnostic (Ollama, Gemini's
OpenAI surface, `scripts/mock-llm.py`), with no `response_format` support.

Options:

| Option | Verdict |
| --- | --- |
| Add `response_format: {type: json_schema}` | Breaks the provider-agnostic promise; ollama/mock-llm don't honour it uniformly |
| **Prompt for JSON + tolerant `ai.ExtractJSON`** | ✅ Already the established idiom — `speccyDraft` does exactly this (`ai.DraftPrompt` → `ExtractJSON`) |
| Model the rubric as a tool call | Costs a round-trip per turn and fights `ask_user`'s halt semantics |

**Decision: reuse `ai.ExtractJSON`**, with one retry on a parse failure and a
graceful fallback ("show the raw reply as a plain chat turn, rubric unchanged").
Dev runs `qwen2.5:7b`; small local models emit sloppy JSON, so tolerance is not
optional.

### 3.2 Streaming vs. structured turns

An interview turn wants both tools *and* a JSON result. `StreamTools` is the only
tool loop and it streams. Use it anyway: buffer the deltas, forward `tool` events
to the client for the activity display (already wired), and parse the final
buffered text as JSON, emitting a terminal `interview` SSE event. The 15s
heartbeat from `speccyChat` must be replicated — factor it out rather than
copy-pasting.

### 3.3 Where the wizard state lives

The source keeps everything in React state and posts the full transcript each
turn. specquill's chat is stateless by design (server holds nothing). **Keep it
stateless**: a `state/wizard.ts` store mirroring `state/chats.ts` (module store +
`useSyncExternalStore` + localStorage per repo). Survives closing the view, no
server state, no store schema change.

### 3.4 "Business vs technical mode" → document family

The source's mode switch selects a prompt set + section template. specquill
already has a stronger axis: the **document family** (`entities.ts`) with a
matching `.specquill/skills/<family>.md`. Map mode → family; keep an optional
"altitude" hint (business / technical) as a single line injected into the prompt,
not a second prompt set.

### 3.5 Section templates

New concept for specquill. Defaults per family, overridable in
`.specquill/config.yml` (git-native, consistent with `entities:`/`drivers:`):

```yaml
sections:
  spec: ["Overview", "Behaviour", "Interfaces & data", "Edge cases", "Open questions"]
  requirement: ["Context", "Requirement statements", "Acceptance criteria"]
```

(Implemented as a top-level `sections:` block rather than a key inside
`entities:` — it parses with the same inline idiom and keeps the entity block
about identity, not authoring.)

**The client sends the resolved `sections[]` in the request**; the server carries
a built-in fallback. Keeps the server dumb and puts template editing where config
editing already lives (`components/ConfigDoc.tsx`).

### 3.6 Block workbench vs. straight to the editor

The source's workbench is a bespoke section editor because it has no real editor.
specquill does. Per the brief ("…which can then be edited in the existing
markdown editor"): **no block workbench**. Instead a lightweight *review* step —
the drafted sections listed, each with `redraft` / `tighten` / free instruction
(the source's `/api/block` semantics, ~120 lines of UI) — then "Create document"
writes one file and navigates to `/editor/<path>`. Nothing is written to the
worktree before that button, so abandoned wizards leave no debris in the changes
drawer.

### 3.7 Deliberately deferred

- **Images / mockups.** `ai.Message.Content` is a plain `string`; multimodal means
  changing the core wire type to content parts and touching every call site.
  Out of scope for v1 — flag it as its own change.
- **Streaming per-token into the review pane.** Nice, not required.

## 4. Proposed feature

**Rail entry ✎ "Draft with Speccy"** → route `/p/<project>[/b/<branch>]/wizard`
(new `ViewName`, `VIEWS`, `VIEW_LABEL`, route in `main.tsx`). Secondary entry
points: a "draft with Speccy" button in `NewDocDialog`, and the Dashboard.

```
┌ Intent ────────┐  ┌ Related ───────┐  ┌ Interview ─────┐  ┌ Review ────────┐
│ what + family  │→ │ already exists?│→ │ grill + rubric │→ │ sections +     │→ /editor/<path>
│ + altitude     │  │ extend vs new  │  │ ready_to_draft │  │ per-sec redraft│
└────────────────┘  └────────────────┘  └────────────────┘  └────────────────┘
```

1. **Intent** — textarea, family picker (reuses `entities`), target subfolder,
   altitude toggle. Prefills id/path via `lib/ids` exactly as `NewDocDialog` does.
2. **Related** — `POST …/speccy/related`. Cards per match with relation +
   reason. "Extend this one" → opens it in the editor with the Speccy panel
   focused on it; "Create new anyway" → carries the matches forward as suggested
   `satisfies:` / `drivers:` frontmatter links (the source turns them into
   `related:`; specquill has real typed links, so this is strictly better).
3. **Interview** — `POST …/speccy/interview` (SSE). Chat column + a persistent
   **rubric checklist** ("5/8 ready"), tool-activity line, quick-answer chips
   from `questions[]`, and a "just draft it" escape hatch (the source's explicit
   behaviour: fill gaps with flagged assumptions). Answers append to the
   transcript, which is all the server ever sees.
4. **Review** — `POST …/speccy/compose` returns `{title, sections[{name,
   content}]}`. Per-section `POST …/speccy/section` for redraft/tighten/instruct.
   "Create document" assembles frontmatter (`lib/newdoc`) + `## ` sections and
   PUTs through the **existing** file endpoint after `ensureWritableBranch()`.
5. **Handoff** — `nav('/editor/' + path)`. From there: normal editing, commit,
   propose/merge. Ask-answers worth keeping land in `.specquill/memory/` via the
   existing memory rules.

## 5. Explicitly not ported

| Source feature | Why not |
| --- | --- |
| GitLab publish, `SPEC-NNN` ids, status folders, MR sign-off | specquill is git-native: worktree drafts → commit → propose/merge; lifecycle is `status:` frontmatter, not folders |
| Work tracking (`work.py`, branch/MR scan, auto status moves) | Different product surface; forge review (`api/forge.go`) already covers the read-only half |
| Admin prompt editor + `.speccy/settings.json` | Replaced by `.specquill/skills/*.md` + `instructions.md` + `speccy.instructions` — versioned in the repo |
| Copy/download markdown | The workspace *is* markdown in git |
| Test-case generation (`/api/testcases`) | Valuable, but independent of this flow — follow-up |
| Compare spec ↔ implementation (`/api/specs/{id}/compare`) | Follow-up, and it lands cheaply: point a **reference source** at the code repo and the existing read tools already reach it |

## 6. Work breakdown

### Phase 0 — extractions (no behaviour change)
- `api/sse.go`: pull the SSE writer + 15s heartbeat out of `speccyChat`.
- `ai/json.go` or extend `client.go`: `CompleteJSON`-style helper = StreamTools
  with buffered content → `ExtractJSON` + one retry.
- `web/src/api/sse.ts`: factor the frame reader out of `streamChat`.
- Cost: ~0.5 day. Touches `api/speccy.go`, `api/speccy.ts` only.

### Phase 1 — MVP: intent → interview → review → editor
- `internal/ai/wizard.go` — `InterviewPrompt`, `ComposePrompt`, `SectionPrompt`
  and their JSON contracts; all built on `GroundingPrompt` + `AuthoringRules`.
- `internal/api/wizard.go` — `POST /api/repos/{repo}/speccy/{interview,compose,section}`,
  all `writableH`. The stages only read, but they exist to produce a document,
  so a viewer is refused up front rather than burning model tokens on a draft
  they could never create. Reuses `speccyToolbox` with `writable: false` (its
  new `readSpecs()` tool set) and `resolveSources`.
- `web/src/state/wizard.ts`, `web/src/api/wizard.ts`, `web/src/views/WizardView.tsx`
  (+ `Intent`, `Interview`, `Review` sub-components), rail entry, route, `VIEWS`.
- Built-in section templates per family (Go fallback + `lib/entities.ts` defaults).
- Tests: `wizard_test.go` (mirrors `speccytools_test.go`/`grounding_test.go`),
  vitest for the store + markdown assembly, `web/e2e/wizard.spec.ts`.
  **`scripts/mock-llm.py` needs canned JSON for the three new contracts** — the
  e2e is otherwise unrunnable keylessly.
- Cost: ~2–3 days.

### Phase 2 — dedup / related step
- `POST …/speccy/related` + the card UI + typed-link carry-forward.
- Cost: ~1 day.

### Phase 3 — polish
- Config-driven `sections:` per entity, rubric UX, "just draft it", resume a
  wizard from the store after reload, empty/error states.
- Cost: ~1 day.

### Later
Test cases; compare-vs-implementation over a code reference source; multimodal
intent (mockup paste).

## 7. Risks

- **JSON conformance on small models.** `qwen2.5:7b` in dev will occasionally
  return prose. Mitigation: tolerant parse + retry + degrade to a plain chat turn
  rather than an error.
- **Tool budgets.** `maxToolRounds = 8`, `maxToolBytes = 64 KiB` are tuned for
  chat. An interview turn that explores *and* asks may starve; likely needs a
  per-flow budget parameter on `StreamTools`.
- **Long silences.** Thinking-class models go quiet for minutes; the heartbeat
  must be on every new SSE endpoint or proxies kill the connection with nothing
  in any log (already a known failure mode here).
- **Wizard debris.** Never write to the worktree before the explicit "Create
  document" — otherwise every abandoned interview shows up in the changes drawer.
- **E2E discipline.** The SPA is embedded in the Go binary: `npm run build` +
  `go build` + restart before running playwright, or the browser serves stale UI.
