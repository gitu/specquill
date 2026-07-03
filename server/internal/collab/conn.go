package collab

import (
	"context"
	"time"

	"github.com/coder/websocket"
)

// conn wraps one websocket peer. Reads are pumped into the room's mailbox;
// writes go through a buffered queue so a slow peer never blocks the actor.
type conn struct {
	id     int64
	userID int64
	name   string
	ws     *websocket.Conn
	send   chan []byte
	done   chan struct{}

	// actor-owned bookkeeping (only the room goroutine touches these)
	lastSentSeq int64 // highest update seq queued to this peer
	lastOwnSeq  int64 // highest seq assigned to updates from this peer
	synced      bool  // replay finished; broadcasts may flow
	seedGrant   bool
	lastContrib time.Time
}

func newConn(id int64, userID int64, name string, ws *websocket.Conn) *conn {
	// generous queue: joining a long-lived room replays the whole update log
	// in one burst; overflow (dead/steady-slow peers) still disconnects
	return &conn{id: id, userID: userID, name: name, ws: ws, send: make(chan []byte, 8192), done: make(chan struct{})}
}

// writePump drains the send queue; closing `done` terminates it.
func (c *conn) writePump(ctx context.Context) {
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.ws.Write(wctx, websocket.MessageBinary, msg)
			cancel()
			if err != nil {
				return
			}
		case <-c.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

// queue enqueues without ever blocking the actor; overflow kills the peer
// (it will reconnect and resync from the log).
func (c *conn) queue(msg []byte) {
	select {
	case c.send <- msg:
	default:
		select {
		case <-c.done:
		default:
			close(c.done)
		}
	}
}

func (c *conn) peer() Peer { return Peer{ConnID: c.id, UserID: c.userID, Name: c.name} }
