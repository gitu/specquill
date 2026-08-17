package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	"specquill/server/internal/ai"
	"specquill/server/internal/project"
)

// Guided authoring endpoints — the staged wizard (intent → related →
// interview → draft) behind /api/repos/{repo}/speccy/{related,interview,
// compose,section}.
//
// All four are READ-ONLY: they explore the workspace and the selected
// reference sources with the speccy's read tools and answer with structured
// JSON. The document itself is created by the SPA through the ordinary file
// endpoint once the human accepts the draft — so an abandoned wizard leaves
// nothing in the worktree, and nothing here needs the editor role.
//
// All four stream SSE for one reason: they run a tool loop against a
// thinking-class model and can stay silent for minutes. Plain JSON responses
// die at the first reverse proxy's idle timeout.

// wizardRequest is the shared wire shape; each stage reads the extra fields
// it needs. Sections/Messages are absent on the earlier stages.
type wizardRequest struct {
	Branch   string `json:"branch"`
	Intent   string `json:"intent"`
	Family   string `json:"family"`   // entity kind: requirement | spec | change | …
	Folder   string `json:"folder"`   // where the document will land (context only)
	Altitude string `json:"altitude"` // business | technical | ""

	Messages []ai.Message `json:"messages"` // interview transcript (text turns)
	Sections []string     `json:"sections"` // the outline the client resolved

	// section refinement only
	Title          string `json:"title"`
	Section        string `json:"section"`
	SectionContent string `json:"sectionContent"`
	Instruction    string `json:"instruction"`
}

func (b wizardRequest) context() ai.WizardContext {
	return ai.WizardContext{Intent: b.Intent, Family: b.Family, Folder: b.Folder, Altitude: b.Altitude}
}

// sections resolves the outline: what the client sent (config-driven,
// user-editable) or the family's built-in default.
func (b wizardRequest) sections() []string {
	out := make([]string, 0, len(b.Sections))
	for _, s := range b.Sections {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return ai.SectionsFor(b.Family)
	}
	return out
}

// wizardStage is the shared body of every stage: decode, ground, run the
// read-only tool loop through askJSON (the repo's one JSON-turn helper —
// same one-re-ask repair as the alignment pipelines), narrate the tool
// activity, stream the parsed result.
// buildRules turns the decoded request into the stage-specific system-prompt
// tail; out receives the parsed result and is sent as the terminal frame.
func (s *Server) wizardStage(
	w http.ResponseWriter, r *http.Request, repo *project.Project, stage string,
	buildRules func(wizardRequest) (rules string, final string), out any,
	finish func(wizardRequest),
	// decode is set by the stages that answer in MARKDOWN rather than JSON
	// (compose, section). When nil the reply goes through askJSON.
	decode func(reply string) error,
) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "Speccy is not configured (ai: in specquill.yml)")
		return
	}
	var body wizardRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Intent) == "" && len(body.Messages) == 0 {
		jsonError(w, http.StatusBadRequest, "intent required")
		return
	}
	branch := repo.ResolveRef(body.Branch)
	files, err := repo.Snapshot(branch)
	if err != nil {
		gitFail(w, err)
		return
	}
	sources, grounded := s.resolveSources(r, repo, body.Branch)
	instructions := ""
	if cfg := inRepoConfig(repo, body.Branch); cfg != nil {
		instructions = cfg.Speccy.Instructions
	}

	rules, final := buildRules(body)
	system := ai.GroundingPrompt(files, grounded, "", s.ai.GroundingBudget(), instructions)
	system += ai.ReadToolRules
	if len(sources) > 0 {
		names := make([]string, 0, len(sources))
		for _, src := range sources {
			names = append(names, "~"+src.Name)
		}
		sort.Strings(names)
		system += "\nSelected reference sources — explore them with list_files/search/read_file even when not excerpted above: " + strings.Join(names, ", ") + "\n"
	}
	system += rules
	msgs := append([]ai.Message{{Role: "system", Content: system}}, ai.TranscriptMessages(body.Intent, body.Messages, final)...)

	stream, ok := startSSE(w)
	if !ok {
		return
	}
	defer stream.Close()

	// the wizard never writes and never asks mid-turn — its questions come
	// back structurally in the JSON, so only the read tools are registered
	tb := &speccyToolbox{repo: repo, branch: branch, writable: false, sources: sources, files: files,
		publish: func() {}}
	note := func(text string) { stream.Send(map[string]string{"note": text}) }

	ctx := ai.WithLabel(r.Context(), "wizard "+stage)
	if decode != nil {
		var reply string
		if reply, err = s.askText(ctx, msgs, tb.readSpecs(), tb.exec, note); err == nil {
			err = decode(reply)
		}
	} else {
		err = s.askJSON(ctx, msgs, tb.readSpecs(), tb.exec, note, out)
	}
	if err != nil {
		// ai.SafeErr, not %v: a provider's error body can echo the grounding
		// prompt (= workspace document content) into the server log
		log.Printf("speccy %s [%s@%s]: %s", stage, repo.ID, branch, ai.SafeErr(err))
		stream.Send(map[string]string{"error": err.Error()})
		return
	}
	if finish != nil {
		finish(body)
	}
	stream.Send(map[string]any{"result": out})
	stream.Send(map[string]bool{"done": true})
}

// askText runs the same read-only tool loop as askJSON but returns the reply
// verbatim. The prose stages need no parse retry: any text is a valid answer,
// which is precisely why they stopped asking for JSON.
func (s *Server) askText(ctx context.Context, msgs []ai.Message, specs []ai.ToolSpec, exec ai.ToolExec, note func(string)) (string, error) {
	var reply strings.Builder
	_, _, err := s.ai.StreamTools(ctx, msgs, specs, exec,
		func(delta string) error { reply.WriteString(delta); return nil },
		func(tc ai.ToolCall, _ string, execErr error) error { note(toolNote(tc, execErr)); return nil },
	)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(reply.String()) == "" {
		return "", fmt.Errorf("model returned an empty reply")
	}
	return reply.String(), nil
}

// --- stage: related -------------------------------------------------------

type relatedMatch struct {
	Path     string `json:"path"`
	Title    string `json:"title"`
	Relation string `json:"relation"` // covers | overlaps | related
	Reason   string `json:"reason"`
}

type relatedResult struct {
	Matches        []relatedMatch `json:"matches"`
	Recommendation string         `json:"recommendation"`
}

// POST /api/repos/{repo}/speccy/related — does the workspace already cover
// this intent? Suggests extending an existing document instead of creating a
// near-duplicate. The human always decides.
func (s *Server) speccyRelated(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	out := &relatedResult{}
	s.wizardStage(w, r, repo, "related",
		func(b wizardRequest) (string, string) {
			return ai.RelatedRules(b.context()), ""
		}, out,
		func(b wizardRequest) {
			// hallucination guard: a suggestion the human cannot open is
			// worse than no suggestion. Only paths that exist survive.
			snapshot, err := repo.Snapshot(repo.ResolveRef(b.Branch))
			if err != nil {
				snapshot = nil
			}
			kept := make([]relatedMatch, 0, len(out.Matches))
			for _, m := range out.Matches {
				m.Path = strings.TrimPrefix(strings.TrimSpace(m.Path), "./")
				if _, ok := snapshot[m.Path]; !ok {
					continue
				}
				if m.Relation != "covers" && m.Relation != "overlaps" {
					m.Relation = "related"
				}
				kept = append(kept, m)
			}
			out.Matches = kept
			if out.Recommendation != "new" {
				found := false
				for _, m := range kept {
					if m.Path == out.Recommendation {
						found = true
						break
					}
				}
				if !found {
					out.Recommendation = "new"
				}
			}
		}, nil)
}

// --- stage: interview -----------------------------------------------------

type rubricItem struct {
	Criterion string `json:"criterion"`
	Met       bool   `json:"met"`
}

// interviewQuestion is one open question plus the concrete answers the author
// can pick. Options are what make the interview clickable instead of an essay
// prompt; a question without them still renders, with the free-text field only.
type interviewQuestion struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
}

// UnmarshalJSON accepts a bare string as well as the object form. The prompt
// asks for objects, but a smaller model reverts to `["question one", …]` often
// enough that failing the whole turn over it would be the wrong trade —
// a question with no options is still a usable question.
func (q *interviewQuestion) UnmarshalJSON(raw []byte) error {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		q.Question, q.Options = text, nil
		return nil
	}
	var obj struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err
	}
	q.Question, q.Options = obj.Question, obj.Options
	return nil
}

type interviewResult struct {
	Reply        string              `json:"reply"`
	Questions    []interviewQuestion `json:"questions"`
	Rubric       []rubricItem        `json:"rubric"`
	ReadyToDraft bool                `json:"readyToDraft"`
}

// POST /api/repos/{repo}/speccy/interview — one grilling turn: what the
// speccy found, what it still needs, and the running readiness rubric.
func (s *Server) speccyInterview(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	out := &interviewResult{}
	s.wizardStage(w, r, repo, "interview",
		func(b wizardRequest) (string, string) {
			return ai.InterviewRules(b.context(), b.sections()), ""
		}, out,
		func(b wizardRequest) {
			// the first turn has only the author's rough idea — a model that
			// declares readiness there has skipped the interview entirely
			if len(b.Messages) == 0 {
				out.ReadyToDraft = false
			}
			// drop empties and cap the options so one verbose turn cannot
			// push a wall of chips at the author
			kept := out.Questions[:0]
			for _, q := range out.Questions {
				if strings.TrimSpace(q.Question) == "" {
					continue
				}
				if len(q.Options) > 5 {
					q.Options = q.Options[:5]
				}
				kept = append(kept, q)
			}
			out.Questions = kept
		}, nil)
}

// --- stage: compose -------------------------------------------------------

type composeResult struct {
	Title    string       `json:"title"`
	Sections []ai.Section `json:"sections"`
}

// POST /api/repos/{repo}/speccy/compose — write the draft, one block per
// section of the outline. Nothing is saved: the SPA shows the blocks for
// review and creates the file through the normal endpoint on accept.
func (s *Server) speccyCompose(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	out := &composeResult{}
	s.wizardStage(w, r, repo, "compose",
		func(b wizardRequest) (string, string) {
			return ai.ComposeRules(b.context(), b.sections()),
				"Write the full document now, one block per requested section."
		}, out,
		func(b wizardRequest) {
			// models drop, reorder and re-case blocks; the UI is built around
			// a stable outline, so normalize against what was asked for
			out.Sections = ai.SortSectionsLike(b.sections(), out.Sections)
		},
		func(reply string) error {
			title, sections := ai.ParseDraft(reply)
			if len(sections) == 0 {
				return fmt.Errorf("no `## ` sections in the reply")
			}
			out.Title, out.Sections = title, sections
			return nil
		})
}

// --- stage: section -------------------------------------------------------

type sectionResult struct {
	Content string `json:"content"`
	Note    string `json:"note"`
}

// POST /api/repos/{repo}/speccy/section — revise one section in place
// ("redraft", "tighten", or a free instruction from the author).
func (s *Server) speccySection(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	out := &sectionResult{}
	s.wizardStage(w, r, repo, "section",
		func(b wizardRequest) (string, string) {
			instruction := strings.TrimSpace(b.Instruction)
			if instruction == "" {
				instruction = "redraft this section"
			}
			return ai.SectionRules(b.context(), b.Title, b.Section, b.SectionContent, instruction), ""
		}, out, nil,
		func(reply string) error {
			out.Content, out.Note = ai.ParseSectionReply(reply)
			return nil
		})
}
