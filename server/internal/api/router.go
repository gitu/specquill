// Package api wires the REST endpoints and serves the embedded SPA.
package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"specquill/server/internal/project"
	"strings"
	"time"

	"specquill/server/internal/ai"
	"specquill/server/internal/auth"
	"specquill/server/internal/collab"
	"specquill/server/internal/config"
	"specquill/server/internal/events"
	"specquill/server/internal/gitx"
	"specquill/server/internal/importer"
	"specquill/server/internal/store"
)

type Server struct {
	cfg      *config.Config
	git      *gitx.Manager
	store    *store.Store
	sessions *auth.Sessions
	oidc     *auth.OIDC
	github   *auth.GitHub
	ai       *ai.Client  // nil when disabled
	bus      *events.Bus // nil-safe
	hub      *collab.Hub
	devUser  *store.User
	srcCache *srcCache        // grounding source snapshots, keyed by repo key + head SHA
	importer *importer.Runner // nil when no non-git sources are configured
}

type Options struct {
	Store    *store.Store
	Sessions *auth.Sessions
	OIDC     *auth.OIDC   // nil when disabled
	GitHub   *auth.GitHub // nil when disabled
	AI       *ai.Client  // nil when disabled
	Bus      *events.Bus // nil-safe
	Hub      *collab.Hub
	Importer *importer.Runner // nil when no non-git sources are configured
	Dist     fs.FS
	Dev      bool
}

func (s *Server) publish(kind, repo, branch string) {
	s.bus.Publish(events.Event{Kind: kind, Repo: repo, Branch: branch})
}

func New(cfg *config.Config, git *gitx.Manager, opts Options) http.Handler {
	s := &Server{cfg: cfg, git: git, store: opts.Store, sessions: opts.Sessions, oidc: opts.OIDC, github: opts.GitHub, ai: opts.AI, bus: opts.Bus, hub: opts.Hub, importer: opts.Importer, srcCache: newSrcCache()}
	if s.hub == nil {
		s.hub = collab.NewHub(opts.Store, git)
	}
	if opts.Dev && cfg.Auth.DevUser != nil {
		u, err := opts.Store.UpsertUser("local", "dev", cfg.Auth.DevUser.Name, cfg.Auth.DevUser.Email)
		if err == nil {
			s.devUser = u
			// the dev user administers the default tenant (management API)
			if def, err := opts.Store.TenantBySlug(gitx.DefaultTenant); err == nil {
				_ = opts.Store.EnsureMember(def.ID, u.ID, "admin")
				_ = opts.Store.SetMemberRole(def.ID, u.ID, "admin")
			}
			log.Printf("dev mode: auto-authenticating as %s <%s>", u.Name, u.Email)
		}
	}

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("GET /api/me", s.me)
	apiMux.HandleFunc("GET /api/repos", s.listRepos)
	apiMux.HandleFunc("GET /api/projects", s.listProjects)
	apiMux.HandleFunc("POST /api/projects", s.roleH("admin", s.createProject))
	apiMux.HandleFunc("DELETE /api/projects/{id}", s.roleH("admin", s.deleteProject))
	apiMux.HandleFunc("POST /api/sources/{name}/sync", s.roleH("member", s.syncSource))
	apiMux.HandleFunc("GET /api/repos/{repo}/tree", s.repoH(s.getTree))
	apiMux.HandleFunc("GET /api/repos/{repo}/linkcheck", s.repoH(s.getLinkCheck))
	apiMux.HandleFunc("GET /api/repos/{repo}/snapshot", s.repoH(s.getSnapshot))
	apiMux.HandleFunc("GET /api/repos/{repo}/files/{path...}", s.repoH(s.getFile))
	apiMux.HandleFunc("GET /api/repos/{repo}/raw/{path...}", s.repoH(s.getRaw))
	apiMux.HandleFunc("PUT /api/repos/{repo}/raw/{path...}", s.writableH(s.putRaw))
	apiMux.HandleFunc("POST /api/repos/{repo}/assets", s.writableH(s.postAsset))
	apiMux.HandleFunc("GET /api/repos/{repo}/branches", s.repoH(s.listBranches))
	apiMux.HandleFunc("PUT /api/repos/{repo}/files/{path...}", s.writableH(s.putFile))
	apiMux.HandleFunc("DELETE /api/repos/{repo}/files/{path...}", s.writableH(s.deleteFile))
	apiMux.HandleFunc("POST /api/repos/{repo}/move", s.writableH(s.postMove))
	apiMux.HandleFunc("GET /api/repos/{repo}/history", s.repoH(s.getHistory))
	apiMux.HandleFunc("GET /api/repos/{repo}/status", s.writableH(s.getStatus))
	apiMux.HandleFunc("POST /api/repos/{repo}/commit", s.writableH(s.postCommit))
	apiMux.HandleFunc("POST /api/repos/{repo}/commit-message", s.writableH(s.postCommitMessage))
	apiMux.HandleFunc("POST /api/repos/{repo}/branches", s.writableH(s.postBranch))
	apiMux.HandleFunc("POST /api/repos/{repo}/push", s.writableH(s.postPush))
	apiMux.HandleFunc("POST /api/repos/{repo}/fetch", s.repoH(s.postFetch))
	apiMux.HandleFunc("POST /api/repos/{repo}/workspace", s.writableH(s.postWorkspace))
	apiMux.HandleFunc("POST /api/repos/{repo}/pull", s.writableH(s.postPull))
	apiMux.HandleFunc("GET /api/repos/{repo}/diff/worktree", s.writableH(s.getWorktreeDiff))
	apiMux.HandleFunc("GET /api/repos/{repo}/collab/{path...}", s.writableH(s.collabWS))
	apiMux.HandleFunc("GET /api/repos/{repo}/presence", s.writableH(s.getPresence))
	apiMux.HandleFunc("GET /api/repos/{repo}/prs", s.writableH(s.listPRs))
	apiMux.HandleFunc("POST /api/repos/{repo}/prs", s.writableH(s.createPR))
	apiMux.HandleFunc("GET /api/repos/{repo}/prs/{n}", s.writableH(s.getPR))
	apiMux.HandleFunc("GET /api/repos/{repo}/prs/{n}/diff", s.writableH(s.getPRDiff))
	apiMux.HandleFunc("GET /api/repos/{repo}/prs/{n}/comments", s.writableH(s.prComments))
	apiMux.HandleFunc("POST /api/repos/{repo}/prs/{n}/comments", s.writableH(s.prComments))
	apiMux.HandleFunc("POST /api/repos/{repo}/prs/{n}/approve", s.writableH(s.approvePR))
	apiMux.HandleFunc("POST /api/repos/{repo}/prs/{n}/merge", s.writableH(s.mergePR))
	apiMux.HandleFunc("POST /api/repos/{repo}/prs/{n}/close", s.writableH(s.closePR))
	apiMux.HandleFunc("GET /api/repos/{repo}/share", s.getShare)
	apiMux.HandleFunc("POST /api/repos/{repo}/share", s.roleH("member", s.createShare))
	apiMux.HandleFunc("DELETE /api/repos/{repo}/share", s.roleH("member", s.deleteShare))
	apiMux.HandleFunc("POST /api/repos/{repo}/copilot/chat", s.writableH(s.copilotChat))
	apiMux.HandleFunc("POST /api/repos/{repo}/copilot/draft", s.writableH(s.copilotDraft))
	apiMux.HandleFunc("GET /api/copilot/info", s.copilotInfo)
	// legacy aliases: resolve the tenant's sole project
	apiMux.HandleFunc("POST /api/copilot/chat", s.copilotChatAlias)
	apiMux.HandleFunc("POST /api/copilot/draft", s.copilotDraftAlias)

	mux := http.NewServeMux()
	mux.Handle("/api/", s.requireAuth(apiMux))
	// public OKF-bundle download — the share token in the URL is the only
	// credential; {name} is the cosmetic filename and is not checked
	mux.HandleFunc("GET /share/{token}/{name}", s.shareDownload)
	mux.HandleFunc("GET /auth/login", s.authLogin)
	mux.HandleFunc("GET /auth/callback", s.authCallback)
	mux.HandleFunc("GET /auth/providers", s.authProviders)
	mux.HandleFunc("GET /auth/github/login", s.authGitHubLogin)
	mux.HandleFunc("GET /auth/github/callback", s.authGitHubCallback)
	mux.HandleFunc("POST /auth/local/login", s.authLocalLogin)
	mux.HandleFunc("POST /auth/logout", s.authLogout)
	mux.Handle("/", spaHandler(opts.Dist, opts.Dev))
	return logMiddleware(csrfGuard(mux))
}

// repoH resolves the {repo} path segment within the request's tenant.
func (s *Server) repoH(h func(http.ResponseWriter, *http.Request, *project.Project)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo, ok := s.tenantProject(w, r)
		if !ok {
			return
		}
		h(w, r, repo)
	}
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func jsonError2(w http.ResponseWriter, status int, msg, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg, "code": code})
}

// gitFail maps gitx errors onto HTTP responses.
func gitFail(w http.ResponseWriter, err error) {
	if errors.Is(err, gitx.ErrProtected) {
		jsonError2(w, http.StatusForbidden, err.Error(), "protected_branch")
		return
	}
	if errors.Is(err, gitx.ErrStale) {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	var ge *gitx.GitError
	if errors.As(err, &ge) {
		jsonError(w, http.StatusBadRequest, ge.Error())
		return
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not found"), strings.Contains(msg, "does not exist"):
		jsonError(w, http.StatusNotFound, msg)
	case strings.Contains(msg, "read-only"):
		jsonError(w, http.StatusForbidden, msg)
	case strings.Contains(msg, "invalid path"):
		jsonError(w, http.StatusBadRequest, msg)
	default:
		jsonError(w, http.StatusInternalServerError, msg)
	}
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}

// spaHandler serves the embedded SPA build; unknown paths fall back to
// index.html so client-side routes deep-link. Without an embedded build
// (fresh checkout, UI served by Vite) it returns a hint instead.
func spaHandler(dist fs.FS, dev bool) http.Handler {
	fileServer := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f, err := dist.Open("index.html"); err != nil {
			jsonError(w, http.StatusNotFound, "no embedded UI build — run `make web` or use the Vite dev server")
			return
		} else {
			_ = f.Close()
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := dist.Open(p); err == nil {
				_ = f.Close()
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, dist, "index.html")
	})
}
