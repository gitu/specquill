package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"specquill/server/internal/config"
)

func attachRequest(t *testing.T, name, hint string, data []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if hint != "" {
		_ = mw.WriteField("hint", hint)
	}
	part, err := mw.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/repos/w/speccy/attach?branch=main", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-SpecQuill", "1")
	return req
}

func TestAttachHTMLArchivesOriginalAndSourcePage(t *testing.T) {
	h, _, gitm := testServerFull(t, false)
	cookie := login(t, h)
	html := "<html><head><title>Widget dashboard</title><style>x{}</style></head><body><h1>Widgets</h1><script>alert(1)</script><p>Drag &amp; drop</p></body></html>"
	req := attachRequest(t, "mock-up.html", "the dashboard mock-up", []byte(html))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("attach: %d %s", rec.Code, rec.Body.String())
	}
	var att Attachment
	_ = json.Unmarshal(rec.Body.Bytes(), &att)
	if att.Kind != "html" || !strings.HasPrefix(att.Asset, "references/assets/mock-up-") || !strings.HasSuffix(att.Asset, ".html") ||
		!strings.HasPrefix(att.SourcePage, "references/archive/mock-up-") || att.TextLength == 0 {
		t.Fatalf("attachment %+v", att)
	}

	repo, _ := gitm.Repo("w")
	original, _, err := repo.File("main", att.Asset)
	if err != nil || original != html {
		t.Fatalf("original not archived byte-identical: %v", err)
	}
	page, _, err := repo.File("main", att.SourcePage)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"type: Source", `title: "mock-up.html"`, `description: "the dashboard mock-up"`, "source_kind: html",
		"Original: [mock-up.html](../assets/mock-up-", "# Widget dashboard", "Drag & drop"} {
		if !strings.Contains(page, want) {
			t.Errorf("source page lacks %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "alert(1)") {
		t.Errorf("script content must not reach the source page")
	}

	// the same bytes again land on the same asset, nothing is duplicated
	req = attachRequest(t, "mock-up.html", "", []byte(html))
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var again Attachment
	_ = json.Unmarshal(rec.Body.Bytes(), &again)
	if again.Asset != att.Asset || again.SourcePage != att.SourcePage {
		t.Fatalf("re-attach moved the archive: %+v vs %+v", again, att)
	}

	// the original is served raw as html under the sandbox CSP
	raw := httptest.NewRequest(http.MethodGet, "/api/repos/w/raw/"+att.Asset+"?ref=main", nil)
	raw.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, raw)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || rec.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("raw html: %d %q csp=%q", rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Content-Security-Policy"))
	}
}

func TestAttachDocumentGoesThroughTheExtractor(t *testing.T) {
	var gotAuth, gotName string
	extract := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, hdr, err := r.FormFile("file")
		if err == nil {
			gotName = hdr.Filename
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pages":[{"text":"page one"},{"text":"page two"}]}`))
	}))
	defer extract.Close()
	t.Setenv("SPECQUILL_TEST_AI_KEY", "k-extract")
	h, _, _ := testServerCfg(t, false, func(cfg *config.Config) {
		cfg.AI.ExtractURL = extract.URL
		cfg.AI.APIKeyEnv = "SPECQUILL_TEST_AI_KEY"
	})
	cookie := login(t, h)
	req := attachRequest(t, "notes.pdf", "", []byte("%PDF-1.4 fake"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("attach pdf: %d %s", rec.Code, rec.Body.String())
	}
	var att Attachment
	_ = json.Unmarshal(rec.Body.Bytes(), &att)
	if att.Kind != "document" || att.TextLength != len("page one\n\npage two") || gotAuth != "Bearer k-extract" || gotName != "notes.pdf" {
		t.Fatalf("attachment %+v auth=%q name=%q", att, gotAuth, gotName)
	}
}

func TestAttachDocumentWithoutExtractorIsRefused(t *testing.T) {
	h := testServer(t)
	cookie := login(t, h)
	req := attachRequest(t, "notes.pdf", "", []byte("%PDF-1.4 fake"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "extract_url") {
		t.Fatalf("want a clear refusal, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAttachmentNoteTellsTheModelHowToUseEachKind(t *testing.T) {
	note := attachmentNote([]Attachment{
		{Asset: "references/assets/shot-1234abcd.png", SourcePage: "references/archive/shot-1234abcd.md", Kind: "image"},
		{Asset: "references/assets/mock-1234abcd.html", SourcePage: "references/archive/mock-1234abcd.md", Kind: "html"},
		{Asset: "references/assets/doc-1234abcd.pdf", SourcePage: "references/archive/doc-1234abcd.md", Kind: "document"},
	})
	for _, want := range []string{"references/archive/shot-1234abcd.md", "embed each original 1:1", "![…]", "[Mock-up]", "Never edit or move anything under references/"} {
		if !strings.Contains(note, want) {
			t.Errorf("note lacks %q:\n%s", want, note)
		}
	}
	if attachmentNote(nil) != "" {
		t.Error("no attachments, no note")
	}
}
