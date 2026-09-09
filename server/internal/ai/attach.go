package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"specquill/server/internal/config"
)

// Attachments: turning a file someone drops into the chat into text the
// speccy can read. Three roads — plain text and HTML are reduced locally,
// images go through the chat model's vision input, everything else (PDF,
// Office, scans) goes to an extraction endpoint next to the chat API
// (ai.extract_url). Nothing here touches the workspace.

// Kind classifies an attachment by extension.
type Kind string

const (
	KindText     Kind = "text"
	KindHTML     Kind = "html"
	KindImage    Kind = "image"
	KindDocument Kind = "document"
)

var imageMIME = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp",
}

// KindOf reports how a file is read, from its name.
func KindOf(filename string) Kind {
	switch ext := strings.ToLower(path.Ext(filename)); ext {
	case ".md", ".txt", ".markdown", ".csv", ".json", ".yaml", ".yml", ".adoc":
		return KindText
	case ".html", ".htm":
		return KindHTML
	default:
		if _, ok := imageMIME[ext]; ok {
			return KindImage
		}
		return KindDocument
	}
}

// Describe reads an image with the main model: a verbatim transcription of
// any text plus a description of what it shows — enough to file it, not a
// replacement for embedding it.
func (c *Client) Describe(ctx context.Context, filename string, data []byte) (string, error) {
	mime, ok := imageMIME[strings.ToLower(path.Ext(filename))]
	if !ok {
		return "", fmt.Errorf("%s is not an image the model can read", path.Base(filename))
	}
	body := map[string]any{
		"model": c.model,
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": "This image was dropped into a team wiki. Transcribe all text in it verbatim (tables as markdown tables), " +
					"then describe what it shows — diagrams, screenshots, sketches — in enough detail that someone who cannot see it " +
					"understands it. Markdown, no preamble."},
				{"type": "image_url", "image_url": map[string]string{"url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)}},
			},
		}},
	}
	if c.effort != "" {
		body["reasoning_effort"] = c.effort
	}
	started := time.Now()
	res, err := c.request(ctx, body)
	if err != nil {
		log.Printf("ai: %s%s describe failed after %s: %v", c.model, labelOf(ctx), since(started), err)
		return "", err
	}
	defer res.Body.Close()
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("the model returned no description for the image")
	}
	// sizes only: the image and its description are workspace material
	log.Printf("ai: %s%s describe in %s (image %s → text %s)", c.model, labelOf(ctx), since(started), size(len(data)), size(len(out.Choices[0].Message.Content)))
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

// Extractor turns documents (PDF, Office, scans) into text through an
// extraction endpoint — the same bearer key as the chat API, so a gateway
// that serves both (Wingman's /v1/extract) needs no second secret.
type Extractor struct {
	url  string
	key  string
	http *http.Client
}

// NewExtractor returns nil when no endpoint is configured.
func NewExtractor(cfg config.AIConfig) *Extractor {
	if cfg.ExtractURL == "" {
		return nil
	}
	key := ""
	if cfg.APIKeyEnv != "" {
		key = os.Getenv(cfg.APIKeyEnv)
	}
	return &Extractor{url: cfg.ExtractURL, key: key, http: &http.Client{Timeout: 3 * time.Minute}}
}

// Extract posts the file as multipart `file` and reads `{text}` — or
// `{pages:[{text}]}` joined — from the reply.
func (e *Extractor) Extract(ctx context.Context, filename string, data []byte) (string, error) {
	if e == nil {
		return "", fmt.Errorf("no extraction endpoint configured (ai.extract_url) — %s cannot be read", path.Base(filename))
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", path.Base(filename))
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	if e.key != "" {
		req.Header.Set("Authorization", "Bearer "+e.key)
	}
	started := time.Now()
	res, err := e.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("extraction failed for %s: HTTP %d", path.Base(filename), res.StatusCode)
	}
	var out struct {
		Text  string `json:"text"`
		Pages []struct {
			Text string `json:"text"`
		} `json:"pages"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(&out); err != nil {
		return "", fmt.Errorf("extraction reply for %s is not JSON: %v", path.Base(filename), err)
	}
	text := strings.TrimSpace(out.Text)
	if text == "" {
		var parts []string
		for _, p := range out.Pages {
			if t := strings.TrimSpace(p.Text); t != "" {
				parts = append(parts, t)
			}
		}
		text = strings.Join(parts, "\n\n")
	}
	if text == "" {
		return "", fmt.Errorf("extraction returned no text for %s", path.Base(filename))
	}
	log.Printf("ai: extract %s in %s (%s → text %s)", strings.ToLower(path.Ext(filename)), since(started), size(len(data)), size(len(text)))
	return text, nil
}

var (
	htmlTitleRe  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	htmlDropRe   = regexp.MustCompile(`(?is)<(script|style|noscript|svg)[^>]*>.*?</(script|style|noscript|svg)>`)
	htmlCommRe   = regexp.MustCompile(`(?s)<!--.*?-->`)
	htmlBreakRe  = regexp.MustCompile(`(?i)</(p|div|h[1-6]|li|tr|br|section|article|header|footer|td|th)>`)
	htmlTagRe    = regexp.MustCompile(`<[^>]+>`)
	htmlSpaceRe  = regexp.MustCompile(`[ \t]+`)
	htmlBlanksRe = regexp.MustCompile(`\n\s*\n+`)
)

// HTMLToText reduces a mock-up to its visible text — enough to understand
// and file it; the page embeds the original.
func HTMLToText(html string) (string, error) {
	title := ""
	if m := htmlTitleRe.FindStringSubmatch(html); m != nil {
		title = strings.TrimSpace(m[1])
	}
	body := htmlDropRe.ReplaceAllString(html, " ")
	body = htmlCommRe.ReplaceAllString(body, " ")
	body = htmlBreakRe.ReplaceAllString(body, "\n")
	body = htmlTagRe.ReplaceAllString(body, " ")
	body = strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'").Replace(body)
	body = htmlSpaceRe.ReplaceAllString(body, " ")
	body = htmlBlanksRe.ReplaceAllString(body, "\n\n")
	body = strings.TrimSpace(body)
	text := body
	if title != "" {
		text = "# " + title + "\n\n" + body
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("the HTML file has no visible text")
	}
	return text, nil
}
