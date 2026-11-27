// View model of the /kiosk wall screen: one /api/dashboard poll per second,
// turned into "on air", "idle" or "connection lost". Pure functions of the
// connection state and the client clock, so the screen can be tested
// without a browser.

import { type Dashboard, type Row, modeLabel, duration, loss, ber, rssi, lossStyle, berStyle, green, amber, red, dataNote } from './classic';

export const POLL_MS = 1000;
export const REQUEST_TIMEOUT_MS = 4000;
// Failed polls for this long (since the last good reply or since polling
// resumed) show "connection lost".
export const LOST_AFTER_MS = 5000;
export const RECENT_CALLS = 10;

export type Conn = {
  data: Dashboard | null;
  okAt: number | null; // client time of the last good reply
  offset: number; // server Now minus client time at that reply
  failures: number; // consecutive failed polls
  since: number; // client time polling started or resumed
  version: string | null; // dashboard build first seen
  reload: boolean; // a different build answered: reload the page
};

export const initialConn = (now: number): Conn =>
  ({ data: null, okAt: null, offset: 0, failures: 0, since: now, version: null, reload: false });

type Obj = Record<string, unknown>;
const isObj = (x: unknown): x is Obj => !!x && typeof x === 'object' && !Array.isArray(x);
const numOrNull = (x: unknown) => x == null || (typeof x === 'number' && Number.isFinite(x));

function isRow(x: unknown): x is Row {
  return isObj(x) && ['time', 'clock', 'mode', 'callsign', 'target', 'source'].every(k => typeof x[k] === 'string')
    && ['duration', 'loss', 'ber', 'rssi'].every(k => numOrNull(x[k])) && typeof x.live === 'boolean'
    && ['name', 'targetName', 'suffix', 'note'].every(k => x[k] == null || typeof x[k] === 'string');
}

// asDashboard accepts the fields the kiosk reads; anything else is a failed poll.
export function asDashboard(x: unknown): Dashboard | null {
  if (!isObj(x) || typeof x.Now !== 'string' || Number.isNaN(Date.parse(x.Now)) || typeof x.Callsign !== 'string') return null;
  if (!isObj(x.TRX) || typeof x.TRX.kind !== 'string') return null;
  for (const k of ['Heard', 'Local']) {
    if (x[k] != null && (!Array.isArray(x[k]) || !(x[k] as unknown[]).every(isRow))) return null;
  }
  return x as unknown as Dashboard;
}

export function accept(c: Conn, raw: unknown, now: number): Conn {
  const data = asDashboard(raw);
  if (!data) return fail(c, now);
  const version = typeof data.Version === 'string' ? data.Version : '';
  return {
    ...c, data, okAt: now, offset: Date.parse(data.Now) - now, failures: 0,
    version: c.version ?? version, reload: c.reload || (c.version != null && version !== c.version),
  };
}

export const fail = (c: Conn, _now: number): Conn => ({ ...c, failures: c.failures + 1 });

// resume restarts the "lost" timer after the tab was hidden (no polls ran).
export const resume = (c: Conn, now: number): Conn => ({ ...c, failures: 0, since: now });

export function isLost(c: Conn, now: number): boolean {
  if (c.failures === 0) return false;
  if (!c.data) return true;
  return now - Math.max(c.okAt ?? 0, c.since) > LOST_AFTER_MS;
}

export type Level = '' | 'good' | 'warn' | 'bad';

export type Call = {
  callsign: string; name: string; target: string; targetName: string;
  mode: string; slot: string; source: 'RF' | 'Net'; sourceLabel: string;
};

export type Recent = Call & {
  clock: string; modeLabel: string; duration: string;
  loss: string; ber: string; quality: string; level: Level; signal: string; data: boolean;
};

export type OnAir = Call & { seconds: number; elapsed: string; kind: 'rx' | 'tx' };

export type Station = { callsign: string; freq: string };

export type View =
  | { state: 'connecting' }
  | { state: 'lost'; lostFor: string; station: Station | null; recent: Recent[] }
  | { state: 'onair'; call: OnAir; more: number; station: Station; recent: Recent[] }
  | { state: 'idle'; ago: string | null; last: Recent | null; offline: boolean; history: boolean; station: Station; recent: Recent[] };

// "DMR Slot 2" -> DMR, TS2; other modes have no slot.
function splitMode(mode: string): { mode: string; slot: string } {
  const m = /^DMR Slot ([12])$/.exec(mode);
  return m ? { mode: 'DMR', slot: 'TS' + m[1] } : { mode, slot: '' };
}

function call(r: Row): Call {
  const { mode, slot } = splitMode(r.mode);
  const source = r.source === 'RF' ? 'RF' : 'Net';
  return {
    callsign: r.callsign.trim() + (r.suffix?.trim() ? '/' + r.suffix.trim() : ''),
    name: r.name ?? '', target: r.target.replace(/\s+/g, ' ').trim(), targetName: r.targetName ?? '',
    mode, slot, source, sourceLabel: source === 'RF' ? 'RF' : 'Network',
  };
}

const styleLevel = (s: string): Level => s === green ? 'good' : s === amber ? 'warn' : s === red ? 'bad' : '';
const worst = (a: Level, b: Level): Level => {
  const order: Level[] = ['', 'good', 'warn', 'bad'];
  return order[Math.max(order.indexOf(a), order.indexOf(b))];
};

export function recent(r: Row): Recent {
  const isData = dataNote(r);
  const d = duration(r);
  const net = r.source !== 'RF';
  return {
    ...call(r),
    clock: /^\d\d:\d\d:\d\d/.test(r.clock) ? r.clock.slice(0, 8) : '',
    modeLabel: modeLabel(r.mode),
    duration: isData ? 'Data' : d === '' || d === 'TOut' ? d : d + ' s',
    loss: net && !isData ? loss(r) : '',
    ber: isData ? '' : ber(r),
    // The list's Loss/BER cell: loss and BER from the network, BER from RF.
    quality: isData ? '—' : [net ? loss(r) : '', ber(r)].filter(Boolean).join(' · '),
    level: isData ? '' : worst(net ? styleLevel(lossStyle(r)) : '', styleLevel(berStyle(r))),
    signal: net ? '' : rssi(r),
    data: isData,
  };
}

// clock formats seconds as m:ss, or h:mm:ss from an hour.
export function clock(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), r = s % 60;
  const two = (n: number) => String(n).padStart(2, '0');
  return h > 0 ? `${h}:${two(m)}:${two(r)}` : `${m}:${two(r)}`;
}

// span formats a past interval for a wall screen: "42 s", "7 min", "2 h 5 min".
export function span(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  if (s < 60) return `${s} s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60), rest = m % 60;
  return rest ? `${h} h ${rest} min` : `${h} h`;
}

export const ago = (seconds: number) => seconds < 5 ? 'just now' : span(seconds) + ' ago';

const isCall = (r: Row) => r.mode !== 'POCSAG';

export function kioskView(c: Conn, now: number): View {
  const d = c.data;
  const rows = (d?.Heard ?? []).filter(isCall);
  const done = rows.filter(r => !r.live);
  const list = done.slice(0, RECENT_CALLS).map(recent);
  const station = d ? { callsign: d.Callsign, freq: d.TX && d.RX && d.TX !== d.RX ? `TX ${d.TX} · RX ${d.RX}` : d.TX } : null;
  if (isLost(c, now)) {
    const since = c.okAt ?? c.since;
    return { state: 'lost', lostFor: span((now - since) / 1000), station, recent: list };
  }
  if (!d || !station) return { state: 'connecting' };
  const serverNow = now + c.offset;
  const live = rows.filter(r => r.live);
  if (live.length > 0) {
    const r = live[0];
    const seconds = Math.max(0, (serverNow - Date.parse(r.time)) / 1000);
    const base = call(r);
    return {
      state: 'onair', station, recent: list, more: live.length - 1,
      call: { ...base, seconds, elapsed: clock(seconds), kind: base.source === 'RF' ? 'rx' : 'tx' },
    };
  }
  const last = done[0];
  let since: string | null = null;
  if (last) {
    const end = Date.parse(last.time) + (last.duration ?? 0) * 1000;
    since = ago((serverNow - end) / 1000);
  }
  return {
    state: 'idle', station, recent: list, ago: since, last: last ? recent(last) : null,
    offline: d.TRX.kind === 'offline', history: d.HeardReady !== false,
  };
}
