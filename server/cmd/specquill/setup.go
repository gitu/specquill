package main

// Interactive onboarding: `specquill setup` walks through the minimum viable
// configuration and writes a commented specquill.yml. `serve` offers it
// automatically when the config file does not exist and stdin is a terminal.

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"

	"specquill/server/internal/config"
)

type prompter struct {
	r *bufio.Reader
	w io.Writer
}

func (p *prompter) readLine() string {
	line, _ := p.r.ReadString('\n')
	return strings.TrimSpace(line)
}

// ask prints "label [def]: " and returns the answer, or def on empty input.
func (p *prompter) ask(label, def string) string {
	if def != "" {
		fmt.Fprintf(p.w, "  %s [%s]: ", label, def)
	} else {
		fmt.Fprintf(p.w, "  %s: ", label)
	}
	if v := p.readLine(); v != "" {
		return v
	}
	return def
}

func (p *prompter) askRequired(label string) string {
	for {
		if v := p.ask(label, ""); v != "" {
			return v
		}
		fmt.Fprintln(p.w, "  (required)")
	}
}

func (p *prompter) askBool(label string, def bool) bool {
	d := "y/N"
	if def {
		d = "Y/n"
	}
	switch strings.ToLower(p.ask(label+" ("+d+")", "")) {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	default:
		return def
	}
}

func (p *prompter) askChoice(label string, choices []string, def string) string {
	for {
		v := strings.ToLower(p.ask(label+" ("+strings.Join(choices, "/")+")", def))
		for _, c := range choices {
			if v == c {
				return v
			}
		}
		fmt.Fprintln(p.w, "  (choose one of: "+strings.Join(choices, ", ")+")")
	}
}

// yamlv renders one scalar YAML-safe (quotes only when the value needs them).
func yamlv(v string) string {
	b, _ := yaml.Marshal(v)
	return strings.TrimSpace(string(b))
}

// defaultBaseURL derives the base-URL prompt default from the listen address:
// ":8080" → http://localhost:8080; "0.0.0.0:8080" (bind-all) also becomes
// localhost, a concrete host stays as-is.
func defaultBaseURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		return "http://localhost:8080"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// projectIDFromRemote derives a default project id from the repo name:
// https://host/acme/trading-specs.git → trading-specs.
func projectIDFromRemote(remote string) string {
	s := strings.TrimSuffix(strings.TrimRight(remote, "/"), ".git")
	if i := strings.LastIndexAny(s, "/:"); i >= 0 {
		s = s[i+1:]
	}
	return strings.ToLower(s)
}

func setupCmd(configPath string, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: specquill [-config %s] setup", configPath)
	}
	return runSetup(os.Stdin, os.Stderr, configPath)
}

// runSetup drives the wizard on in/out and writes the config to configPath.
// Split out (and fed a plain reader) so tests can script it.
func runSetup(in io.Reader, out io.Writer, configPath string) error {
	p := &prompter{r: bufio.NewReader(in), w: out}

	if _, err := os.Stat(configPath); err == nil {
		fmt.Fprintf(out, "%s already exists.\n", configPath)
		if !p.askBool("overwrite it?", false) {
			return fmt.Errorf("aborted — nothing written")
		}
	}

	fmt.Fprintln(out, "\n== SpecQuill setup — server ==")
	listen := p.ask("listen address", ":8080")
	baseURL := p.ask("public base URL", defaultBaseURL(listen))
	dataDir := p.ask("data directory (clones, drafts, store)", "./data")

	fmt.Fprintln(out, "\n== Workspace project ==")
	fmt.Fprintln(out, "  The writable git repository holding your requirements workspace.")
	remote := p.askRequired("git remote URL (or local path)")
	projectID := p.ask("project id", projectIDFromRemote(remote))
	branch := p.ask("default branch", "main")
	contentRoot := p.ask("content root subfolder (empty = repo root)", "")

	fmt.Fprintln(out, "\n== Authentication ==")
	fmt.Fprintln(out, "  forge — users sign in with a GitLab/GitHub personal access token;")
	fmt.Fprintln(out, "          every git operation uses the user's own token (recommended)")
	fmt.Fprintln(out, "  local — username/password accounts managed by this server")
	mode := p.askChoice("auth mode", []string{"forge", "local"}, "forge")
	var forgeKind, forgeBase, tokenEnv string
	if mode == "forge" {
		forgeKind = p.askChoice("forge kind", []string{"gitlab", "github"}, "gitlab")
		forgeBase = p.ask("self-hosted forge base URL (empty for "+forgeKind+".com)", "")
	} else {
		tokenEnv = p.ask("env var holding the project's git token (empty = remote needs no auth)", "SPECQUILL_TOKEN")
	}
	admins := p.ask("admin email(s), comma-separated (get the admin role on login)", "")

	fmt.Fprintln(out, "\n== Git identity ==")
	fmt.Fprintln(out, "  Recorded as a Co-authored-by trailer; the logged-in user stays author.")
	committer := p.ask("committer name", "specquill")
	host := "local"
	if u, err := url.Parse(baseURL); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	committerMail := p.ask("committer email", "specquill@"+host)

	fmt.Fprintln(out, "\n== Speccy (optional) ==")
	aiOn := p.askBool("enable the AI speccy (any OpenAI-compatible endpoint)?", false)
	var aiBase, aiModel, aiQuick, aiKeyEnv string
	if aiOn {
		aiBase = p.ask("API base URL", "https://api.openai.com/v1")
		aiModel = p.ask("model (chat + draft edits)", "gpt-4o")
		aiQuick = p.ask("quick model (commit messages)", "gpt-4o-mini")
		aiKeyEnv = p.ask("env var holding the API key (empty for keyless/local)", "SPECQUILL_AI_KEY")
	}

	cookieSecure := strings.HasPrefix(baseURL, "https://")

	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w("# specquill server configuration — generated by `specquill setup`")
	w("# Reference for every option: specquill.example.yml")
	w("")
	w("listen: %s", yamlv(listen))
	w("data_dir: %s", yamlv(dataDir))
	w("base_url: %s", yamlv(baseURL))
	w("")
	w("projects:")
	w("  - id: %s", yamlv(projectID))
	w("    remote: %s", yamlv(remote))
	w("    default_branch: %s", yamlv(branch))
	if contentRoot != "" {
		w("    content_root: %s", yamlv(contentRoot))
	}
	if tokenEnv != "" {
		w("    token_env: %s   # export this before starting the server", yamlv(tokenEnv))
	}
	w("")
	w("git:")
	w("  committer_name: %s", yamlv(committer))
	w("  committer_email: %s", yamlv(committerMail))
	w("")
	w("auth:")
	if mode == "forge" {
		w("  forge:")
		w("    kind: %s", yamlv(forgeKind))
		if forgeBase != "" {
			w("    base_url: %s", yamlv(forgeBase))
		}
	} else {
		w("  local:")
		w("    enabled: true                # add users with `specquill user add`")
	}
	if admins != "" {
		var list []string
		for _, a := range strings.Split(admins, ",") {
			if a = strings.TrimSpace(a); a != "" {
				list = append(list, yamlv(a))
			}
		}
		w("  admin_emails: [%s]", strings.Join(list, ", "))
	} else {
		w("  # admin_emails: [you@example.com]   # nobody gets the admin role without this")
	}
	w("")
	w("session:")
	w("  ttl: 10m")
	w("  cookie_secure: %v", cookieSecure)
	if aiOn {
		w("")
		w("ai:")
		w("  enabled: true")
		w("  base_url: %s", yamlv(aiBase))
		w("  model: %s", yamlv(aiModel))
		w("  quick_model: %s", yamlv(aiQuick))
		if aiKeyEnv != "" {
			w("  api_key_env: %s", yamlv(aiKeyEnv))
		}
		w("  # reasoning_effort: none   # OpenAI gpt-5.x: required for chat tools (auto-negotiated otherwise)")
		w("  # grounding_budget: 98304  # prompt-stuffed grounding cap in bytes (default 48KiB)")
	}

	if err := os.WriteFile(configPath, []byte(b.String()), 0o644); err != nil {
		return err
	}
	// round-trip through the real loader so the wizard can never leave a
	// config behind that the server then refuses to start with
	if _, err := config.Load(configPath); err != nil {
		return fmt.Errorf("generated config failed validation (please report this): %w", err)
	}

	fmt.Fprintf(out, "\nwrote %s\n\nnext steps:\n", configPath)
	if tokenEnv != "" {
		fmt.Fprintf(out, "  - export %s=<token with read/write access to %s>\n", tokenEnv, remote)
	}
	if mode == "local" {
		fmt.Fprintf(out, "  - create the first account: specquill -config %s user add <username> <name> <email>\n", configPath)
	}
	if aiOn && aiKeyEnv != "" {
		fmt.Fprintf(out, "  - export %s=<API key>\n", aiKeyEnv)
	}
	fmt.Fprintf(out, "  - start the server: specquill -config %s\n", configPath)
	fmt.Fprintf(out, "  - new workspace repo? scaffold folders + schema with: specquill init %s\n", path.Base(strings.TrimSuffix(remote, ".git")))
	return nil
}
