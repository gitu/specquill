package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"specquill/server/internal/ai"
	"specquill/server/internal/project"
)

// Chat attachments: a file dropped into the speccy panel becomes two
// uncommitted drafts on the branch — the original under references/assets/
// and its extracted text as a source page under references/archive/ — and
// the chat turn that follows carries a note telling the speccy what arrived
// and how to use it. The original is the record; the text exists so the model
// can read it. Both travel with the branch like any other draft.

const (
	attachAssetDir   = "references/assets"
	attachArchiveDir = "references/archive"
	maxAttachSize    = 50 << 20   // 50 MiB
	maxArchiveText   = 900 * 1024 // a source page keeps its head; the original is the record
)

// Attachment is what the chat receives back for each uploaded file.
type Attachment struct {
	Asset      string  `json:"asset"`      // references/assets/<name>-<hash>.<ext>
	SourcePage string  `json:"sourcePage"` // references/archive/<name>-<hash>.md
	Kind       ai.Kind `json:"kind"`
	Filename   string  `json:"filename"`
	TextLength int     `json:"textLength"`
}

// POST /api/repos/{repo}/speccy/attach?branch= — multipart `file` (+ `hint`).
// Writes are drafts on the branch, so the same rules as the chat's write
// tools apply: an editor on an unprotected branch.
func (s *Server) speccyAttach(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	if repo.Repo.Cfg.IsProtected(branch) {
		jsonError2(w, http.StatusForbidden, "attachments are drafts — switch to your workspace branch first", "protected_branch")
		return
	}
	if err := r.ParseMultipartForm(maxAttachSize); err != nil {
		jsonError(w, http.StatusBadRequest, "parse upload: "+err.Error())
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxAttachSize+1))
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(data) > maxAttachSize {
		jsonError(w, http.StatusRequestEntityTooLarge, "attachment exceeds 50 MiB")
		return
	}
	hint := strings.TrimSpace(r.FormValue("hint"))

	text, kind, err := s.extractAttachment(r, hdr.Filename, data)
	if err != nil {
		jsonError2(w, http.StatusBadGateway, err.Error(), "extract_failed")
		return
	}

	// content hash: the same file attached twice lands on the same asset
	ext := strings.ToLower(path.Ext(hdr.Filename))
	base := strings.TrimSuffix(path.Base(hdr.Filename), path.Ext(hdr.Filename))
	base = strings.Trim(assetNameRe.ReplaceAllString(base, "-"), "-.")
	if base == "" {
		base = "file"
	}
	if len(base) > 60 {
		base = base[:60]
	}
	sum := sha256.Sum256(data)
	stem := base + "-" + hex.EncodeToString(sum[:])[:8]
	asset := path.Join(attachAssetDir, stem+ext)
	page := path.Join(attachArchiveDir, stem+".md")

	if _, _, err := repo.File(branch, asset); err != nil { // SaveFile with an empty baseSha refuses existing files
		if _, err := repo.SaveFile(branch, asset, string(data), ""); err != nil {
			gitFail(w, err)
			return
		}
	}
	if _, _, err := repo.File(branch, page); err != nil {
		if _, err := repo.SaveFile(branch, page, sourcePage(hdr.Filename, asset, page, kind, hint, text), ""); err != nil {
			gitFail(w, err)
			return
		}
	}
	s.publish("save", repo.Key(), branch)
	jsonOK(w, Attachment{Asset: asset, SourcePage: page, Kind: kind, Filename: path.Base(hdr.Filename), TextLength: len(text)})
}

// extractAttachment picks the road by kind: local for text and HTML, the
// chat model's vision input for images, the extraction endpoint for the rest.
func (s *Server) extractAttachment(r *http.Request, filename string, data []byte) (string, ai.Kind, error) {
	kind := ai.KindOf(filename)
	switch kind {
	case ai.KindText:
		if !isText(data) {
			return "", kind, fmt.Errorf("%s is not a text file", path.Base(filename))
		}
		return string(data), kind, nil
	case ai.KindHTML:
		text, err := ai.HTMLToText(string(data))
		return text, kind, err
	case ai.KindImage:
		if s.ai == nil {
			return "", kind, fmt.Errorf("Speccy is not configured (ai: in specquill.yml) — images cannot be read")
		}
		text, err := s.ai.Describe(ai.WithLabel(r.Context(), "attach image"), filename, data)
		return text, kind, err
	default:
		text, err := s.extract.Extract(ai.WithLabel(r.Context(), "attach document"), filename, data)
		return text, kind, err
	}
}

// isText is the same heuristic the snapshot uses: no NUL byte in the head.
func isText(b []byte) bool {
	head := b
	if len(head) > 8000 {
		head = head[:8000]
	}
	for _, c := range head {
		if c == 0 {
			return false
		}
	}
	return true
}

// sourcePage is the archive record: what the file was, where the original
// is, and the text read from it. The system writes it; the speccy never
// rewrites references/ (the note below says so).
func sourcePage(filename, asset, page string, kind ai.Kind, hint, text string) string {
	if len(text) > maxArchiveText {
		text = text[:maxArchiveText] + "\n\n*(truncated — see the archived original)*"
	}
	desc := "Archived source file (" + path.Base(filename) + ")."
	if hint != "" {
		desc = hint
	}
	rel, _ := relPath(page, asset)
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("type: Source\n")
	b.WriteString("title: " + yamlQuote(path.Base(filename)) + "\n")
	b.WriteString("description: " + yamlQuote(desc) + "\n")
	b.WriteString("source_kind: " + string(kind) + "\n")
	b.WriteString("original: " + yamlQuote(asset) + "\n")
	b.WriteString("---\n\n")
	b.WriteString("# " + path.Base(filename) + "\n\n")
	if hint != "" {
		b.WriteString(hint + "\n\n")
	}
	b.WriteString("Original: [" + path.Base(filename) + "](" + rel + ")\n\n")
	switch kind {
	case ai.KindImage:
		b.WriteString("## What the image shows\n\n")
	case ai.KindHTML:
		b.WriteString("## Visible text of the mock-up\n\n")
	default:
		b.WriteString("## Extracted text\n\n")
	}
	b.WriteString(text)
	if !strings.HasSuffix(text, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

func yamlQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ").Replace(s) + `"`
}

// relPath is the markdown-relative link from one repo path to another.
func relPath(from, to string) (string, error) {
	fromDir := path.Dir(from)
	up := 0
	for !strings.HasPrefix(to+"/", fromDir+"/") && fromDir != "." {
		fromDir = path.Dir(fromDir)
		up++
	}
	rest := to
	if fromDir != "." {
		rest = strings.TrimPrefix(to, fromDir+"/")
	}
	return strings.Repeat("../", up) + rest, nil
}

// attachmentNote is appended to the message that carries attachments: what
// was archived and what to do with each kind. Images and mock-ups are
// content to embed, not text to summarise; their source pages exist so the
// speccy understands them well enough to place them.
func attachmentNote(atts []Attachment) string {
	if len(atts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nAttached to this message (archived as uncommitted drafts on this branch):\n")
	for _, a := range atts {
		b.WriteString("  " + a.SourcePage + "   (original: " + a.Asset + ", " + string(a.Kind) + ")\n")
	}
	b.WriteString("\nRead each source page — it holds the extracted text — and work what it says into the " +
		"documents it concerns, linking them to the source page. When no existing document is the right " +
		"home, create one in the family that fits (its folder and conventions are in the create_file " +
		"description and the authoring skills) — a summary in the chat is not filing. Never edit or move " +
		"anything under references/: it is the immutable record of what a file actually contained.\n")
	var images, mockups []Attachment
	for _, a := range atts {
		switch a.Kind {
		case ai.KindImage:
			images = append(images, a)
		case ai.KindHTML:
			mockups = append(mockups, a)
		}
	}
	if len(images) > 0 {
		b.WriteString("\nImages are content, not just text: embed each original 1:1 in the document it belongs to, " +
			"as a markdown image with a path relative to that document, a short alt text and at most one caption line. " +
			"The description on the source page only tells you where the image belongs — do not paste it instead of the image.\n")
		for _, a := range images {
			b.WriteString("  ![…](<relative path to " + a.Asset + ">)\n")
		}
	}
	if len(mockups) > 0 {
		b.WriteString("\nHTML files are mock-ups to show, not text to summarise: link each original from the document it " +
			"belongs to with a plain markdown link ([Mock-up: <title>](<relative path>)) — the reader shows such links " +
			"as an embedded preview. One line saying what the mock-up shows is enough.\n")
		for _, a := range mockups {
			b.WriteString("  [Mock-up](<relative path to " + a.Asset + ">)\n")
		}
	}
	return b.String()
}
