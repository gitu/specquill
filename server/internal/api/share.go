package api

// Share links: an unauthenticated download of the project's OKF bundle as a
// zip — the secret token in the URL is the only credential, so the link can
// be pasted straight into an LLM chat or fetched by an agent. Minting and
// revoking require the maintainer role (REQ-021: exporting protected-branch
// content to an unauthenticated URL is a protected-content decision);
// downloads do not.

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"specquill/server/internal/auth"
	"specquill/server/internal/authz"
	"specquill/server/internal/project"
	"specquill/server/internal/store"
)

// projectByID resolves a project id within a tenant without an HTTP context
// (share downloads have no session). Mirrors tenantProject's writable half;
// read-only sources are never shareable.
func (s *Server) projectByID(t *store.Tenant, id string) (*project.Project, bool) {
	if tp, err := s.store.TenantProject(t.ID, id); err == nil {
		repo, ok := s.git.Repo(t.Slug + "/" + tp.RepoID)
		if !ok {
			return nil, false
		}
		return project.New(repo, tp.ProjectID, tp.ContentRoot, false), true
	}
	repo, ok := s.git.Repo(t.Slug + "/" + id)
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

// shareAccess resolves the tenant + project of {repo} and gates on the
// effective per-repo role (share links are repo-scoped, so tenant-level
// roleH would wrongly deny grant-only members).
func (s *Server) shareAccess(w http.ResponseWriter, r *http.Request, minRole authz.Role) (*store.Tenant, bool) {
	t, ok := s.tenant(w, r)
	if !ok {
		return nil, false
	}
	id := r.PathValue("repo")
	p, ok := s.projectByID(t, id)
	if !ok {
		jsonError(w, http.StatusNotFound, "unknown project "+id)
		return nil, false
	}
	u := auth.UserFrom(r.Context())
	if s.effectiveRepoRole(u, t, p.Repo.Cfg.ID) < minRole {
		jsonError2(w, http.StatusForbidden, "requires "+minRole.String()+" role", "role_forbidden")
		return nil, false
	}
	return t, true
}

// GET /api/repos/{repo}/share — the project's current share link, if any.
func (s *Server) getShare(w http.ResponseWriter, r *http.Request) {
	t, ok := s.shareAccess(w, r, authz.Viewer)
	if !ok {
		return
	}
	l, err := s.store.ShareLink(t.ID, r.PathValue("repo"))
	if err != nil {
		jsonOK(w, map[string]any{"url": nil})
		return
	}
	jsonOK(w, shareResp(l))
}

// POST /api/repos/{repo}/share — mint (or rotate) the share token.
func (s *Server) createShare(w http.ResponseWriter, r *http.Request) {
	t, ok := s.shareAccess(w, r, authz.Maintainer)
	if !ok {
		return
	}
	id := r.PathValue("repo")
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	token := hex.EncodeToString(buf)
	u := auth.UserFrom(r.Context())
	if err := s.store.SetShareLink(t.ID, id, token, u.ID); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	l, err := s.store.ShareLink(t.ID, id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, shareResp(l))
}

// DELETE /api/repos/{repo}/share — revoke the link.
func (s *Server) deleteShare(w http.ResponseWriter, r *http.Request) {
	t, ok := s.shareAccess(w, r, authz.Maintainer)
	if !ok {
		return
	}
	if err := s.store.DeleteShareLink(t.ID, r.PathValue("repo")); err != nil {
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
	t, err := s.store.TenantByID(l.TenantID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, ok := s.projectByID(t, l.ProjectID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	zip, err := p.ArchiveZip(p.Cfg.DefaultBranch)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+l.ProjectID+`-okf.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(zip)
}
