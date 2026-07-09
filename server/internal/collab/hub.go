package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"specquill/server/internal/gitx"
	"specquill/server/internal/store"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// Hub owns the room registry and is the only public surface of the package.
type Hub struct {
	store *store.Store
	git   *gitx.Manager

	mu     sync.Mutex
	rooms  map[roomKey]*room
	connID atomic.Int64
	token  atomic.Uint64
}

func NewHub(st *store.Store, git *gitx.Manager) *Hub {
	return &Hub{store: st, git: git, rooms: map[roomKey]*room{}}
}

func (h *Hub) room(key roomKey) *room {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.rooms[key]
	if !ok {
		r = newRoom(key, h)
		h.rooms[key] = r
		go r.run()
	}
	return r
}

func (h *Hub) dropRoom(key roomKey) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rooms, key)
}

// fileSha reads the current file content + blob sha from the branch worktree.
func (h *Hub) fileSha(key roomKey) (content, sha string, err error) {
	repo, ok := h.git.Repo(key.repo)
	if !ok {
		return "", "", fmt.Errorf("unknown repo")
	}
	return repo.File(key.branch, key.path)
}

// saveForce writes room-owned content into the worktree (no baseSha check).
func (h *Hub) saveForce(key roomKey, content string) (string, error) {
	repo, ok := h.git.Repo(key.repo)
	if !ok {
		return "", fmt.Errorf("unknown repo")
	}
	return repo.SaveFileForce(key.branch, key.path, content)
}

// Join hands an accepted websocket to the room for (repo, branch, path).
// Blocks until the peer disconnects.
func (h *Hub) Join(ctx context.Context, ws *websocket.Conn, repo, branch, path string, userID int64, name string) {
	key := roomKey{repo, branch, path}
	r := h.room(key)
	c := newConn(h.connID.Add(1), userID, name, ws)
	go c.writePump(ctx)
	// heartbeat: a silently-dropped peer (killed browser, dead NAT path)
	// otherwise blocks Read forever and keeps the room live as a zombie
	go func() {
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.done:
				return
			case <-t.C:
				pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := ws.Ping(pctx)
				cancel()
				if err != nil {
					_ = ws.Close(websocket.StatusGoingAway, "heartbeat timeout")
					return
				}
			}
		}
	}()
	r.mail <- msgJoin{c}
	readLoop(ctx, c, r)
}

// RoomActive reports whether a live room exists for the file.
func (h *Hub) RoomActive(repo, branch, path string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.rooms[roomKey{repo, branch, path}]
	return ok
}

// ActiveOnBranch lists paths with live rooms on a branch.
func (h *Hub) ActiveOnBranch(repo, branch string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for k := range h.rooms {
		if k.repo == repo && k.branch == branch {
			out = append(out, k.path)
		}
	}
	return out
}

// FlushBranch asks the leader of every live room on the branch (optionally
// restricted to paths) to flush, waiting up to 3s each — the commit barrier.
func (h *Hub) FlushBranch(ctx context.Context, repo, branch string, paths []string) error {
	want := map[string]bool{}
	for _, p := range paths {
		want[p] = true
	}
	h.mu.Lock()
	var targets []*room
	for k, r := range h.rooms {
		if k.repo == repo && k.branch == branch && (len(want) == 0 || want[k.path]) {
			targets = append(targets, r)
		}
	}
	h.mu.Unlock()

	for _, r := range targets {
		reply := make(chan error, 1)
		token := h.token.Add(1)
		select {
		case r.mail <- msgFlushReq{token: token, reply: reply}:
		case <-time.After(time.Second):
			continue
		}
		select {
		case <-reply:
		case <-time.After(3 * time.Second):
			// leader unresponsive — commit proceeds with the last flushed state
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// RoomPresence is the coarse who-is-where view for the UI.
type RoomPresence struct {
	Branch   string `json:"branch"`
	Path     string `json:"path"`
	Users    []Peer `json:"users"`
	Orphaned bool   `json:"orphaned"`
}

func (h *Hub) Presence(repo string) []RoomPresence {
	type probe struct {
		key roomKey
		r   *room
	}
	h.mu.Lock()
	probes := make([]probe, 0, len(h.rooms))
	for k, r := range h.rooms {
		if k.repo == repo {
			probes = append(probes, probe{k, r})
		}
	}
	h.mu.Unlock()

	out := []RoomPresence{}
	seen := map[string]bool{}
	for _, p := range probes {
		// snapshot peers via the mailbox-free read: peers() touches actor
		// state, so route through a tiny request instead
		users := p.r.peersSnapshot()
		out = append(out, RoomPresence{Branch: p.key.branch, Path: p.key.path, Users: users})
		seen[p.key.branch+"\x00"+p.key.path] = true
	}
	// orphaned rooms (persisted unflushed logs without live peers)
	if rows, err := h.store.OrphanedCollabRooms(repo); err == nil {
		for _, row := range rows {
			if !seen[row.Branch+"\x00"+row.Path] {
				out = append(out, RoomPresence{Branch: row.Branch, Path: row.Path, Users: []Peer{}, Orphaned: true})
			}
		}
	}
	return out
}
