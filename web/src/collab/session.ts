// CollabSession — one live co-editing session per (repo, branch, path).
// Owns the Y.Doc (which OUTLIVES editor remounts), the websocket provider,
// awareness, leader duties (debounced flush, compaction snapshots), and the
// frontmatter Y.Map. Sessions are cached module-level with refcounting and a
// grace teardown so theme/gen remounts rebind instead of rejoining.
import { Doc, applyUpdate, encodeStateAsUpdate } from 'yjs';
import { Awareness, applyAwarenessUpdate, encodeAwarenessUpdate, removeAwarenessStates } from 'y-protocols/awareness';
import {
  ControlMsg, FRAME_AWARENESS, FRAME_FLUSH, FRAME_SNAPSHOT, FRAME_UPDATE,
  decodeFrame, encodeControl, encodePayload, encodeToken, encodeText,
} from './frames';

export type SessionStatus = 'connecting' | 'seeding' | 'synced' | 'offline' | 'error';

export interface SessionPeer { connId: number; userId: number; name: string }

export interface SessionUser { id: number; name: string }

const FLUSH_IDLE_MS = 1500;
const FLUSH_MAX_MS = 10_000;
const TEARDOWN_GRACE_MS = 5000;

const PALETTE = ['#2563c9', '#12876a', '#b06f16', '#6a4fc4', '#c0392b', '#0e7490', '#a21caf', '#4d7c0f'];
export const userColor = (id: number) => PALETTE[Math.abs(id) % PALETTE.length];

type Listener = () => void;

export class CollabSession {
  readonly doc = new Doc();
  readonly awareness = new Awareness(this.doc);
  status: SessionStatus = 'connecting';
  peers: SessionPeer[] = [];
  seedGranted = false;
  isLeader = false;
  savedSha = '';
  dirty = false;
  errorMsg = '';

  private ws: WebSocket | null = null;
  private listeners = new Set<Listener>();
  private serializer: (() => string | null) | null = null;
  /** fired on every flush ack — outlives editor unmounts (final flush lands
   * after the view released the session) */
  onFlushed: ((sha: string) => void) | null = null;
  private flushTimer: ReturnType<typeof setTimeout> | 0 = 0;
  private flushFirstAt = 0;
  private closed = false;
  private reconnectDelay = 500;
  private editedSinceSeed = false;

  constructor(
    readonly key: string,
    private url: string,
    private baseSha: string,
    private initialFm: string,
    readonly me: SessionUser,
  ) {
    this.awareness.setLocalStateField('user', { name: me.name, color: userColor(me.id), id: me.id });
    // diagnostics handle (harmless in prod; used by e2e debugging)
    (window as unknown as { __collab?: unknown }).__collab = this;
    // IMPORTANT: never mutate the doc before the socket is open — a joiner's
    // pre-sync items would exist only locally, and every later edit of theirs
    // would reference clock ranges the peers never received (pending forever).
    // fm is initialized by the seeder only (see the hello handler).
    // transport only — dirtiness is driven by explicit user-edit signals
    // (markUserEdited), so template application never counts as an edit
    this.doc.on('update', (update: Uint8Array, origin: unknown) => {
      if (origin === 'remote') return;
      if (this.ws?.readyState === WebSocket.OPEN) {
        this.send(encodePayload(FRAME_UPDATE, update));
      }
    });
    this.awareness.on('update', ({ added, updated, removed }: { added: number[]; updated: number[]; removed: number[] }) => {
      const changed = added.concat(updated, removed);
      if (this.ws?.readyState === WebSocket.OPEN) {
        this.send(encodePayload(FRAME_AWARENESS, encodeAwarenessUpdate(this.awareness, changed)));
      }
    });
    this.connect();
  }

  meta() { return this.doc.getMap<string>('meta'); }
  getFm(): string { return this.meta().get('fm') || ''; }
  setFm(fm: string) {
    this.doc.transact(() => this.meta().set('fm', fm));
    this.markUserEdited();
  }
  onFmChange(fn: () => void): () => void {
    const h = () => fn();
    this.meta().observe(h);
    return () => this.meta().unobserve(h);
  }

  setSerializer(fn: (() => string | null) | null) { this.serializer = fn; }

  subscribe(fn: Listener): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }
  private emit() { this.listeners.forEach((fn) => fn()); }

  /** called on real user edits (DOM input / properties form) */
  markUserEdited() {
    this.editedSinceSeed = true;
    this.dirty = true;
    this.emit();
    if (this.isLeader) this.scheduleFlush();
  }

  private scheduleFlush() {
    if (this.flushTimer) clearTimeout(this.flushTimer);
    const now = Date.now();
    if (!this.flushFirstAt) this.flushFirstAt = now;
    const cap = Math.max(0, this.flushFirstAt + FLUSH_MAX_MS - now);
    this.flushTimer = setTimeout(() => this.flushNow(0), Math.min(FLUSH_IDLE_MS, cap));
  }

  /**
   * Flush a pre-serialized body — used by the editor's unmount cleanup, when
   * the flush timer may still be pending but the serializer is about to die.
   */
  flushSerialized(body: string) {
    const prev = this.serializer;
    this.serializer = () => body;
    this.flushNow(0);
    this.serializer = prev;
  }

  /** serialize body + fm and send it to the server (token 0 = autonomous). */
  flushNow(token: number) {
    if (this.flushTimer) { clearTimeout(this.flushTimer); this.flushTimer = 0; }
    this.flushFirstAt = 0;
    if (!this.editedSinceSeed && token === 0) return;
    const body = this.serializer?.();
    if (body == null) {
      if (token !== 0) this.send(encodeToken(FRAME_FLUSH, token, encodeText(''))); // barrier no-op
      return;
    }
    const nl = body.endsWith('\n') ? body : body + '\n';
    const fm = this.getFm();
    const content = fm ? `---\n${fm}\n---\n\n${nl}` : nl;
    this.send(encodeToken(FRAME_FLUSH, token, encodeText(content)));
  }

  private connect() {
    if (this.closed) return;
    this.setStatus('connecting');
    const ws = new WebSocket(this.url);
    ws.binaryType = 'arraybuffer';
    this.ws = ws;

    ws.onopen = () => { this.reconnectDelay = 500; };
    ws.onmessage = (ev) => this.onFrame(new Uint8Array(ev.data as ArrayBuffer));
    ws.onclose = () => {
      if (this.closed) return;
      this.setStatus('offline');
      setTimeout(() => this.connect(), this.reconnectDelay);
      this.reconnectDelay = Math.min(this.reconnectDelay * 2, 15_000);
    };
    ws.onerror = () => ws.close();
  }

  private onFrame(data: Uint8Array) {
    const frame = decodeFrame(data);
    if (!frame) return;
    if (frame.type === 'update') {
      applyUpdate(this.doc, frame.payload, 'remote');
      return;
    }
    if (frame.type === 'awareness') {
      applyAwarenessUpdate(this.awareness, frame.payload, 'remote');
      return;
    }
    this.onControl(frame.msg);
  }

  private onControl(c: ControlMsg) {
    switch (c.kind) {
      case 'hello':
        this.peers = c.peers || [];
        this.isLeader = !!c.leader;
        if (c.seed) {
          this.seedGranted = true;
          this.setStatus('seeding');
          this.send(encodeControl({ kind: 'hello-ack', baseSha: this.baseSha }));
          // the seeder initializes fm now — socket is open, so this lands in
          // the seed state every joiner replays
          if (this.initialFm && !this.meta().get('fm')) {
            this.doc.transact(() => this.meta().set('fm', this.initialFm), 'seed');
          }
          // push full state: covers both the fm above and reconnect-with-state
          this.send(encodePayload(FRAME_UPDATE, encodeStateAsUpdate(this.doc)));
        }
        this.emit();
        break;
      case 'synced':
        this.setStatus('synced');
        // announce presence to peers who were already in the room
        this.send(encodePayload(FRAME_AWARENESS, encodeAwarenessUpdate(this.awareness, [this.doc.clientID])));
        break;
      case 'peers':
        this.peers = c.peers || [];
        // re-announce so joiners see our cursor
        this.send(encodePayload(FRAME_AWARENESS, encodeAwarenessUpdate(this.awareness, [this.doc.clientID])));
        this.emit();
        break;
      case 'leader':
        this.isLeader = true;
        if (this.dirty) this.scheduleFlush();
        this.emit();
        break;
      case 'request-flush':
        this.flushNow(c.token || 0);
        break;
      case 'request-snapshot':
        this.send(encodeToken(FRAME_SNAPSHOT, c.token || 0, encodeStateAsUpdate(this.doc)));
        break;
      case 'flushed':
        this.savedSha = c.sha || '';
        this.dirty = false;
        this.emit();
        this.onFlushed?.(this.savedSha);
        break;
      case 'error':
        this.errorMsg = `${c.code}: ${c.msg}`;
        this.setStatus('error');
        break;
    }
  }

  private setStatus(s: SessionStatus) {
    this.status = s;
    this.emit();
  }

  private send(data: Uint8Array) {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(data);
  }

  destroy() {
    this.closed = true;
    if (this.isLeader) this.flushNow(0);
    if (this.flushTimer) clearTimeout(this.flushTimer);
    removeAwarenessStates(this.awareness, [this.doc.clientID], 'destroy');
    this.ws?.close();
    this.awareness.destroy();
    this.doc.destroy();
  }
}

// ---------------------------------------------------------------- cache

const cache = new Map<string, { session: CollabSession; refs: number; teardown?: ReturnType<typeof setTimeout> }>();

export function acquireSession(
  key: string,
  make: () => CollabSession,
): { session: CollabSession; release: () => void } {
  let entry = cache.get(key);
  if (entry) {
    if (entry.teardown) { clearTimeout(entry.teardown); entry.teardown = undefined; }
    entry.refs++;
  } else {
    entry = { session: make(), refs: 1 };
    cache.set(key, entry);
  }
  const e = entry;
  let released = false;
  return {
    session: e.session,
    release: () => {
      if (released) return;
      released = true;
      e.refs--;
      if (e.refs <= 0) {
        // grace period lets remounts (theme switch etc.) rebind cheaply
        e.teardown = setTimeout(() => {
          if (e.refs <= 0) {
            e.session.destroy();
            cache.delete(key);
          }
        }, TEARDOWN_GRACE_MS);
      }
    },
  };
}
