package collab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"reqbase/server/internal/auth"
	"reqbase/server/internal/config"
	"reqbase/server/internal/gitx"
	"reqbase/server/internal/store"
)

// ---- test scaffolding -------------------------------------------------

type env struct {
	hub   *Hub
	st    *store.Store
	repo  *gitx.Repo
	url   string
	close func()
}

func setup(t *testing.T) *env {
	t.Helper()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	run := func(args ...string) {
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main", src)
	if err := os.WriteFile(filepath.Join(src, "doc.md"), []byte("# Doc\n\nseed body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("-C", src, "add", "-A")
	run("-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init")

	cfg := &config.Config{
		DataDir: filepath.Join(tmp, "data"),
		Git:     config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Repos:   []config.RepoConfig{{ID: "w", Mode: config.Writable, Remote: src, DefaultBranch: "main"}},
	}
	git, err := gitx.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := git.Init(); err != nil {
		t.Fatal(err)
	}
	repo, _ := git.Repo("w")
	_ = repo.CreateBranch("ws/a", "main")

	st, err := store.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	// contributor lookups join the users table — seed the fixture identities
	// (ids are deterministic: alice=1, bob=2)
	if _, err := st.UpsertUser("local", "alice", "Alice A", "alice@t"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertUser("local", "bob", "Bob B", "bob@t"); err != nil {
		t.Fatal(err)
	}
	hub := NewHub(st, git)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /collab/{path...}", func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer ws.CloseNow()
		u := auth.UserFrom(r.Context())
		userID, name := int64(1), "anon"
		if u != nil {
			userID, name = u.ID, u.Name
		} else {
			name = r.URL.Query().Get("user")
			if v, err := strconv.ParseInt(r.URL.Query().Get("uid"), 10, 64); err == nil {
				userID = v
			}
		}
		hub.Join(r.Context(), ws, "w", r.URL.Query().Get("branch"), r.PathValue("path"), userID, name)
	})
	srv := httptest.NewServer(mux)
	return &env{hub: hub, st: st, repo: repo, url: "ws" + strings.TrimPrefix(srv.URL, "http"),
		close: func() { srv.Close(); st.Close() }}
}

type peer struct {
	ws  *websocket.Conn
	in  chan []byte
	ctx context.Context
}

func dial(t *testing.T, e *env, branch, path, user string, uid int) *peer {
	t.Helper()
	ctx := context.Background()
	ws, _, err := websocket.Dial(ctx, e.url+"/collab/"+path+"?branch="+branch+"&user="+user+"&uid="+strconv.Itoa(uid), nil)
	if err != nil {
		t.Fatal(err)
	}
	ws.SetReadLimit(4 << 20)
	p := &peer{ws: ws, in: make(chan []byte, 256), ctx: ctx}
	go func() {
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				close(p.in)
				return
			}
			p.in <- data
		}
	}()
	return p
}

func (p *peer) send(t *testing.T, data []byte) {
	t.Helper()
	if err := p.ws.Write(p.ctx, websocket.MessageBinary, data); err != nil {
		t.Fatal(err)
	}
}

// wait for a control message of the given kind, collecting updates seen on the way
func (p *peer) waitControl(t *testing.T, kind string) (Control, [][]byte) {
	t.Helper()
	var updates [][]byte
	deadline := time.After(5 * time.Second)
	for {
		select {
		case data, ok := <-p.in:
			if !ok {
				t.Fatalf("connection closed waiting for %q", kind)
			}
			switch data[0] {
			case FrameUpdate:
				updates = append(updates, data[1:])
			case FrameControl:
				var c Control
				_ = json.Unmarshal(data[1:], &c)
				if c.Kind == kind {
					return c, updates
				}
				if c.Kind == "error" {
					t.Fatalf("error control while waiting for %q: %+v", kind, c)
				}
			}
		case <-deadline:
			t.Fatalf("timeout waiting for %q", kind)
		}
	}
}

func (p *peer) seed(t *testing.T, e *env, branch, path string) {
	t.Helper()
	c, _ := p.waitControl(t, "hello")
	if !c.Seed {
		t.Fatal("first peer should get the seed grant")
	}
	_, sha, err := e.repo.File(branch, path)
	if err != nil {
		t.Fatal(err)
	}
	ack, _ := json.Marshal(Control{Kind: "hello-ack", BaseSha: sha})
	p.send(t, append([]byte{FrameControl}, ack...))
	p.send(t, encodePayload(FrameUpdate, []byte("SEED")))
	p.waitControl(t, "synced")
}

// ---- tests -------------------------------------------------------------

func TestSeedAndReplayToJoiner(t *testing.T) {
	e := setup(t)
	defer e.close()

	a := dial(t, e, "ws/a", "doc.md", "alice", 1)
	a.seed(t, e, "ws/a", "doc.md")
	a.send(t, encodePayload(FrameUpdate, []byte("EDIT-1")))

	b := dial(t, e, "ws/a", "doc.md", "bob", 2)
	b.waitControl(t, "hello")
	_, updates := b.waitControl(t, "synced")
	if len(updates) != 2 || string(updates[0]) != "SEED" || string(updates[1]) != "EDIT-1" {
		t.Fatalf("joiner replay mismatch: %q", updates)
	}

	// live broadcast flows to B (skipping control chatter like `peers`)
	a.send(t, encodePayload(FrameUpdate, []byte("EDIT-2")))
	deadline := time.After(3 * time.Second)
	for {
		select {
		case data, ok := <-b.in:
			if !ok {
				t.Fatal("connection closed")
			}
			if data[0] == FrameUpdate {
				if string(data[1:]) != "EDIT-2" {
					t.Fatalf("expected EDIT-2, got %q", data[1:])
				}
				return
			}
		case <-deadline:
			t.Fatal("broadcast never arrived")
		}
	}
}

func TestStaleSeedRejected(t *testing.T) {
	e := setup(t)
	defer e.close()

	a := dial(t, e, "ws/a", "doc.md", "alice", 1)
	c, _ := a.waitControl(t, "hello")
	if !c.Seed {
		t.Fatal("expected seed grant")
	}
	ack, _ := json.Marshal(Control{Kind: "hello-ack", BaseSha: "deadbeef"})
	a.send(t, append([]byte{FrameControl}, ack...))
	// expect stale-seed error
	deadline := time.After(3 * time.Second)
	for {
		select {
		case data := <-a.in:
			if data[0] == FrameControl {
				var ctl Control
				_ = json.Unmarshal(data[1:], &ctl)
				if ctl.Kind == "error" && ctl.Code == "stale-seed" {
					return
				}
			}
		case <-deadline:
			t.Fatal("no stale-seed error")
		}
	}
}

func TestFlushWritesWorktreeAndRecordsContributors(t *testing.T) {
	e := setup(t)
	defer e.close()

	a := dial(t, e, "ws/a", "doc.md", "alice", 1)
	a.seed(t, e, "ws/a", "doc.md")
	a.send(t, encodePayload(FrameUpdate, []byte("EDIT")))
	a.send(t, encodeToken(FrameFlush, 0, []byte("# Doc\n\nedited by alice\n")))
	c, _ := a.waitControl(t, "flushed")
	if c.Sha == "" {
		t.Fatal("flushed ack missing sha")
	}
	content, _, err := e.repo.File("ws/a", "doc.md")
	if err != nil || !strings.Contains(content, "edited by alice") {
		t.Fatalf("worktree flush missing: %v %q", err, content)
	}
	// contributor recorded (seed excluded, edit counted)
	users, err := e.st.Contributors("w", "ws/a", nil)
	if err != nil || len(users) != 1 {
		t.Fatalf("contributors: %v %v", err, users)
	}
}

func TestCommitBarrierFlushesLeader(t *testing.T) {
	e := setup(t)
	defer e.close()

	a := dial(t, e, "ws/a", "doc.md", "alice", 1)
	a.seed(t, e, "ws/a", "doc.md")
	a.send(t, encodePayload(FrameUpdate, []byte("EDIT")))
	time.Sleep(300 * time.Millisecond) // let the edit register before the barrier

	// leader answers request-flush (plain reader — no t.Fatal in goroutines)
	go func() {
		deadline := time.After(5 * time.Second)
		for {
			select {
			case data, ok := <-a.in:
				if !ok {
					return
				}
				if len(data) > 1 && data[0] == FrameControl {
					var c Control
					_ = json.Unmarshal(data[1:], &c)
					if c.Kind == "request-flush" {
						_ = a.ws.Write(a.ctx, websocket.MessageBinary, encodeToken(FrameFlush, c.Token, []byte("# Doc\n\nbarrier content\n")))
						return
					}
				}
			case <-deadline:
				return
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.hub.FlushBranch(ctx, "w", "ws/a", nil); err != nil {
		t.Fatal(err)
	}
	content, _, _ := e.repo.File("ws/a", "doc.md")
	if !strings.Contains(content, "barrier content") {
		t.Fatalf("barrier flush missing: %q", content)
	}
}

func TestRestartReplayAfterCleanFlush(t *testing.T) {
	e := setup(t)
	defer e.close()

	a := dial(t, e, "ws/a", "doc.md", "alice", 1)
	a.seed(t, e, "ws/a", "doc.md")
	a.send(t, encodePayload(FrameUpdate, []byte("EDIT-A")))
	a.send(t, encodeToken(FrameFlush, 0, []byte("flushed content\n")))
	a.waitControl(t, "flushed")
	// leave with everything flushed → clean end → log deleted
	a.ws.Close(websocket.StatusNormalClosure, "bye")
	time.Sleep(300 * time.Millisecond)
	if _, err := e.st.CollabRoom("w", "ws/a", "doc.md"); err == nil {
		t.Fatal("clean room end should delete the persisted row")
	}

	// next join reseeds from the (flushed) file
	b := dial(t, e, "ws/a", "doc.md", "bob", 2)
	c, _ := b.waitControl(t, "hello")
	if !c.Seed {
		t.Fatal("fresh room after clean end should grant seed")
	}
}

func TestOrphanedRoomSurvivesAndReplays(t *testing.T) {
	e := setup(t)
	defer e.close()

	a := dial(t, e, "ws/a", "doc.md", "alice", 1)
	a.seed(t, e, "ws/a", "doc.md")
	a.send(t, encodePayload(FrameUpdate, []byte("UNFLUSHED")))
	time.Sleep(700 * time.Millisecond) // persist tick
	a.ws.Close(websocket.StatusNormalClosure, "crash-ish")
	time.Sleep(300 * time.Millisecond)

	rooms, err := e.st.OrphanedCollabRooms("w")
	if err != nil || len(rooms) != 1 {
		t.Fatalf("expected orphaned room, got %v %v", rooms, err)
	}

	// rejoin: file unchanged since last flush-state → log replays
	b := dial(t, e, "ws/a", "doc.md", "bob", 2)
	c, _ := b.waitControl(t, "hello")
	if c.Seed {
		t.Fatal("orphaned room with valid log must not reseed")
	}
	_, updates := b.waitControl(t, "synced")
	found := false
	for _, u := range updates {
		if string(u) == "UNFLUSHED" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unflushed edit lost across orphaning: %q", updates)
	}
}

func TestCompactionNeverDropsUncovered(t *testing.T) {
	e := setup(t)
	defer e.close()

	a := dial(t, e, "ws/a", "doc.md", "alice", 1)
	a.seed(t, e, "ws/a", "doc.md")
	// exceed the compaction high-water mark
	for i := 0; i < compactUpdateHigh+5; i++ {
		a.send(t, encodePayload(FrameUpdate, []byte("E")))
	}
	c, _ := a.waitControl(t, "request-snapshot")
	// concurrent update from a second peer AFTER the snapshot request
	b := dial(t, e, "ws/a", "doc.md", "bob", 2)
	b.waitControl(t, "hello")
	b.waitControl(t, "synced")
	b.send(t, encodePayload(FrameUpdate, []byte("LATE")))
	time.Sleep(200 * time.Millisecond)
	a.send(t, encodeToken(FrameSnapshot, c.Token, []byte("SNAPSHOT")))
	time.Sleep(700 * time.Millisecond)

	updates, err := e.st.CollabUpdates("w", "ws/a", "doc.md")
	if err != nil {
		t.Fatal(err)
	}
	foundSnap, foundLate := false, false
	for _, u := range updates {
		if string(u) == "SNAPSHOT" {
			foundSnap = true
		}
		if string(u) == "LATE" {
			foundLate = true
		}
	}
	if !foundSnap || !foundLate {
		t.Fatalf("compaction lost data: snapshot=%v late=%v n=%d", foundSnap, foundLate, len(updates))
	}
}
