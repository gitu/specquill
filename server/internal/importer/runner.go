package importer

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"specquill/server/internal/config"
	"specquill/server/internal/gitx"
	"specquill/server/internal/store"
)

// Runner materializes non-git sources into their gitx mirror repos on a
// schedule and on demand, recording each import's outcome in the store. The
// importer definition (endpoint, urls, space, token_env) lives in app config —
// like credentials, it is infrastructure, not tenant data.
type Runner struct {
	git   *gitx.Manager
	store *store.Store
	hc    *http.Client

	mu    sync.Mutex
	specs map[string]regSpec // "tenant/name" → registration
}

type regSpec struct {
	tenant string
	tid    int64
	cfg    config.SourceConfig
}

func NewRunner(git *gitx.Manager, st *store.Store) *Runner {
	return &Runner{
		git:   git,
		store: st,
		hc:    &http.Client{Timeout: 60 * time.Second},
		specs: map[string]regSpec{},
	}
}

// Register enrolls a non-git source for scheduled + on-demand import. tid is the
// tenant's DB id (for status rows). Git sources are ignored.
func (rn *Runner) Register(tenant string, tid int64, sc config.SourceConfig) {
	if sc.IsGit() {
		return
	}
	rn.mu.Lock()
	rn.specs[tenant+"/"+sc.Name] = regSpec{tenant: tenant, tid: tid, cfg: sc}
	rn.mu.Unlock()
}

// Manages reports whether the runner drives this source (non-git, registered).
func (rn *Runner) Manages(tenant, name string) bool {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	_, ok := rn.specs[tenant+"/"+name]
	return ok
}

// Sync imports one source now: fetch → snapshot the mirror repo → record the
// result. It records an error row on failure and returns the same error.
func (rn *Runner) Sync(ctx context.Context, tenant, name string) (store.SourceSync, error) {
	rn.mu.Lock()
	sp, ok := rn.specs[tenant+"/"+name]
	rn.mu.Unlock()
	if !ok {
		return store.SourceSync{}, fmt.Errorf("no importer source %s/%s", tenant, name)
	}
	rec := store.SourceSync{TenantID: sp.tid, Name: name, Status: "ok"}
	fail := func(err error) (store.SourceSync, error) {
		rec.Status, rec.Error = "error", err.Error()
		_ = rn.store.RecordSourceSync(rec)
		return rec, err
	}

	repo, ok := rn.git.Repo(tenant + "/" + name)
	if !ok {
		return fail(fmt.Errorf("mirror repo not initialized"))
	}
	imp, err := For(sp.cfg.Kind)
	if err != nil {
		return fail(err)
	}
	token := ""
	if sp.cfg.TokenEnv != "" {
		token = os.Getenv(sp.cfg.TokenEnv)
	}
	files, err := imp(ctx, rn.hc, Source{
		Name: name, Kind: sp.cfg.Kind, Remote: sp.cfg.Remote,
		Token: token, URLs: sp.cfg.URLs, Space: sp.cfg.Space,
	})
	if err != nil {
		return fail(err)
	}
	sha, changed, err := repo.SnapshotMirror(fmt.Sprintf("import %s: %d file(s)", name, len(files)), files)
	if err != nil {
		return fail(err)
	}
	rec.FileCount, rec.HeadSHA = len(files), sha
	if err := rn.store.RecordSourceSync(rec); err != nil {
		return rec, err
	}
	if changed && rn.git.Notify != nil {
		rn.git.Notify("fetch", tenant+"/"+name, "")
	}
	return rec, nil
}

// Start launches a per-source import loop (initial sync immediately, then on the
// configured interval) for every registered source. Non-blocking.
func (rn *Runner) Start(ctx context.Context) {
	rn.mu.Lock()
	specs := make([]regSpec, 0, len(rn.specs))
	for _, sp := range rn.specs {
		specs = append(specs, sp)
	}
	rn.mu.Unlock()
	for _, sp := range specs {
		go rn.loop(ctx, sp)
	}
}

func (rn *Runner) loop(ctx context.Context, sp regSpec) {
	interval := sp.cfg.SyncInterval
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	run := func() {
		if _, err := rn.Sync(ctx, sp.tenant, sp.cfg.Name); err != nil {
			log.Printf("import %s/%s: %v", sp.tenant, sp.cfg.Name, err)
		}
	}
	run() // initial import at startup
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}
