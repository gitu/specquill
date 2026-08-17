package api

// Share links: an unauthenticated download of the project's OKF bundle as a
// zip — the secret token in the URL is the only credential, so the link can
// be pasted straight into an LLM chat or fetched by an agent. Minting and
// revoking require a session (member role); downloads do not.

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"

	"specquill/server/internal/auth"
	"specquill/server/internal/authz"
	"specquill/server/internal/gitx"
	"specquill/server/internal/project"
	"specquill/server/internal/store"
)

// projectByID resolves a project id without an HTTP context (share downloads
// have no session), against an explicit manager. Mirrors resolveProject's
// writable half; read-only sources are never shareable.
func (s *Server) projectByID(mgr *gitx.Manager, id string) (*project.Project, bool) {
	if tp, err := s.store.Project(id); err == nil {
		repo, ok := mgr.Repo(tp.RepoID)
		if !ok {
			return nil, false
		}
		return project.New(repo, tp.ProjectID, tp.ContentRoot, false), true
	}
	repo, ok := mgr.Repo(id)
	if !ok || !repo.Writable() {
		return nil, false
	}
	return project.New(repo, id, "", false), true
}

func shareResp(l *store.ShareLink) map[string]any {
	return map[string]any{
		"url":       "/share/" + l.Token + "/" + l.ProjectID + "-okf.zip",
		"createdAt": l.CreatedAt,
	}
}

// shareAccess resolves the project of {repo} and gates on the effective
// per-repo role (share links are repo-scoped, so deployment-level roleH
// would wrongly deny grant-only members).
func (s *Server) shareAccess(w http.ResponseWriter, r *http.Request, minRole authz.Role) bool {
	id := r.PathValue("repo")
	p, ok := s.projectByID(s.gitm(r), id)
	if !ok {
		jsonError(w, http.StatusNotFound, "unknown project "+id)
		return false
	}
	u := auth.UserFrom(r.Context())
	if s.effectiveRepoRole(u, p.Repo.Cfg.ID) < minRole {
		jsonError2(w, http.StatusForbidden, "requires "+minRole.String()+" role", "role_forbidden")
		return false
	}
	return true
}

// GET /api/repos/{repo}/share — the project's current share link, if any.
func (s *Server) getShare(w http.ResponseWriter, r *http.Request) {
	if !s.shareAccess(w, r, authz.Viewer) {
		return
	}
	l, err := s.store.ShareLink(r.PathValue("repo"))
	if err != nil {
		jsonOK(w, map[string]any{"url": nil})
		return
	}
	jsonOK(w, shareResp(l))
}

// POST /api/repos/{repo}/share — mint (or rotate) the share token.
func (s *Server) createShare(w http.ResponseWriter, r *http.Request) {
	if !s.shareAccess(w, r, authz.Maintainer) {
		return
	}
	id := r.PathValue("repo")
	// forge-PAT mode: the public download has no session and therefore no
	// token, so it can only read a clone that already exists. Minting is the
	// last moment we hold the creator's token — materialize it now, or the
	// link would 404 later.
	if s.patMode() {
		p, ok := s.projectByID(s.gitm(r), id)
		if !ok {
			jsonError(w, http.StatusNotFound, "unknown project "+id)
			return
		}
		if err := p.Repo.EnsureCloned(s.tok(r)); err != nil {
			jsonError2(w, http.StatusBadGateway, "clone failed: "+err.Error(), "clone_failed")
			return
		}
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	token := hex.EncodeToString(buf)
	u := auth.UserFrom(r.Context())
	if err := s.store.SetShareLink(id, token, u.ID); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	l, err := s.store.ShareLink(id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, shareResp(l))
}

// DELETE /api/repos/{repo}/share — revoke the link.
func (s *Server) deleteShare(w http.ResponseWriter, r *http.Request) {
	if !s.shareAccess(w, r, authz.Maintainer) {
		return
	}
	if err := s.store.DeleteShareLink(r.PathValue("repo")); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

// GET /share/{token}/{name} — the public download. No session: knowing the
// token IS the authorization. Serves the DEFAULT branch only (the reviewed,
// merged state — drafts and workspace branches never leak through a link).
func (s *Server) shareDownload(w http.ResponseWriter, r *http.Request) {
	l, err := s.store.ShareLinkByToken(r.PathValue("token"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// forge-PAT mode: serve from the minting user's clone — the download has
	// no session, so the only content it may expose is what the link's
	// creator could already see
	mgr := s.git
	if s.patMode() {
		mgr = s.fleet.ForUser(l.CreatedBy)
	}
	p, ok := s.projectByID(mgr, l.ProjectID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	zip, err := p.ArchiveZip(p.Cfg.DefaultBranch)
	if err != nil {
		// no clone to serve from (evicted, or the creator's storage is gone).
		// Nothing token-less can fix that, and a raw git error would leak
		// server paths — the link is simply not servable right now.
		log.Printf("share download %s: %v", l.ProjectID, err)
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+l.ProjectID+`-okf.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(zip)
}
