// reqbased — the reqbase server: files + git + auth + PR review over a
// locally checked-out requirements workspace.
//
// Usage:
//
//	reqbased [-config reqbase.yml] [-dev]              serve
//	reqbased [-config reqbase.yml] user add <username> <name> <email>
//	reqbased init <dir> [-types requirements,specs,…] [-name project]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"golang.org/x/term"

	"reqbase/server/internal/ai"
	"reqbase/server/internal/api"
	"reqbase/server/internal/auth"
	"reqbase/server/internal/config"
	"reqbase/server/internal/events"
	"reqbase/server/internal/gitx"
	"reqbase/server/internal/scaffold"
	"reqbase/server/internal/store"
	"reqbase/server/internal/webui"
)

func main() {
	configPath := flag.String("config", "reqbase.yml", "path to config file")
	dev := flag.Bool("dev", false, "dev mode: UI served by Vite, dev_user honored")
	flag.Parse()

	var err error
	if args := flag.Args(); len(args) > 0 && args[0] == "user" {
		err = userCmd(*configPath, args[1:])
	} else if len(args) > 0 && args[0] == "init" {
		err = initCmd(args[1:])
	} else {
		err = serve(*configPath, *dev)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "reqbased:", err)
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

	// mirror the YAML repos into the built-in default tenant's registry
	// (docs/multi-tenancy.md — GitHub App installations add further tenants)
	def, err := st.EnsureTenant(gitx.DefaultTenant, "config", 0, "Workspace")
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

	git, err := gitx.NewManager(cfg)
	if err != nil {
		return err
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

	var oidcAuth *auth.OIDC
	if cfg.Auth.OIDC.Enabled {
		oidcAuth, err = auth.NewOIDC(context.Background(), cfg)
		if err != nil {
			return err
		}
		log.Printf("oidc enabled: issuer %s", cfg.Auth.OIDC.Issuer)
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
		Store:    st,
		Sessions: auth.NewSessions(st, cfg),
		OIDC:     oidcAuth,
		AI:       aiClient,
		Bus:      bus,
		Dist:     dist,
		Dev:      dev,
	})
	log.Printf("listening on %s (dev=%v)", cfg.Listen, dev)
	return http.ListenAndServe(cfg.Listen, handler)
}

// userCmd implements `reqbased user add <username> <name> <email>`.
func userCmd(configPath string, args []string) error {
	if len(args) != 4 || args[0] != "add" {
		return fmt.Errorf("usage: reqbased user add <username> <name> <email>")
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
		return fmt.Errorf("usage: reqbased init <dir> [-types %s] [-name project]", strings.Join(scaffold.DefaultTypes, ","))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return scaffold.Init(dir, *name, strings.Split(*typesFlag, ","))
}
