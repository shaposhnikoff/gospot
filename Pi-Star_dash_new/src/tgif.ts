// Formatting of the classic "Active TGIF Connections" table
// (mmdvmhost/tgif_static_links.php).

export type TGIFState = 'ok' | 'unavailable' | 'denied';
export type TGIFLinks = {
  id: string; static: string[] | null; dynamic: string[] | null;
  staticState: TGIFState; dynamicState: TGIFState; errors: string[] | null;
};

// PHP wordwrap($text, $width, "\n", false), split into lines. Words longer
// than the width are never cut.
export function wordwrap(text: string, width: number): string[] {
  const lines: string[] = [];
  let lastStart = 0, lastSpace = 0, current = 0;
  for (; current < text.length; current++) {
    if (text[current] === ' ') {
      if (current - lastStart >= width) {
        lines.push(text.slice(lastStart, current));
        lastStart = current + 1;
      }
      lastSpace = current;
    } else if (current - lastStart >= width && lastStart < lastSpace) {
      lines.push(text.slice(lastStart, lastSpace));
      lastStart = lastSpace = lastSpace + 1;
    }
  }
  if (lastStart !== current) lines.push(text.slice(lastStart));
  return lines;
}

// One TG cell: 'TG91(1) ' entries wrapped at 15 characters, or the classic
// fallback text when there are none.
export function tgifCell(list: string[] | null, state: TGIFState): string[] {
  if (list && list.length) return wordwrap(list.map(t => t + ' ').join(''), 15);
  if (state === 'denied') return ['Token too old'];
  return [state === 'ok' ? 'None' : 'Unavailable'];
}

// The admin page refreshes the table every 180 s; a failed load is retried
// once after 15 s (admin/index.php reloadtgifStaticConnections).
export function nextTGIFRefresh(ok: boolean, retried: boolean): { delay: number; retried: boolean } {
  if (!ok && !retried) return { delay: 15000, retried: true };
  return { delay: 180000, retried };
}
