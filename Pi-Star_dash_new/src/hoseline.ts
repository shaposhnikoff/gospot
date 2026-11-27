// BrandMeister Hoseline player: the board relays one talkgroup over a
// same-origin WebSocket (panel/hoseline.go). Binary frames are G.711 μ-law at
// 8 kHz; text frames are small JSON events.

export const RATE = 8000;

// G.711 μ-law code to a signed 16-bit sample (hoseline_player.py decode_mulaw_sample).
export function mulawSample(code: number): number {
  const value = ~code & 0xff;
  const magnitude = (((value & 0x0f) << 3) + 0x84) << ((value & 0x70) >> 4);
  const sample = magnitude - 0x84;
  return value & 0x80 ? 0 - sample : sample; // 0 - 0 is +0
}

export const MULAW = Float32Array.from({ length: 256 }, (_, code) => mulawSample(code) / 32768);

export function decodeMulaw(bytes: Uint8Array): Float32Array {
  const out = new Float32Array(bytes.length);
  for (let i = 0; i < bytes.length; i++) out[i] = MULAW[bytes[i]];
  return out;
}

// A ring buffer between the WebSocket and the audio clock: it holds the start
// of a call (and any underrun) until `prebuffer` samples are queued, plays a
// short call out when the call ends, and drops the oldest audio on overflow.
// next(step) reads one output sample, step = 8000 / output sample rate, with
// linear interpolation when the output runs faster than 8 kHz.
export class JitterBuffer {
  private readonly data: Float32Array;
  private head = 0;
  private size = 0;
  private pos = 0;
  private waiting = true;
  private draining = false;
  dropped = 0;

  constructor(readonly prebuffer = RATE / 5, capacity = RATE * 4) {
    this.data = new Float32Array(capacity);
  }

  get buffered(): number { return this.size; }
  get playing(): boolean { return !this.waiting; }

  push(samples: ArrayLike<number>): void {
    const cap = this.data.length;
    for (let i = 0; i < samples.length; i++) {
      if (this.size === cap) {
        this.head = (this.head + 1) % cap;
        this.size--;
        this.dropped++;
      }
      this.data[(this.head + this.size) % cap] = samples[i];
      this.size++;
    }
  }

  // End of a call: play what is queued even if it is shorter than the prebuffer.
  flush(): void { this.draining = true; }

  reset(): void {
    this.head = this.size = this.pos = 0;
    this.waiting = true;
    this.draining = false;
  }

  next(step = 1): number {
    if (this.waiting && (this.size >= this.prebuffer || (this.draining && this.size > 0))) this.waiting = false;
    if (this.waiting || this.size === 0) return 0;
    const cap = this.data.length;
    const a = this.data[this.head];
    const b = this.size > 1 ? this.data[(this.head + 1) % cap] : a;
    const value = a + (b - a) * this.pos;
    this.pos += step;
    while (this.pos >= 1 && this.size > 0) {
      this.pos -= 1;
      this.head = (this.head + 1) % cap;
      this.size--;
    }
    if (this.size === 0) {
      this.waiting = true;
      this.draining = false;
      this.pos = 0;
    }
    return value;
  }
}

export type HoselineEvent =
  | { type: 'listening'; tg: number }
  | { type: 'start'; source: number | null }
  | { type: 'end' }
  | { type: 'error'; message: string };

export function parseEvent(text: string): HoselineEvent | null {
  let x: unknown;
  try { x = JSON.parse(text); } catch { return null; }
  if (!x || typeof x !== 'object') return null;
  const o = x as Record<string, unknown>;
  const id = (v: unknown) => typeof v === 'number' && Number.isInteger(v) && v > 0 ? v : null;
  switch (o.type) {
    case 'listening': return id(o.tg) ? { type: 'listening', tg: o.tg as number } : null;
    case 'start': return { type: 'start', source: id(o.source) };
    case 'end': return { type: 'end' };
    case 'error': return { type: 'error', message: typeof o.message === 'string' ? o.message.slice(0, 200) : 'Hoseline error' };
  }
  return null;
}

// Talkgroups and listener use as /api/hoseline/groups reports them.
export type Groups = { groups: number[]; listeners: number; max: number };

export function parseGroups(x: unknown): Groups | null {
  if (!x || typeof x !== 'object') return null;
  const o = x as Record<string, unknown>;
  const int = (v: unknown) => typeof v === 'number' && Number.isInteger(v) && v >= 0;
  if (!Array.isArray(o.groups) || !o.groups.every(int) || !int(o.listeners) || !int(o.max)) return null;
  return { groups: o.groups as number[], listeners: o.listeners as number, max: o.max as number };
}

export const apiBase = (admin: boolean) => (admin ? '/manage' : '') + '/api/hoseline';

export function socketURL(loc: { protocol: string; host: string }, admin: boolean, tg: number): string {
  return (loc.protocol === 'https:' ? 'wss:' : 'ws:') + '//' + loc.host + apiBase(admin) + '?tg=' + tg;
}

// Why a socket closed before it opened: the relay refuses with HTTP 503 when
// all listener slots are taken, which a browser cannot see, so the page asks.
export function refusal(g: Groups | null): string {
  if (g && g.max > 0 && g.listeners >= g.max) return `Too many listeners: all ${g.max} Hoseline slots are in use. Try again later.`;
  return 'Hoseline relay unavailable';
}
