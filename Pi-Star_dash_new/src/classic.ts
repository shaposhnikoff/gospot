// Formatting rules ported from Pi-Star_DV_Dash mmdvmhost/lh.php, localtx.php
// and functions.php (revision 10bf34c9).

export type Cell = { label: string; state: 'on' | 'fail' | 'warn' | 'off' };
export type Row = {
  time: string; clock: string; mode: string; callsign: string; target: string; source: string;
  duration: number | null; loss: number | null; ber: number | null; rssi: number | null; live: boolean;
  suffix?: string; note?: string;
};
export type Dashboard = {
  Hostname: string; Callsign: string; Version: string; PiStar: string; Controller: string;
  Modes: Cell[] | null; Networks: Cell[] | null;
  TRX: { kind: string; mode: string; label: string };
  TX: string; RX: string; Firmware: string; TCXO: string;
  DStar: { RPT1: string; RPT2: string; APRS: string; IRC: string } | null;
  DMR: { ID: string; CC: string; TS1: boolean; TS2: boolean; Network: boolean; Masters: string[] | null } | null;
  YSF: { Code: string; Link: string } | null;
  P25: { Code: string; Link: string } | null;
  NXDN: { Code: string; Link: string } | null;
  M17: { Code: string; Link: string } | null;
  POCSAG: { TX: string; Master: string } | null;
  Heard: Row[] | null; Local: Row[] | null; HeardReady: boolean; TimeZone: string; Now: string;
};

export const red = 'background:#f33;';
export const green = 'background:#1d1;';
export const amber = 'background:#fa0;';

export function cellStyle(state: Cell['state']): string {
  switch (state) {
    case 'on': return 'background:#0b0; color:#030; width:50%;';
    case 'fail': return 'background:#b00; color:#500; width:50%;';
    case 'warn': return 'background:#ff9; color:#030; width:50%;';
    default: return 'background:#606060; color:#b0b0b0;';
  }
}

const hangColour: Record<string, string> = { 'D-Star': '#ade', DMR: '#f93', YSF: '#ff9', P25: '#f9f', NXDN: '#c9f', M17: '#f9f', FM: '#f9f' };

export function trxStyle(kind: string, mode: string): string {
  switch (kind) {
    case 'tx': return red;
    case 'rx': case 'pocsag': return 'background:#4aa361;';
    case 'hang': return `background:${hangColour[mode] ?? '#0b0'};`;
    case 'listening': return 'background:#0b0; color:#030;';
    case 'offline': return 'background:#606060; color:#b0b0b0;';
    default: return '';
  }
}

export const modeLabel = (mode: string) => mode.replace('Slot ', 'TS');

export function target(raw: string): string {
  const padded = raw.length === 1 ? raw.padStart(8, ' ') : raw;
  return raw.startsWith('CQCQCQ') ? padded : padded.replaceAll(' ', ' ');
}

export type CallsignView =
  | { kind: 'text'; text: string }
  | { kind: 'id'; text: string; href: string }
  | { kind: 'call'; text: string; href: string; gps: string };

export function callsign(raw: string): CallsignView {
  if (/^[0-9]+(\.[0-9]+)?$/.test(raw)) {
    return Number(raw) > 9999
      ? { kind: 'id', text: raw, href: 'https://database.radioid.net/database/view?id=' + encodeURIComponent(raw) }
      : { kind: 'text', text: raw };
  }
  if (!/[A-Za-z].*[0-9]|[0-9].*[A-Za-z]/.test(raw)) return { kind: 'text', text: raw };
  const dash = raw.indexOf('-');
  const trimmed = dash > 0 ? raw.slice(0, dash) : raw;
  const url = encodeURIComponent(trimmed);
  return { kind: 'call', text: trimmed, href: 'https://database.radioid.net/database/view?callsign=' + url, gps: 'https://aprs.fi/#!call=' + url + '*' };
}

export function liveSeconds(row: Row, now: string): string {
  const seconds = (Date.parse(now) - Date.parse(row.time)) / 1000;
  return seconds < 999 ? `${Math.max(0, Math.round(seconds))}+` : '∞';
}

export const duration = (row: Row) => row.note === 'TOut' && row.duration == null ? 'TOut' : row.duration == null ? '' : row.duration.toFixed(1);
export const loss = (row: Row) => row.loss == null ? (row.source === 'RF' ? '0%' : '') : `${row.loss}%`;
export const ber = (row: Row) => row.ber == null ? (row.note === 'TOut' ? '??%' : '') : `${row.ber.toFixed(1)}%`;
// lh.php shows data transfers as one green cell instead of the metrics.
export const dataNote = (row: Row) => row.note === 'DMR Data' || row.note === 'POCSAG Data';

export function lossStyle(row: Row): string {
  const v = row.loss ?? 0;
  if (v < 1) return '';
  if (v === 1) return green;
  if (v <= 3) return amber;
  return red;
}

export function berStyle(row: Row): string {
  const v = row.ber ?? 0;
  if (v === 0) return '';
  if (v <= 1.9) return green;
  if (v >= 2.0 && v <= 4.9) return amber;
  return red;
}

// functions.php: S-meter from the average dBm.
export function rssi(row: Row): string {
  if (row.rssi == null) return '';
  const raw = Math.trunc(row.rssi);
  const rel = raw + 93;
  const signal = Math.min(9, Math.max(0, Math.round(rel / 6 + 9)));
  return rel > 0 ? `S${signal}+${rel}dB (${raw})` : `S${signal} (${raw})`;
}
