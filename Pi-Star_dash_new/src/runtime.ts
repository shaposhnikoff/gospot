import type { HeardRow, Signal, Snapshot } from './data';

type Obj = Record<string, unknown>;
const object = (x: unknown): x is Obj => !!x && typeof x === 'object' && !Array.isArray(x);
const strings = (o: Obj, keys: string[]) => keys.every(k => typeof o[k] === 'string');
const signal = (x: unknown): x is Signal => object(x) && strings(x, ['Detail','Source','At']) && ['up','down','unknown'].includes(String(x.State));
function row(x: unknown): x is HeardRow {
  return object(x) && strings(x, ['At','From','To','Slot']) && (x.Source === 'RF' || x.Source === 'Net')
    && ['Duration','BER','Loss','RSSI'].every(k => x[k] == null || typeof x[k] === 'number');
}
export function asSnapshot(x: unknown): Snapshot {
  if (!object(x) || !strings(x, ['Node','UpdatedAt']) || !signal(x.Modem) || !signal(x.Master) || !object(x.Radio)
      || !strings(x.Radio, ['Callsign','ID','RX','TX','Operation','CC','Slots']) || typeof x.HeardAvailable !== 'boolean') throw Error('Неизвестный формат снимка');
  // Go encodes an uninitialised slice as null.
  const heard = x.Heard === null ? [] : x.Heard;
  const modes = x.Radio.Modes === null ? [] : x.Radio.Modes;
  if (!Array.isArray(heard) || !heard.every(row) || !Array.isArray(modes) || !modes.every(s=>typeof s==='string')) throw Error('Неизвестный формат журнала');
  return {...x, Radio:{...x.Radio,Modes:modes},Heard:heard} as Snapshot;
}
export async function readJSON(path: string): Promise<unknown> {
  const r = await fetch(path, {cache:'no-store',mode:'same-origin',signal:AbortSignal.timeout(8000),headers:{Accept:'application/json'}});
  if (!r.ok) throw Error(`HTTP ${r.status}`);
  return r.json();
}
export function serviceOrigin(origin: string, protocol: 'http:'|'https:', port: unknown): string | null {
  if (typeof port !== 'number' || !Number.isInteger(port) || port < 1 || port > 65535) return null;
  const url = new URL(origin);
  url.protocol = protocol;
  url.port = String(port);
  return url.origin;
}
export async function detectRuntime(): Promise<{mode:'demo'|'pi', classicPort:number|null, wizardPort:number|null, preview:boolean}> {
  const x = await readJSON('./runtime.json');
  if (object(x) && x.backend === 'gospot-go') {
    const port = x.classicHttpsPort;
    return {mode:'pi', classicPort:typeof port === 'number' && Number.isInteger(port) && port > 0 && port <= 65535 ? port : null,
      wizardPort:typeof x.wizardPort === 'number' ? x.wizardPort : null, preview:x.preview === true};
  }
  if (object(x) && x.backend === 'demo') return {mode:'demo',classicPort:null,wizardPort:null,preview:false};
  throw Error('Не распознан режим панели');
}

// HTTP origin of the operator dashboard as advertised by the management server.
export async function operatorOrigin(): Promise<string | null> {
  try {
    const c = await readJSON('/runtime.json');
    return serviceOrigin(location.origin, 'http:', object(c) ? c.operatorPort : null);
  } catch {
    return null;
  }
}
