package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"

	"specquill/server/internal/gitx"
)

// image/asset types we serve raw and accept as uploads
var assetTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
}

const maxAssetSize = 10 << 20 // 10 MiB

// GET /api/repos/{repo}/raw/{path...}?ref= — raw blob bytes (binary-safe;
// the JSON /files endpoint would mangle non-UTF-8 content). Images embedded
// in documents load through this.
func (s *Server) getRaw(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	p := r.PathValue("path")
	ct, ok := assetTypes[strings.ToLower(path.Ext(p))]
	if !ok {
		ct = "application/octet-stream"
	}
	content, sha, err := repo.File(r.URL.Query().Get("ref"), p)
	if err != nil {
		gitFail(w, err)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("ETag", `"`+sha+`"`)
	w.Header().Set("Cache-Control", "private, max-age=60")
	if match := r.Header.Get("If-None-Match"); match == `"`+sha+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = io.WriteString(w, content)
}

var assetNameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// POST /api/repos/{repo}/assets?branch=&dir= — multipart image upload into
// the branch worktree (a normal uncommitted save). Returns the repo-relative
// path for embedding.
func (s *Server) postAsset(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	if err := r.ParseMultipartForm(maxAssetSize); err != nil {
		jsonError(w, http.StatusBadRequest, "parse upload: "+err.Error())
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer f.Close()
	ext := strings.ToLower(path.Ext(hdr.Filename))
	if _, ok := assetTypes[ext]; !ok {
		jsonError(w, http.StatusBadRequest, "unsupported image type: "+ext)
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxAssetSize+1))
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(data) > maxAssetSize {
		jsonError(w, http.StatusRequestEntityTooLarge, "image exceeds 10 MiB")
		return
	}

	branch := r.URL.Query().Get("branch")
	dir := strings.Trim(r.URL.Query().Get("dir"), "/")
	base := strings.TrimSuffix(path.Base(hdr.Filename), ext)
	base = strings.Trim(assetNameRe.ReplaceAllString(base, "-"), "-.")
	if base == "" {
		base = "image"
	}

	// uniquify: SaveFile with an empty baseSha refuses existing files (ErrStale)
	var sha, rel string
	for i := 0; i < 100; i++ {
		name := base + ext
		if i > 0 {
			name = fmt.Sprintf("%s-%d%s", base, i+1, ext)
		}
		rel = path.Join(dir, name)
		sha, err = repo.SaveFile(branch, rel, string(data), "")
		if err == nil {
			break
		}
		if !errors.Is(err, gitx.ErrStale) {
			gitFail(w, err)
			return
		}
	}
	if err != nil {
		jsonError(w, http.StatusConflict, "could not find a free filename for "+base+ext)
		return
	}
	s.publish("save", repo.Key(), branch)
	jsonOK(w, map[string]string{"path": rel, "sha": sha})
}

// PUT /api/repos/{repo}/raw/{path...}?branch=&baseSha= — binary-safe file
// save (excalidraw PNGs etc.); same optimistic-concurrency contract as the
// JSON files endpoint.
func (s *Server) putRaw(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	p := r.PathValue("path")
	branch := r.URL.Query().Get("branch")
	if s.hub.RoomActive(repo.Key(), repo.ResolveRef(branch), p) {
		jsonError2(w, http.StatusConflict, "a live co-editing session owns this file", "room_active")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxAssetSize+1))
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(data) > maxAssetSize {
		jsonError(w, http.StatusRequestEntityTooLarge, "file exceeds 10 MiB")
		return
	}
	sha, err := repo.SaveFile(branch, p, string(data), r.URL.Query().Get("baseSha"))
	if err != nil {
		gitFail(w, err)
		return
	}
	s.publish("save", repo.Key(), branch)
	jsonOK(w, map[string]string{"sha": sha})
}
