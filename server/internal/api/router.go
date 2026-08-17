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
	"specquill/server/internal/authz"
	"specquill/server/internal/config"
	"specquill/server/internal/events"
	"specquill/server/internal/gitx"
	"specquill/server/internal/importer"
	"specquill/server/internal/store"
)

type Server struct {
	cfg          *config.Config
	git          *gitx.Manager // shared manager (local/dev mode git operations)
	fleet        *gitx.Fleet   // per-user managers (forge-PAT mode git operations)
	vault        *auth.TokenVault
	store        *store.Store
	sessions     *auth.Sessions
	ai           *ai.Client  // nil when disabled
	bus          *events.Bus // nil-safe
	devUser      *store.User
	srcCache     *srcCache        // grounding source snapshots, keyed by repo key + head SHA
	forgeCache   *forgeCache      // forge review threads, keyed by user + repo key + branch
	summaryCache *summaryCache    // per-commit AI summaries, keyed by repo id + sha (immutable)
	importer     *importer.Runner // nil when no non-git sources are configured
	drift        *driftRegistry   // in-flight source-drift runs, one per repo+branch
}

type Options struct {
	Store    *store.Store
	Sessions *auth.Sessions
	AI       *ai.Client       // nil when disabled
	Bus      *events.Bus      // nil-safe
	Importer *importer.Runner // nil when no non-git sources are configured
	Dist     fs.FS
	Dev      bool
}

func (s *Server) publish(kind, repo, branch string) {
	s.bus.Publish(events.Event{Kind: kind, Repo: repo, Branch: branch})
}

// New wires the REST endpoints and the embedded SPA. The Server behind the
// handler is returned too (NewServer) for callers that need its internals;
// most callers want just the handler.
func New(cfg *config.Config, git *gitx.Manager, opts Options) http.Handler {
	h, _ := NewServer(cfg, git, opts)
	return h
}

func NewServer(cfg *config.Config, git *gitx.Manager, opts Options) (http.Handler, *Server) {
	s := &Server{cfg: cfg, git: git, store: opts.Store, sessions: opts.Sessions, ai: opts.AI, bus: opts.Bus, importer: opts.Importer, srcCache: newSrcCache(), forgeCache: newForgeCache(), summaryCache: newSummaryCache(), vault: auth.NewTokenVault(), drift: newDriftRegistry()}
	// drift workers died with the previous process — re-running is the resume
	if n, err := opts.Store.MarkInterruptedDriftRuns(); err == nil && n > 0 {
		log.Printf("drift: marked %d interrupted run(s)", n)
	}
	if cfg.Auth.Forge.Enabled() {
		s.fleet = gitx.NewFleet(cfg)
		s.fleet.Notify = func(kind, repo, branch string) { s.publish(kind, repo, branch) }
		go s.vaultJanitor()
		if cfg.Dynamic.Enabled {
			go s.cloneJanitor()
		}
	}
	if opts.Dev && cfg.Auth.DevUser != nil {
		u, err := opts.Store.UpsertUser("local", "dev", cfg.Auth.DevUser.Name, cfg.Auth.DevUser.Email)
		if err == nil {
			s.devUser = u
			// the dev user administers the deployment (management API)
			_ = opts.Store.SetUserRole(u.ID, "admin")
			u.Role = "admin"
			log.Printf("dev mode: auto-authenticating as %s <%s>", u.Name, u.Email)
		}
	}

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("GET /api/me", s.me)
	apiMux.HandleFunc("GET /api/repos", s.listRepos)
	apiMux.HandleFunc("GET /api/projects", s.listProjects)
	apiMux.HandleFunc("POST /api/projects", s.roleH(authz.Admin, s.createProject))
	apiMux.HandleFunc("DELETE /api/projects/{id}", s.roleH(authz.Admin, s.deleteProject))
	apiMux.HandleFunc("POST /api/sources/{name}/sync", s.roleH(authz.Editor, s.syncSource))
	apiMux.HandleFunc("GET /api/members", s.roleH(authz.Admin, s.listMembers))
	apiMux.HandleFunc("GET /api/repos/{repo}/grants", s.roleH(authz.Admin, s.listGrants))
	apiMux.HandleFunc("POST /api/repos/{repo}/grants", s.roleH(authz.Admin, s.createGrant))
	apiMux.HandleFunc("DELETE /api/repos/{repo}/grants/{userId}", s.roleH(authz.Admin, s.deleteGrant))
	apiMux.HandleFunc("DELETE /api/repos/{repo}/grants/invites/{id}", s.roleH(authz.Admin, s.deleteGrantInvite))
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
	// repo-wide change feed (REQ-027); /history above is per-document
	apiMux.HandleFunc("GET /api/repos/{repo}/log", s.repoH(s.getLog))
	apiMux.HandleFunc("GET /api/repos/{repo}/commit", s.repoH(s.getCommit))
	apiMux.HandleFunc("GET /api/repos/{repo}/commit/summary", s.repoH(s.getCommitSummary))
	apiMux.HandleFunc("GET /api/repos/{repo}/status", s.writableViewH(s.getStatus))
	apiMux.HandleFunc("POST /api/repos/{repo}/commit", s.writableH(s.postCommit))
	apiMux.HandleFunc("POST /api/repos/{repo}/discard", s.writableH(s.postDiscard))
	apiMux.HandleFunc("POST /api/repos/{repo}/commit-message", s.writableH(s.postCommitMessage))
	apiMux.HandleFunc("POST /api/repos/{repo}/branches", s.writableH(s.postBranch))
	apiMux.HandleFunc("POST /api/repos/{repo}/push", s.writableH(s.postPush))
	apiMux.HandleFunc("POST /api/repos/{repo}/fetch", s.repoH(s.postFetch))
	apiMux.HandleFunc("POST /api/repos/{repo}/workspace", s.writableH(s.postWorkspace))
	apiMux.HandleFunc("POST /api/repos/{repo}/pull", s.writableH(s.postPull))
	apiMux.HandleFunc("GET /api/repos/{repo}/diff/worktree", s.writableH(s.getWorktreeDiff))
	apiMux.HandleFunc("GET /api/repos/{repo}/merge", s.writableViewH(s.getMergePreview))
	apiMux.HandleFunc("GET /api/repos/{repo}/forge/request", s.writableViewH(s.getForgeRequest))
	apiMux.HandleFunc("POST /api/repos/{repo}/merge", s.writableH(s.postMerge))
	apiMux.HandleFunc("POST /api/repos/{repo}/propose", s.writableH(s.postPropose))
	apiMux.HandleFunc("GET /api/repos/{repo}/share", s.getShare)
	apiMux.HandleFunc("POST /api/repos/{repo}/share", s.createShare)
	apiMux.HandleFunc("DELETE /api/repos/{repo}/share", s.deleteShare)
	apiMux.HandleFunc("GET /api/repos/{repo}/drift", s.writableViewH(s.getDrift))
	apiMux.HandleFunc("POST /api/repos/{repo}/drift/run", s.writableH(s.postDriftRun))
	apiMux.HandleFunc("POST /api/repos/{repo}/drift/cancel", s.writableH(s.postDriftCancel))
	apiMux.HandleFunc("POST /api/repos/{repo}/drift/focus", s.writableH(s.postDriftFocus))
	apiMux.HandleFunc("GET /api/repos/{repo}/alignment/recipes", s.writableViewH(s.getRecipes))
	apiMux.HandleFunc("POST /api/repos/{repo}/alignment/recipes/validate", s.writableViewH(s.postRecipeValidate))
	apiMux.HandleFunc("POST /api/repos/{repo}/drift/findings/{fp}/dismiss", s.writableH(s.postDriftDismiss))
	apiMux.HandleFunc("POST /api/repos/{repo}/drift/findings/{fp}/file", s.writableH(s.postDriftFile))
	apiMux.HandleFunc("POST /api/repos/{repo}/drift/findings/{fp}/draft", s.writableH(s.postDriftDraft))
	apiMux.HandleFunc("POST /api/repos/{repo}/drift/findings/{fp}/remedy", s.writableH(s.postDriftRemedy))
	apiMux.HandleFunc("POST /api/repos/{repo}/drift/findings/{fp}/plan", s.writableH(s.postDriftPlan))
	apiMux.HandleFunc("POST /api/repos/{repo}/drift/findings/{fp}/create", s.writableH(s.postDriftCreate))
	apiMux.HandleFunc("POST /api/repos/{repo}/linker/propose", s.writableH(s.postLinkerPropose))
	apiMux.HandleFunc("POST /api/repos/{repo}/linker/apply", s.writableH(s.postLinkerApply))
	apiMux.HandleFunc("POST /api/repos/{repo}/speccy/chat", s.writableH(s.speccyChat))
	apiMux.HandleFunc("POST /api/repos/{repo}/speccy/draft", s.writableH(s.speccyDraft))
	apiMux.HandleFunc("POST /api/repos/{repo}/speccy/title", s.writableViewH(s.postSpeccyTitle))
	// guided authoring (wizard.go). The stages themselves only read, but they
	// exist to produce a document — editor role, same gate as the chat, so a
	// viewer is refused up front instead of burning model tokens on a draft
	// they could never create.
	apiMux.HandleFunc("POST /api/repos/{repo}/speccy/related", s.writableH(s.speccyRelated))
	apiMux.HandleFunc("POST /api/repos/{repo}/speccy/interview", s.writableH(s.speccyInterview))
	apiMux.HandleFunc("POST /api/repos/{repo}/speccy/compose", s.writableH(s.speccyCompose))
	apiMux.HandleFunc("POST /api/repos/{repo}/speccy/section", s.writableH(s.speccySection))
	apiMux.HandleFunc("GET /api/speccy/info", s.speccyInfo)
	// token-scoped dynamic projects (REQ-025); /api/dynamic itself is served
	// even when off so the SPA can probe the feature
	apiMux.HandleFunc("GET /api/dynamic", s.dynamicInfo)
	apiMux.HandleFunc("POST /api/dynamic/open", s.dynH(s.dynamicOpen))
	apiMux.HandleFunc("GET /api/dynamic/search", s.dynH(s.dynamicSearch))
	apiMux.HandleFunc("GET /api/dynamic/checkouts", s.dynH(s.dynamicCheckouts))
	apiMux.HandleFunc("POST /api/dynamic/reclaim", s.dynH(s.dynamicReclaim))
	// legacy aliases: resolve the deployment's sole project
	apiMux.HandleFunc("POST /api/speccy/chat", s.speccyChatAlias)
	apiMux.HandleFunc("POST /api/speccy/draft", s.speccyDraftAlias)

	mux := http.NewServeMux()
	mux.Handle("/api/", s.requireAuth(apiMux))
	// public OKF-bundle download — the share token in the URL is the only
	// credential; {name} is the cosmetic filename and is not checked
	mux.HandleFunc("GET /share/{token}/{name}", s.shareDownload)
	mux.HandleFunc("GET /auth/login", s.authLogin)
	mux.HandleFunc("GET /auth/providers", s.authProviders)
	mux.HandleFunc("POST /auth/local/login", s.authLocalLogin)
	mux.HandleFunc("POST /auth/pat/login", s.authPatLogin)
	mux.HandleFunc("POST /auth/logout", s.authLogout)
	var spa http.Handler = spaHandler(opts.Dist, opts.Dev)
	if opts.Dev {
		spa = devViteProxy(spa)
	}
	mux.Handle("/", spa)
	return logMiddleware(csrfGuard(mux)), s
}

// repoH resolves the {repo} path segment and gates on the effective per-repo
// role (REQ-020): reading needs viewer. The resolved role rides the request
// context for writableH and handlers.
func (s *Server) repoH(h func(http.ResponseWriter, *http.Request, *project.Project)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo, ok := s.resolveProject(w, r)
		if !ok {
			return
		}
		u := auth.UserFrom(r.Context())
		role := s.effectiveRepoRole(u, repo.Repo.Cfg.ID)
		if role < authz.Viewer {
			jsonError2(w, http.StatusForbidden, "no access to repo "+repo.ID, "repo_forbidden")
			return
		}
		h(w, r.WithContext(withRepoRole(r.Context(), role)), repo)
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
