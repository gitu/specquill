package collab

import (
	"context"
	"log"
	"time"

	"specquill/server/internal/store"
)

const (
	persistEvery      = 500 * time.Millisecond
	persistBatchMax   = 64
	compactEverySecs  = 60
	compactUpdateHigh = 500
	contribThrottle   = time.Minute
)

type roomKey struct{ repo, branch, path string }

// mailbox messages
type msgJoin struct{ c *conn }
type msgLeave struct{ c *conn }
type msgFrame struct {
	c    *conn
	data []byte
}
type msgFlushReq struct {
	token uint64
	reply chan error
}
type msgPeers struct{ reply chan []Peer }

// room serializes everything for one (repo, branch, path) through a single
// actor goroutine — no shared-state locking.
type room struct {
	key  roomKey
	hub  *Hub
	mail chan any

	conns   []*conn
	seq     int64
	seedSeq int64
	seeded  bool
	seeder  *conn
	held    []*conn // joined before the seed landed

	pending      [][]byte // updates awaiting SQLite persistence
	pendingFirst int64
	sinceCompact int64
	snapToken    uint64
	snapCovered  int64

	flushedSeq int64
	flushedSha string

	flushWaiters map[uint64]chan error
	tokenSeq     uint64
}

func newRoom(key roomKey, hub *Hub) *room {
	return &room{key: key, hub: hub, mail: make(chan any, 512), flushWaiters: map[uint64]chan error{}}
}

func (r *room) run() {
	persistT := time.NewTicker(persistEvery)
	compactT := time.NewTicker(compactEverySecs * time.Second)
	defer persistT.Stop()
	defer compactT.Stop()
	defer r.hub.dropRoom(r.key)

	// resume or reset persisted state
	r.restore()

	idle := time.NewTimer(time.Minute)
	defer idle.Stop()

	for {
		select {
		case m := <-r.mail:
			switch m := m.(type) {
			case msgJoin:
				r.onJoin(m.c)
			case msgLeave:
				if r.onLeave(m.c) {
					return // last peer gone, state settled
				}
			case msgFrame:
				r.onFrame(m.c, m.data)
			case msgFlushReq:
				r.onFlushRequest(m)
			case msgPeers:
				m.reply <- r.peers()
			}
			if len(r.conns) == 0 && len(r.held) == 0 {
				idle.Reset(10 * time.Second)
			}
		case <-persistT.C:
			r.persistPending()
		case <-compactT.C:
			r.maybeCompact(true)
		case <-idle.C:
			if len(r.conns) == 0 && len(r.held) == 0 {
				r.persistPending()
				return
			}
		}
	}
}

// restore decides between resuming the persisted log and reseeding: the log
// is only valid when the file on disk still matches the last flush.
func (r *room) restore() {
	row, err := r.hub.store.CollabRoom(r.key.repo, r.key.branch, r.key.path)
	if err != nil {
		return // fresh room
	}
	_, sha, ferr := r.hub.fileSha(r.key)
	if ferr == nil && sha == row.FlushedSha && row.FlushedSha != "" {
		r.seq = row.LastSeq
		r.seedSeq = row.SeedSeq
		r.flushedSeq = row.FlushedSeq
		r.flushedSha = row.FlushedSha
		r.snapCovered = 0
		r.seeded = true
		return
	}
	// file changed while the room was dead → file wins
	_ = r.hub.store.DeleteCollabRoom(r.key.repo, r.key.branch, r.key.path)
}

func (r *room) onJoin(c *conn) {
	if !r.seeded && r.seeder == nil {
		// first peer of a fresh room seeds it from its copy of the file
		r.seeder = c
		r.conns = append(r.conns, c)
		c.seedGrant = true
		c.queue(encodeControl(Control{Kind: "hello", Seed: true, Leader: true, Peers: r.peers()}))
		return
	}
	if !r.seeded {
		// hold until the seed lands
		r.held = append(r.held, c)
		c.queue(encodeControl(Control{Kind: "hello", Peers: r.peers()}))
		return
	}
	r.conns = append(r.conns, c)
	c.queue(encodeControl(Control{Kind: "hello", Leader: len(r.conns) == 1, Peers: r.peers()}))
	r.replayTo(c)
	r.broadcastPeers()
}

func (r *room) replayTo(c *conn) {
	r.persistPending()
	updates, err := r.hub.store.CollabUpdates(r.key.repo, r.key.branch, r.key.path)
	if err != nil {
		c.queue(encodeControl(Control{Kind: "error", Code: "replay", Msg: err.Error()}))
		return
	}
	for _, u := range updates {
		c.queue(encodePayload(FrameUpdate, u))
	}
	c.lastSentSeq = r.seq
	c.synced = true
	c.queue(encodeControl(Control{Kind: "synced"}))
}

func (r *room) onFrame(c *conn, data []byte) {
	if len(data) == 0 {
		return
	}
	kind, payload := data[0], data[1:]
	switch kind {
	case FrameControl:
		r.onControl(c, payload)
	case FrameUpdate:
		r.onUpdate(c, payload)
	case FrameAwareness:
		for _, o := range r.conns {
			if o != c && o.synced {
				o.queue(encodePayload(FrameAwareness, payload))
			}
		}
	case FrameSnapshot:
		token, snap, err := decodeToken(payload)
		if err == nil && token == r.snapToken && r.snapToken != 0 {
			r.persistPending()
			covered := r.snapCovered
			if c.lastOwnSeq > covered {
				covered = c.lastOwnSeq
			}
			if err := r.hub.store.CompactCollabLog(r.key.repo, r.key.branch, r.key.path, covered, snap); err != nil {
				log.Printf("collab compact %v: %v", r.key, err)
			}
			r.snapToken = 0
			r.sinceCompact = 0
		}
	case FrameFlush:
		token, content, err := decodeToken(payload)
		if err != nil {
			return
		}
		r.onFlush(c, token, content)
	}
}

func (r *room) onControl(c *conn, payload []byte) {
	var ctl Control
	if err := jsonUnmarshal(payload, &ctl); err != nil {
		return
	}
	if ctl.Kind == "hello-ack" && c.seedGrant && !r.seeded {
		// verify the seed is based on the current file before accepting it
		_, sha, err := r.hub.fileSha(r.key)
		if err == nil && sha != ctl.BaseSha {
			c.queue(encodeControl(Control{Kind: "error", Code: "stale-seed", Msg: "file changed — refetch and reconnect"}))
			c.seedGrant = false
			r.seeder = nil
			return
		}
		r.flushedSha = sha
	}
}

func (r *room) onUpdate(c *conn, payload []byte) {
	buf := make([]byte, len(payload))
	copy(buf, payload)
	r.seq++
	c.lastOwnSeq = r.seq
	if len(r.pending) == 0 {
		r.pendingFirst = r.seq
	}
	r.pending = append(r.pending, buf)
	r.sinceCompact++

	if c.seedGrant && !r.seeded {
		// the first update from the seeder is the document seed
		r.seedSeq = r.seq
		r.seeded = true
		c.seedGrant = false
		c.synced = true
		c.lastSentSeq = r.seq
		c.queue(encodeControl(Control{Kind: "synced"}))
		r.persistPending()
		held := r.held
		r.held = nil
		for _, h := range held {
			r.conns = append(r.conns, h)
			r.replayTo(h)
		}
		r.broadcastPeers()
		return
	}

	// a real edit (not the seed): track contribution + fan out
	if r.seq > r.seedSeq {
		if time.Since(c.lastContrib) > contribThrottle {
			c.lastContrib = time.Now()
			if err := r.hub.store.RecordContributor(r.key.repo, r.key.branch, r.key.path, c.userID); err != nil {
				log.Printf("collab contributor: %v", err)
			}
		}
	}
	for _, o := range r.conns {
		if o != c && o.synced {
			o.queue(encodePayload(FrameUpdate, buf))
			o.lastSentSeq = r.seq
		}
	}
	if len(r.pending) >= persistBatchMax {
		r.persistPending()
	}
	if r.sinceCompact >= compactUpdateHigh {
		r.maybeCompact(false)
	}
}

func (r *room) onFlush(c *conn, token uint64, content []byte) {
	sha, err := r.hub.saveForce(r.key, string(content))
	if err != nil {
		log.Printf("collab flush %v: %v", r.key, err)
		if ch, ok := r.flushWaiters[token]; ok {
			ch <- err
			delete(r.flushWaiters, token)
		}
		c.queue(encodeControl(Control{Kind: "error", Code: "flush", Msg: err.Error()}))
		return
	}
	fs := c.lastSentSeq
	if c.lastOwnSeq > fs {
		fs = c.lastOwnSeq
	}
	if fs > r.flushedSeq {
		r.flushedSeq = fs
	}
	r.flushedSha = sha
	r.saveRow()
	for _, o := range r.conns {
		if o.synced {
			o.queue(encodeControl(Control{Kind: "flushed", Sha: sha}))
		}
	}
	if ch, ok := r.flushWaiters[token]; ok {
		ch <- nil
		delete(r.flushWaiters, token)
	}
}

func (r *room) onFlushRequest(m msgFlushReq) {
	leader := r.leader()
	if leader == nil || r.seq <= r.seedSeq {
		m.reply <- nil // nothing to flush
		return
	}
	r.flushWaiters[m.token] = m.reply
	leader.queue(encodeControl(Control{Kind: "request-flush", Token: m.token}))
}

func (r *room) maybeCompact(timed bool) {
	if r.snapToken != 0 || !r.seeded {
		return
	}
	if !timed && r.sinceCompact < compactUpdateHigh {
		return
	}
	if r.sinceCompact == 0 {
		return
	}
	leader := r.leader()
	if leader == nil || !leader.synced {
		return
	}
	r.tokenSeq++
	r.snapToken = r.tokenSeq
	r.snapCovered = leader.lastSentSeq
	leader.queue(encodeControl(Control{Kind: "request-snapshot", Token: r.snapToken}))
}

func (r *room) onLeave(c *conn) (done bool) {
	wasLeader := r.leader() == c
	r.conns = removeConn(r.conns, c)
	r.held = removeConn(r.held, c)
	if r.seeder == c && !r.seeded {
		// seeder died before seeding: re-grant to the next peer
		r.seeder = nil
		if len(r.held) > 0 {
			next := r.held[0]
			r.held = r.held[1:]
			r.conns = append(r.conns, next)
			r.seeder = next
			next.seedGrant = true
			next.queue(encodeControl(Control{Kind: "hello", Seed: true, Leader: true, Peers: r.peers()}))
		}
	}
	close(c.done)
	if len(r.conns) == 0 {
		r.persistPending()
		if r.seq <= r.seedSeq || r.flushedSeq >= r.seq {
			// untouched or fully flushed — clean end
			_ = r.hub.store.DeleteCollabRoom(r.key.repo, r.key.branch, r.key.path)
		} else {
			r.saveRow() // orphaned: unflushed edits survive in the log
		}
		return len(r.held) == 0
	}
	if wasLeader {
		if l := r.leader(); l != nil {
			l.queue(encodeControl(Control{Kind: "leader"}))
		}
	}
	r.broadcastPeers()
	return false
}

func (r *room) leader() *conn {
	if len(r.conns) == 0 {
		return nil
	}
	return r.conns[0]
}

func (r *room) peers() []Peer {
	out := make([]Peer, 0, len(r.conns))
	for _, c := range r.conns {
		out = append(out, c.peer())
	}
	return out
}

func (r *room) broadcastPeers() {
	msg := encodeControl(Control{Kind: "peers", Peers: r.peers()})
	for _, c := range r.conns {
		c.queue(msg)
	}
}

func (r *room) persistPending() {
	if len(r.pending) == 0 {
		return
	}
	if err := r.hub.store.AppendCollabUpdates(r.key.repo, r.key.branch, r.key.path, r.pendingFirst, r.pending); err != nil {
		log.Printf("collab persist %v: %v", r.key, err)
		return
	}
	r.pending = nil
	r.saveRow()
}

func (r *room) saveRow() {
	_ = r.hub.store.UpsertCollabRoom(&store.CollabRoom{
		Repo: r.key.repo, Branch: r.key.branch, Path: r.key.path,
		LastSeq: r.seq, SeedSeq: r.seedSeq, FlushedSeq: r.flushedSeq, FlushedSha: r.flushedSha,
	})
}

// peersSnapshot reads the member list through the actor mailbox (race-free).
func (r *room) peersSnapshot() []Peer {
	reply := make(chan []Peer, 1)
	select {
	case r.mail <- msgPeers{reply}:
	case <-time.After(time.Second):
		return []Peer{}
	}
	select {
	case p := <-reply:
		return p
	case <-time.After(time.Second):
		return []Peer{}
	}
}

func removeConn(list []*conn, c *conn) []*conn {
	out := list[:0]
	for _, x := range list {
		if x != c {
			out = append(out, x)
		}
	}
	return out
}

// context helper used by hub when pumping reads
func readLoop(ctx context.Context, c *conn, r *room) {
	for {
		_, data, err := c.ws.Read(ctx)
		if err != nil {
			r.mail <- msgLeave{c}
			return
		}
		r.mail <- msgFrame{c, data}
	}
}
