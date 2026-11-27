// admin/calibration.php: how the page reads MMDVMCal output, the BER figures
// and the chart. Pure logic; Calibration.svelte does the requests.

export type Mode = 'DStar' | 'DMR' | 'YSF' | 'P25' | 'NXDN';

/** The mode buttons in page order: id, label, MMDVMCal key. */
export const MODES: [Mode, string, string][] = [
  ['DStar', 'D-Star', 'k'], ['DMR', 'DMR', 'b'], ['YSF', 'YSF', 'J'], ['P25', 'P25', 'j'], ['NXDN', 'NXDN', 'n'],
];

// Checked in this order; as in the classic page, a later one wins.
const BANNERS: [Mode, string][] = [
  ['DStar', '\nBER Test Mode (FEC) for D-Star'], ['DMR', '\nBER Test Mode (FEC) for DMR Simplex'],
  ['YSF', '\nBER Test Mode (FEC) for YSF'], ['P25', '\nBER Test Mode (FEC) for P25'], ['NXDN', '\nBER Test Mode (FEC) for NXDN'],
];

export interface Scan {
  /** MMDVMCal printed its version: the page then sets the saved frequency. */
  version: boolean;
  /** pistar-mmdvmcal's last line ("Finnished...", sic): every LED goes red. */
  finished: boolean;
  mode: Mode | null;
  /** Last " frequency: N" (TX frequency / New frequency). */
  frequency: number | null;
  frames: number;
  errors: number;
  bits: number;
  /** "voice end received,": the totals restart after the next update. */
  eot: boolean;
}

/** One poll's output, as the classic $.get callback reads it. */
export function scan(data: string): Scan {
  const text = '\n' + data;
  let mode: Mode | null = null;
  for (const [m, banner] of BANNERS) if (text.includes(banner)) mode = m;
  let frequency: number | null = null;
  for (const m of data.matchAll(/ frequency: (\d+)/g)) frequency = Number(m[1]);
  let frames = 0, errors = 0, bits = 0;
  for (const m of data.matchAll(/% \((\d+)\/(\d+)\)/g)) {
    frames += 1;
    errors += Number(m[1]);
    bits += Number(m[2]);
  }
  return {
    version: text.includes('Version:'), finished: text.includes('Finnished...'), mode, frequency,
    frames, errors, bits, eot: data.includes('voice end received,'),
  };
}

/** The value Start sends with "e" once MMDVMCal is ready: base + saved offset. */
export const startFrequency = (rxFrequency: number, rxOffset: number) => String(Math.trunc(rxFrequency) + Math.trunc(rxOffset));

/** Offset label for a frequency line: frequency - RXFrequency. */
export const offsetOf = (frequency: number, rxFrequency: number) => String(Math.trunc(frequency) - Math.trunc(rxFrequency));

/** Offsets the board accepts (gospot-configure writes [Modem] RXOffset). */
export const validOffset = (v: string) => /^-?\d{1,6}$/.test(v.trim());

export interface Figures { frames: string; bits: string; errors: string; ber: string; seconds?: string }

const percent = (errors: number, bits: number) => (errors / bits * 100).toFixed(2) + '%';

/** The Current/Total table and the chart points, updated once per poll. */
export class BerMeter {
  private cnt = 0;
  private tcnt = 0;
  private c = { frames: 0, bits: 0, errors: 0 };
  private t = { frames: 0, bits: 0, errors: 0 };
  private eot = false;
  current: Figures | null = null;
  total: Figures | null = null;
  points: [number, number][] = [[0, 0]];

  add(s: Scan) {
    this.c.frames += s.frames; this.c.errors += s.errors; this.c.bits += s.bits;
    this.t.frames += s.frames; this.t.errors += s.errors; this.t.bits += s.bits;
    if (s.eot) this.eot = true;
  }

  /** Called after every poll; true when the figures changed. */
  tick(every: number): boolean {
    if (this.c.bits <= 0) return false;
    this.cnt++; this.tcnt++;
    if (this.tcnt % every !== 0 && !this.eot) return false;
    const c = this.c, t = this.t;
    this.current = { frames: String(c.frames), bits: String(c.bits), errors: String(c.errors), ber: percent(c.errors, c.bits) };
    this.points = [...this.points, [this.cnt, c.errors / c.bits * 100]].slice(-(WINDOW + 1)) as [number, number][];
    this.c = { frames: 0, bits: 0, errors: 0 };
    this.total = { frames: String(t.frames), bits: String(t.bits), errors: String(t.errors), ber: percent(t.errors, t.bits), seconds: String(this.tcnt) };
    if (this.eot) {
      this.eot = false;
      this.t = { frames: 0, bits: 0, errors: 0 };
      this.tcnt = 0;
    }
    return true;
  }
}

/** Seconds shown by the chart: the last three minutes, like the classic plot. */
export const WINDOW = 60 * 3;

export interface Chart { line: string; area: string; xmin: number; xmax: number }

/** SVG paths for the BER chart (0..5 %, filled to zero) in a w x h box. */
export function chart(points: [number, number][], w: number, h: number): Chart {
  const last = points.length ? points[points.length - 1][0] : 0;
  const xmax = Math.max(last, 10), xmin = last > WINDOW ? last - WINDOW : 0;
  const x = (v: number) => ((v - xmin) / (xmax - xmin) * w).toFixed(1);
  const y = (v: number) => (h - Math.min(Math.max(v, 0), 5) / 5 * h).toFixed(1);
  const shown = points.filter(([px]) => px >= xmin);
  if (!shown.length) return { line: '', area: '', xmin, xmax };
  const line = shown.map(([px, py], i) => `${i ? 'L' : 'M'}${x(px)},${y(py)}`).join(' ');
  const area = `${line} L${x(shown[shown.length - 1][0])},${h} L${x(shown[0][0])},${h} Z`;
  return { line, area, xmin, xmax };
}
