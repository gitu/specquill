// Binary frame codec — mirrors server/internal/collab/frame.go.

export const FRAME_UPDATE = 0x01;
export const FRAME_AWARENESS = 0x02;
export const FRAME_CONTROL = 0x03;
export const FRAME_SNAPSHOT = 0x04;
export const FRAME_FLUSH = 0x05;

export interface ControlMsg {
  kind: 'hello' | 'hello-ack' | 'synced' | 'peers' | 'leader' | 'request-snapshot' | 'request-flush' | 'flushed' | 'error';
  seed?: boolean;
  leader?: boolean;
  peers?: { connId: number; userId: number; name: string }[];
  token?: number;
  sha?: string;
  baseSha?: string;
  code?: string;
  msg?: string;
}

const te = new TextEncoder();
const td = new TextDecoder();

export function encodePayload(type: number, payload: Uint8Array): Uint8Array {
  const out = new Uint8Array(1 + payload.length);
  out[0] = type;
  out.set(payload, 1);
  return out;
}

export function encodeControl(c: ControlMsg): Uint8Array {
  return encodePayload(FRAME_CONTROL, te.encode(JSON.stringify(c)));
}

export function encodeToken(type: number, token: number, payload: Uint8Array): Uint8Array {
  const out = new Uint8Array(9 + payload.length);
  out[0] = type;
  const view = new DataView(out.buffer);
  view.setUint32(1, Math.floor(token / 2 ** 32));
  view.setUint32(5, token >>> 0);
  out.set(payload, 9);
  return out;
}

export type Frame =
  | { type: 'update'; payload: Uint8Array }
  | { type: 'awareness'; payload: Uint8Array }
  | { type: 'control'; msg: ControlMsg };

export function decodeFrame(data: Uint8Array): Frame | null {
  if (data.length === 0) return null;
  const payload = data.subarray(1);
  switch (data[0]) {
    case FRAME_UPDATE: return { type: 'update', payload };
    case FRAME_AWARENESS: return { type: 'awareness', payload };
    case FRAME_CONTROL:
      try { return { type: 'control', msg: JSON.parse(td.decode(payload)) as ControlMsg }; } catch { return null; }
    default: return null;
  }
}

export function encodeText(s: string): Uint8Array {
  return te.encode(s);
}
