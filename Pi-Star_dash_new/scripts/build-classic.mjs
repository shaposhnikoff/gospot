import * as fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { parseArgs } from 'node:util';

const project = path.resolve(fileURLToPath(new URL('../', import.meta.url)));
const build = path.resolve(project, 'build');
const css = 'css/pistar-modern.css';
const link = `<link rel="stylesheet" href="/${css}" />`;
const defaults = {
  source: path.resolve(project, '../Pi-Star_DV_Dash'),
  out: path.join(build, 'overlay/docroot'),
};
const usage = `node scripts/build-classic.mjs [--source DIR] [--out DIR]
Defaults: ../Pi-Star_DV_Dash and build/overlay/docroot (relative to the prototype).
Explicit paths are relative to the working directory. Output must be a fresh
directory below the prototype's build/, or an identical previous build.
For changed inputs or local edits choose a fresh --out. Nothing is deleted.`;

function inside(dir, file) {
  const rel = path.relative(dir, file);
  return rel !== '' && rel !== '..' && !rel.startsWith(`..${path.sep}`) && !path.isAbsolute(rel);
}

function skin(name, bytes) {
  if (!/\.(?:php|phtml|html?|inc)$/i.test(name)) return bytes;
  // Latin-1 is a reversible byte mapping; leave legacy language encodings alone.
  const text = bytes.toString('latin1');
  if (!/<\/head\s*>/i.test(text)) return bytes;
  // Match literal template HTML, skipping PHP, comments and raw-text elements.
  // Two upstream pages also emit complete documents from single-quoted PHP
  // strings. Patch only their known markup; the link has no single quotes.
  const output = text.replace(/<\?(?:php\b|=)[\s\S]*?(?:\?>|$)|<!--[\s\S]*?-->|<(script|style|textarea|title)\b[^>]*>[\s\S]*?<\/\1\s*>|<\/head\s*>/gi, (part) => {
    if (/^<\/head/i.test(part)) return link + part;
    if (part.startsWith('<?')) {
      if (name === 'admin/wifi.php') {
        return part.replace(/(<title>Pi-Star - Digital Voice Dashboard - WiFi Config<\/title>\s*)(<\/head>)/i, `$1${link}$2`);
      }
      if (name === 'config/csrf.php') {
        return part.replace(/(<title>403 Forbidden<\/title>)(<\/head>)/i, `$1${link}$2`);
      }
    }
    return part;
  });
  return Buffer.from(output, 'latin1');
}

// Never recurse into links. Inventory before writing so that collisions,
// unsupported special files and invalid link targets fail before publication.
function inventory(root, expected) {
  const entries = new Map();
  function visit(dir) {
    for (const item of fs.readdirSync(path.join(root, dir), { withFileTypes: true })) {
      if (!expected && item.name === '.git') continue;
      const name = path.join(dir, item.name);
      const full = path.join(root, name);
      if (expected && !expected.has(name)) throw Error(`Unmanaged output: ${name}; choose a fresh --out`);
      if (item.isDirectory()) {
        entries.set(name, { kind: 'dir' });
        visit(name);
      } else if (item.isSymbolicLink()) {
        entries.set(name, { kind: 'link', target: fs.readlinkSync(full) });
      } else if (item.isFile()) {
        const bytes = fs.readFileSync(full);
        entries.set(name, { kind: 'file', mode: fs.statSync(full).mode & 0o777,
          bytes: expected ? bytes : skin(name, bytes) });
      } else {
        throw Error(`Unsupported entry: ${full}`);
      }
    }
  }
  visit('');
  return entries;
}

// Resolve link chains against the inventory, never against the host filesystem.
// A lexical resolve alone would miss e.g. child/alias/../../outside.
function checkLinks(entries) {
  for (const [name, entry] of entries) {
    if (entry.kind !== 'link') continue;
    let hops = 0;
    const stack = name.split('/').slice(0, -1);
    const pending = [entry.target.split('/')];
    if (path.isAbsolute(entry.target)) throw Error(`Absolute link: ${name}`);
    while (pending.length) {
      const current = pending.at(-1);
      if (!current.length) { pending.pop(); continue; }
      const part = current.shift();
      if (!part || part === '.') continue;
      if (part === '..') {
        if (!stack.length) throw Error(`Escaping link: ${name}`);
        stack.pop();
      } else {
        if (part === '.git') throw Error(`Link into .git: ${name}`);
        stack.push(part);
        const next = entries.get(stack.join('/'));
        if (next?.kind === 'link') {
          if (++hops > 40 || path.isAbsolute(next.target)) throw Error(`Unsafe link chain: ${name}`);
          stack.pop();
          pending.push(next.target.split('/'));
        }
      }
    }
  }
}

function unchanged(a, b) {
  return a?.kind === b.kind && (b.kind === 'dir' ||
    (b.kind === 'link' ? a.target === b.target : a.mode === b.mode && a.bytes.equals(b.bytes)));
}

export function buildClassic({ source = defaults.source, out = defaults.out } = {}) {
  source = path.resolve(source);
  out = path.resolve(out);
  if (!inside(build, out) || out.split('/').includes('.git') ||
      out === source || inside(source, out) || inside(out, source)) {
    throw Error(`Output must be below ${build} and separate from the source`);
  }
  if (fs.realpathSync(source) !== source || !fs.lstatSync(source).isDirectory()) {
    throw Error(`Source must be a real directory without symlink components: ${source}`);
  }
  // Check *every* existing output component, including dangling symlinks.
  let dir = project;
  for (const part of path.relative(project, out).split(path.sep)) {
    dir = path.join(dir, part);
    const stat = fs.lstatSync(dir, { throwIfNoEntry: false });
    if (stat && !stat.isDirectory()) throw Error(`Not a real output directory: ${dir}`);
  }
  const entries = inventory(source);
  if (entries.has(css) || (entries.has('css') && entries.get('css').kind !== 'dir')) {
    throw Error(`Upstream asset path collision: ${css}`);
  }
  const sheet = path.resolve(project, 'compat/pistar-modern.css');
  if (fs.realpathSync(sheet) !== sheet || !fs.lstatSync(sheet).isFile()) throw Error(`Invalid stylesheet: ${sheet}`);
  if (!entries.has('css')) entries.set('css', { kind: 'dir' });
  entries.set(css, { kind: 'file', bytes: fs.readFileSync(sheet), mode: 0o644 });
  checkLinks(entries);

  if (fs.existsSync(out) && fs.readdirSync(out).length) {
    const existing = inventory(out, entries);
    for (const [name, item] of existing) {
      if (!unchanged(entries.get(name), item)) throw Error(`Existing output differs at ${name}; choose a fresh --out`);
    }
    if (existing.size !== entries.size) throw Error('Incomplete existing output; choose a fresh --out');
  } else {
    fs.mkdirSync(out, { recursive: true });
    for (const [name, item] of entries) {
      const dst = path.join(out, name);
      if (item.kind === 'dir') fs.mkdirSync(dst);
      else if (item.kind === 'link') fs.symlinkSync(item.target, dst);
      else {
        fs.writeFileSync(dst, item.bytes, { flag: 'wx', mode: item.mode });
        fs.chmodSync(dst, item.mode);
      }
    }
  }
  return out;
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  try {
    const { values } = parseArgs({ options: {
      source: { type: 'string' }, out: { type: 'string' }, help: { type: 'boolean', short: 'h' },
    } });
    console.log(values.help ? usage : `Classic docroot ready: ${buildClassic(values)}`);
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
