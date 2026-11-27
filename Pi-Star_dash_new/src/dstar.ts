// D-Star panels of the classic dashboard, ported from Pi-Star_DV_Dash
// index.php and dstarrepeater/*.php (revision 10bf34c9).

export type DStarLinkRow = {
  Incoming: boolean; Radio: string; Default: string; Auto: string; Timer: string;
  Status: string; Linked: string; Mode: string; Direction: string; Changed: string;
};
export type DStarCCSRow = { Repeater: string; Remote: string; Direction: string; Changed: string };
export type DStarStarNet = {
  Groups: { Callsign: string; Logoff: string; Info: string; UserTimeout: string; GroupTimeout: string }[] | null;
  Members: { Clock: string; Group: string; Callsign: string }[] | null;
};
export type DStarHeardRow = {
  Time: string; Clock: string; Callsign: string; ID: string; Lookup: string; Your: string; RPT1: string; RPT2: string;
};
export type DStarInfo = {
  Network: boolean;
  Gateway: { IRCDDB: string; APRS: string; Flags: { label: string; state: string }[] | null } | null;
  Links: DStarLinkRow[] | null;
  CCS: DStarCCSRow[] | null;
  CCSShow: boolean;
  StarNet: DStarStarNet | null;
  Heard: DStarHeardRow[] | null;
  Local: DStarHeardRow[] | null;
};

export type DStarManager = {
  csrf: string; available: boolean; remote: boolean; modules: string[]; reflectors: string[];
  defaultName: string; defaultLetter: string; keeper: string[] | null;
};

export type DStarVisibility = { gateway: boolean; links: boolean; manager: boolean; ccs: boolean; heard: boolean };

// index.php: which D-Star partials appear for the controller and page variant.
//   MMDVMHost, [D-Star Network] Enable=1: reflector links and the link manager
//     on the admin page only; CCS connections on both.
//   DStarRepeater: gateway config, reflector links, CCS, last heard and local
//     TX on both; the link manager on the admin page.
export function dstarVisibility(controller: string | undefined, info: DStarInfo | null | undefined, admin: boolean): DStarVisibility {
  const none = { gateway: false, links: false, manager: false, ccs: false, heard: false };
  if (!info) return none;
  if (controller === 'dstarrepeater') return { gateway: true, links: true, manager: admin, ccs: true, heard: true };
  if (controller === 'mmdvmhost' && info.Network) return { gateway: false, links: admin, manager: admin, ccs: true, heard: false };
  return none;
}

// str_replace(' ', '&nbsp;', ...) of the classic cells.
export const nbsp = (s: string | null | undefined) => (s ?? '').replaceAll(' ', ' ');

export const flagStyle = (state: string) => state === 'on' ? 'background-color:#1d1;' : 'background:#606060; color:#b0b0b0;';

// last_herd.php / local_tx.php: RadioID or QRZ (CSS Tool > Lookup Service) and aprs.fi for dPRS.
export function dstarCall(row: DStarHeardRow, lookup = 'RadioID') {
  const url = encodeURIComponent(row.Lookup);
  const base = lookup === 'QRZ' ? 'https://www.qrz.com/db/' : 'https://database.radioid.net/database/view?callsign=';
  return { text: row.Callsign, id: row.ID, href: base + url, dprs: 'https://aprs.fi/#!call=' + url + '*' };
}

// active_starnet_groups.php links members to QRZ.
export const starNetHref = (call: string) => 'http://www.qrz.com/db/' + encodeURIComponent(call);

export const letters = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('');

export type DStarForm = { module: string; ref: string; text: string; custom: boolean; letter: string; action: 'LINK' | 'UNLINK' };

// link_manager.php posts the reflector select or, after "Text Entry", the text field.
export function dstarLinkBody(f: DStarForm) {
  return { action: f.action, module: f.module, refName: f.custom ? f.text : f.ref, letter: f.letter, custom: f.custom };
}

// The classic page echoes input errors as bare text and command results in a
// "Command Output" table.
export function dstarOutput(status: number | null, text: string): { text: string; table: boolean } {
  if (status === null) return { text: 'No response from the board', table: true };
  return { text: text.trim(), table: status !== 400 };
}
