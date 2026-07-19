// specquill — the specquill server: files + git + auth + PR review over a
// locally checked-out requirements workspace.
//
// Usage:
//
//	specquill [-config specquill.yml] [-dev]              serve
//	specquill [-config specquill.yml] user add <username> <name> <email>
//	specquill init <dir> [-types requirements,specs,…] [-name project]
//	specquill add <type> [name] [-dir <workspace>]        new document
//	specquill validate [dir]                              OKF + link check
//	specquill graph [dir]                                 traceability DOT
//	specquill export [dir]                                model as JSON
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"specquill/server/internal/ai"
	"specquill/server/internal/api"
	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/events"
	"specquill/server/internal/githubapp"
	"specquill/server/internal/gitx"
	"specquill/server/internal/importer"
	"specquill/server/internal/scaffold"
	"specquill/server/internal/secrets"
	"specquill/server/internal/store"
	"specquill/server/internal/webui"
)

func main() {
	configPath := flag.String("config", "specquill.yml", "path to config file")
	dev := flag.Bool("dev", false, "dev mode: UI served by Vite, dev_user honored")
	flag.Parse()

	var err error
	args := flag.Args()
	switch {
	case len(args) > 0 && args[0] == "user":
		err = userCmd(*configPath, args[1:])
	case len(args) > 0 && args[0] == "init":
		err = initCmd(args[1:])
	case len(args) > 0 && args[0] == "add":
		err = addCmd(args[1:])
	case len(args) > 0 && args[0] == "validate":
		err = validateCmd(args[1:])
	case len(args) > 0 && args[0] == "graph":
		err = graphCmd(args[1:])
	case len(args) > 0 && args[0] == "export":
		err = exportCmd(args[1:])
	default:
		err = serve(*configPath, *dev)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "specquill:", err)
		os.Exit(1)
	}
}

func openStore(cfg *config.Config) (*store.Store, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}
	dsn, err := cfg.Database.DSN()
	if err != nil {
		return nil, err
	}
	return store.Open(dsn)
}

func serve(configPath string, dev bool) error {
	if err := gitx.CheckGitVersion(); err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	st, err := openStore(cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	release, err := gitx.LockDataDir(cfg.DataDir)
	if err != nil {
		return err
	}
	defer release()

	// mirror the YAML content into the deployment's config tenant, when one
	// is declared (REQ-022.4/5 — a hosted deployment boots tenant-less and
	// gains tenants through GitHub App installations)
	var def *store.Tenant
	if cfg.Tenant != nil {
		def, err = st.EnsureTenant(cfg.Tenant.Slug, "config", 0, cfg.Tenant.DisplayName)
		if err != nil {
			return err
		}
		decls := make([]store.TenantRepo, 0, len(cfg.Repos))
		for _, rc := range cfg.Repos {
			decls = append(decls, store.TenantRepo{
				RepoID: rc.ID, Mode: string(rc.Mode), Remote: rc.Remote, DefaultBranch: rc.DefaultBranch,
			})
		}
		if err := st.SyncTenantRepos(def.ID, decls); err != nil {
			return err
		}
		// projects + the global source catalog + config-tenant grants
		// (config-managed rows reconcile to the YAML; api-managed rows persist)
		projDecls := make([]store.Project, 0, len(cfg.Projects))
		for _, pc := range cfg.Projects {
			projDecls = append(projDecls, store.Project{ProjectID: pc.ID, RepoID: pc.ID, ContentRoot: pc.ContentRoot})
		}
		if err := st.SyncTenantProjects(def.ID, projDecls); err != nil {
			return err
		}
		srcDecls := make([]store.Source, 0, len(cfg.Sources))
		for _, sc := range cfg.Sources {
			srcDecls = append(srcDecls, store.Source{
				Name: sc.Name, Kind: sc.Kind, Remote: sc.Remote, TokenEnv: sc.TokenEnv,
				DefaultBranch: sc.DefaultBranch, SyncInterval: int64(sc.SyncInterval.Seconds()),
			})
		}
		if err := st.SyncGlobalSources(srcDecls); err != nil {
			return err
		}
		granted := cfg.Grants
		if len(granted) == 0 { // omitted = all sources (self-host convenience)
			for _, sc := range cfg.Sources {
				granted = append(granted, sc.Name)
			}
		}
		grantIDs := make([]int64, 0, len(granted))
		for _, name := range granted {
			src, err := st.SourceByName(def.ID, name)
			if err != nil {
				return fmt.Errorf("grants: source %s: %w", name, err)
			}
			grantIDs = append(grantIDs, src.ID)
		}
		if err := st.SyncGrants(def.ID, grantIDs); err != nil {
			return err
		}
	}

	git, err := gitx.NewManager(cfg)
	if err != nil {
		return err
	}

	// GitHub App: installation tokens authenticate git for github tenants
	var ghApp *githubapp.App
	if cfg.GitHubApp.Enabled() {
		ghApp, err = githubapp.New(cfg.GitHubApp)
		if err != nil {
			return err
		}
		log.Printf("github app enabled: app id %d", cfg.GitHubApp.AppID)
	}

	// Credential store (REQ-023): sealed tenant PATs. Absent the master key
	// the server runs normally — only the credential endpoints answer 501.
	secretsBox, err := secrets.NewFromEnv()
	if err != nil && !errors.Is(err, secrets.ErrNotConfigured) {
		return err
	}
	if secretsBox != nil {
		log.Printf("credential store enabled (%s)", secrets.EnvKey)
	}

	// One credential resolution chain (REQ-023.4), wired before any AddRepo:
	// installation token > attached tenant credential > token_env fallback
	// (gitx applies token_env itself when this hook declines).
	git.TokenFor = func(r *gitx.Repo) (string, string, bool) {
		if ghApp != nil {
			if ten, err := st.TenantBySlug(r.Tenant()); err == nil && ten.Provider == "github" && ten.Installation != 0 {
				tok, err := ghApp.InstallationToken(ten.Installation)
				if err != nil {
					log.Printf("github app: token for %s: %v", r.Key(), err)
					return "", "", false
				}
				return "x-access-token", tok, true
			}
		}
		if secretsBox != nil {
			if c, err := st.RepoCredential(r.Tenant(), r.Cfg.ID); err == nil {
				tok, err := secretsBox.Open(c.TenantID, c.Nonce, c.Ciphertext, c.KeyID)
				if err != nil {
					// log without token material and fall through the chain
					log.Printf("credential %d (%s) for %s: %v", c.ID, c.Name, r.Key(), err)
				} else {
					user := c.Username
					if user == "" {
						user = "x-access-token"
					}
					return user, tok, true
				}
			}
		}
		if ghApp != nil {
			// config-tenant repos on github.com ride the app too when it is
			// installed on them — no PAT needed; anything else (app not
			// installed, non-GitHub host) falls back to token_env
			if full, ok := githubapp.RepoFromRemote(r.Cfg.Remote); ok {
				inst, err := ghApp.RepoInstallation(full)
				if err != nil {
					if err != githubapp.ErrNotInstalled {
						log.Printf("github app: installation for %s: %v", full, err)
					}
					return "", "", false
				}
				tok, err := ghApp.InstallationToken(inst)
				if err != nil {
					log.Printf("github app: token for %s: %v", r.Key(), err)
					return "", "", false
				}
				return "x-access-token", tok, true
			}
		}
		return "", "", false
	}

	// api-managed repos (added in-app) survive reconciliation — re-register
	// them with the manager so their projects resolve after a restart
	if repos, err := st.TenantRepos(def.ID); err == nil {
		for _, tr := range repos {
			if tr.ManagedBy != "api" {
				continue
			}
			mode := config.ReadOnly
			if tr.Mode == string(config.Writable) {
				mode = config.Writable
			}
			if _, err := git.AddRepo(def.Slug, config.RepoConfig{
				ID: tr.RepoID, Mode: mode, Remote: tr.Remote, DefaultBranch: tr.DefaultBranch,
				SyncInterval:      2 * time.Minute,
				ProtectedBranches: []string{tr.DefaultBranch},
			}); err != nil {
				log.Printf("api-managed repo %s: %v", tr.RepoID, err)
			}
		}
	}
	// github tenants: re-register their persisted repos too (clones happen
	// through the installation-token TokenFor above)
	if ghApp != nil {
		tens, err := st.TenantsByProvider("github")
		if err != nil {
			return err
		}
		for _, ten := range tens {
			repos, err := st.TenantRepos(ten.ID)
			if err != nil {
				continue
			}
			for _, tr := range repos {
				mode := config.ReadOnly
				if tr.Mode == string(config.Writable) {
					mode = config.Writable
				}
				if _, err := git.AddRepo(ten.Slug, config.RepoConfig{
					ID: tr.RepoID, Mode: mode, Remote: tr.Remote, DefaultBranch: tr.DefaultBranch,
					SyncInterval:      2 * time.Minute,
					ProtectedBranches: []string{tr.DefaultBranch},
				}); err != nil {
					log.Printf("github tenant repo %s/%s: %v", ten.Slug, tr.RepoID, err)
				}
			}
		}
	}

	bus := events.New()
	git.Notify = func(kind, repo, branch string) {
		bus.Publish(events.Event{Kind: kind, Repo: repo, Branch: branch})
	}
	log.Printf("initializing %d repo(s) under %s", len(cfg.Repos), cfg.DataDir)
	if err := git.Init(); err != nil {
		return err
	}
	git.StartSyncLoops()

	// importer.Runner materializes non-git sources (url/openapi/confluence) into
	// their mirror repos on a schedule; git.Init() has already created the empty
	// bare repos, Start() does the first import
	imp := importer.NewRunner(git, st)
	if def != nil {
		for _, sc := range cfg.Sources {
			if !sc.IsGit() {
				imp.Register(def.Slug, def.ID, sc)
				log.Printf("importer registered: %s (%s)", sc.Name, sc.Kind)
			}
		}
	}
	imp.Start(context.Background())

	var oidcAuth *auth.OIDC
	if cfg.Auth.OIDC.Enabled {
		oidcAuth, err = auth.NewOIDC(context.Background(), cfg)
		if err != nil {
			return err
		}
		log.Printf("oidc enabled: issuer %s", cfg.Auth.OIDC.Issuer)
	}

	var githubAuth *auth.GitHub
	if cfg.Auth.GitHub.Enabled {
		githubAuth = auth.NewGitHub(cfg)
		log.Printf("github login enabled: client %s (%d allowed users)", cfg.Auth.GitHub.ClientID, len(cfg.Auth.GitHub.AllowedUsers))
	}

	var aiClient *ai.Client
	if cfg.AI.Enabled {
		aiClient = ai.New(cfg.AI)
		log.Printf("copilot enabled: %s @ %s", cfg.AI.Model, cfg.AI.BaseURL)
	}

	dist, err := webui.Dist()
	if err != nil {
		return err
	}
	handler := api.New(cfg, git, api.Options{
		Secrets:   secretsBox,
		Store:     st,
		Sessions:  auth.NewSessions(st, cfg),
		OIDC:      oidcAuth,
		GitHub:    githubAuth,
		GitHubApp: ghApp,
		AI:        aiClient,
		Bus:       bus,
		Importer:  imp,
		Dist:      dist,
		Dev:       dev,
	})
	log.Printf("listening on %s (dev=%v)", cfg.Listen, dev)
	return http.ListenAndServe(cfg.Listen, handler)
}

// userCmd implements `specquill user add <username> <name> <email>`.
func userCmd(configPath string, args []string) error {
	if len(args) != 4 || args[0] != "add" {
		return fmt.Errorf("usage: specquill user add <username> <name> <email>")
	}
	username, name, email := args[1], args[2], args[3]
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	st, err := openStore(cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	var password string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "password: ")
		raw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		password = string(raw)
	} else { // piped: read one line from stdin (for scripting/tests)
		if _, err := fmt.Fscanln(os.Stdin, &password); err != nil {
			return fmt.Errorf("read password from stdin: %w", err)
		}
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := st.AddLocalUser(username, name, email, hash); err != nil {
		return err
	}
	fmt.Printf("local user %q (%s <%s>) created\n", username, name, email)
	return nil
}

// initCmd scaffolds a new workspace repository (folders, schema, AI skills).
func initCmd(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	typesFlag := fs.String("types", strings.Join(scaffold.DefaultTypes, ","),
		"spec types to onboard (available: "+strings.Join(scaffold.AllTypes(), ", ")+")")
	name := fs.String("name", "", "project name (default: directory name)")
	// accept both `init <dir> -types …` and `init -types … <dir>`
	var dir string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		dir, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dir == "" && fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	if dir == "" {
		return fmt.Errorf("usage: specquill init <dir> [-types %s] [-name project]", strings.Join(scaffold.DefaultTypes, ","))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return scaffold.Init(dir, *name, strings.Split(*typesFlag, ","))
}
