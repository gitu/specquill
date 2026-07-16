// Package collab is the real-time co-editing relay: rooms keyed by
// (repo, branch, path) broadcast opaque Yjs updates between peers and persist
// them for durability. The server never interprets CRDT payloads.
package collab

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
)

// Frame types — first byte of every websocket binary message.
const (
	FrameUpdate    byte = 0x01 // [type][yjs update]                 persist + broadcast
	FrameAwareness byte = 0x02 // [type][awareness update]           broadcast only
	FrameControl   byte = 0x03 // [type][json]                       control channel
	FrameSnapshot  byte = 0x04 // [type][8B token][merged update]    leader → server (compaction)
	FrameFlush     byte = 0x05 // [type][8B token][full file bytes]  leader → server (persist to worktree)
)

// Control message kinds (JSON payload of FrameControl).
type Control struct {
	Kind    string `json:"kind"` // hello|hello-ack|synced|peers|leader|request-snapshot|request-flush|flushed|error
	Seed    bool   `json:"seed,omitempty"`
	Leader  bool   `json:"leader,omitempty"`
	Peers   []Peer `json:"peers,omitempty"`
	Token   uint64 `json:"token,omitempty"`
	Sha     string `json:"sha,omitempty"`
	BaseSha string `json:"baseSha,omitempty"` // hello-ack (client → server, with the seed)
	Code    string `json:"code,omitempty"`
	Msg     string `json:"msg,omitempty"`
}

type Peer struct {
	ConnID int64  `json:"connId"`
	UserID int64  `json:"userId"`
	Name   string `json:"name"`
}

// maxControlPayload bounds encoded control frames. Real payloads are tiny
// (peer lists, shas); the cap guards the allocation below.
const maxControlPayload = 1 << 20

func encodeControl(c Control) []byte {
	raw, _ := json.Marshal(c)
	if len(raw) > maxControlPayload {
		raw, _ = json.Marshal(Control{Kind: "error", Code: "oversize", Msg: "control payload too large"})
	}
	out := make([]byte, 1+len(raw))
	out[0] = FrameControl
	copy(out[1:], raw)
	return out
}

func encodePayload(t byte, payload []byte) []byte {
	out := make([]byte, 1+len(payload))
	out[0] = t
	copy(out[1:], payload)
	return out
}

func encodeToken(t byte, token uint64, payload []byte) []byte {
	out := make([]byte, 9+len(payload))
	out[0] = t
	binary.BigEndian.PutUint64(out[1:9], token)
	copy(out[9:], payload)
	return out
}

func decodeToken(data []byte) (token uint64, payload []byte, err error) {
	if len(data) < 8 {
		return 0, nil, fmt.Errorf("short token frame")
	}
	return binary.BigEndian.Uint64(data[:8]), data[8:], nil
}
