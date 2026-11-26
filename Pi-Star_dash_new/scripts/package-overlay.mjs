import { chmodSync, cpSync, existsSync, lstatSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { build } from 'vite';
import { buildClassic } from './build-classic.mjs';
import { patchWifi } from '../compat/classic-wifi/patch.mjs';
import { patchExpert } from '../compat/classic-expert/patch.mjs';
import { patchBackup } from '../compat/classic-backup/patch.mjs';
import { patchConfigure } from '../compat/classic-configure/patch.mjs';

const project = resolve(fileURLToPath(new URL('../', import.meta.url)));

// These are full operator-facing templates, not an extension-based crawl.
// admin/index.php is normally a symlink to index.php. Includes, auth errors,
// JSON/download endpoints and bundled applications never enter this pass.
const pages = new Set([
  'index.html', 'index.php',
  ...'index calibration change_password_required config_backup configure live_modem_log power sysinfo update wifi'
    .split(' ').map(page => `admin/${page}.php`),
  ...`index jitter_test modem_fw_upgrade ssh_access upgrade
    edit_dapnetgateway edit_dashboard edit_dmrgateway edit_dstarrepeater edit_ircddbgateway
    edit_mmdvmhost edit_nxdngateway edit_p25gateway edit_starnetserver edit_timeserver edit_ysfgateway
    fulledit_apikeys fulledit_bmapikey fulledit_cron fulledit_dapnetapi fulledit_dmrgateway
    fulledit_pistar-remote fulledit_rssidat fulledit_tgifapikey fulledit_wpaconfig`
    .split(/\s+/).map(page => `admin/expert/${page}.php`),
]);

export function enhanceHead(page, bytes, { js, css }) {
  if (!pages.has(page)) return bytes;
  // buildClassic marks literal HTML head boundaries (and the one audited WiFi
  // echo). Mask opaque template regions, keeping byte offsets and encodings.
  const source = bytes.toString('latin1');
  const mask = s => ' '.repeat(s.length);
  const literal = source.replace(/<\?[\s\S]*?(?:\?>|$)/g, php => {
    if (page !== 'admin/wifi.php') return mask(php);
    const echo = /\becho '(<!DOCTYPE html[^']*?<body>)'/.exec(php);
    if (!echo) return mask(php);
    const start = echo.index + "echo '".length;
    return mask(php.slice(0, start)) + echo[1] + mask(php.slice(start + echo[1].length));
  }).replace(/<!--[\s\S]*?-->|<(script|style|title|textarea)\b[^>]*>[\s\S]*?<\/\1\s*>/gi, mask);
  const head = /<html\b[^>]*>\s*<head\b[^>]*>((?:(?!<\/?head\b|<body\b)[\s\S])*?<link rel="stylesheet" href="\/css\/pistar-modern\.css" \/>)<\/head\s*>\s*<body\b/i.exec(literal);
  if (!head || /<base\b/i.test(head[1])) return bytes;
  const at = head.index + head[0].indexOf(head[1]) + head[1].length;
  const viewport = /<meta\b[^>]*\bname\s*=\s*["']?viewport\b/i.test(head[1]) ? ''
    : '<meta name="viewport" content="width=device-width, initial-scale=1" />';
  // Keep this ASCII and single-quote-free: wifi.php emits its head from PHP.
  const tags = `${viewport}<link rel="stylesheet" data-pistar-classic="" href="${css}" /><script type="module" defer src="${js}"></script>`;
  return Buffer.from(source.slice(0, at) + tags + source.slice(at), 'latin1');
}

export async function packageOverlay({ source, dist = join(project, 'dist') } = {}) {
  if (!existsSync(join(dist, 'index.html'))) throw new Error('Run npm run build first');
  mkdirSync(join(project, 'build'), {recursive:true});
  const release = mkdtempSync(join(project, 'build/release-'));
  mkdirSync(join(release, 'staging'));
  cpSync(join(project, 'staging/server.py'), join(release, 'staging/server.py'));
  const root = buildClassic({source, out:join(release, 'docroot')});
  // The legacy writers rebuild whole files from the browser's partial model.
  // Patch only the release copy; base checkout and pristine skin stay intact.
  patchWifi(root);
  patchExpert(root);
  patchBackup(root);
  patchConfigure(root);
  const operator = join(root, 'operator');
  if (lstatSync(operator, { throwIfNoEntry: false })) throw Error('Upstream already has /operator');
  cpSync(dist, operator, {recursive:true, errorOnExist:true, force:false});
  // dist may have been built under a developer's private umask. These are
  // public static assets, served by www-data, not the user who made the copy.
  function publishStatic(path) {
    const st = lstatSync(path);
    if (st.isDirectory()) {
      chmodSync(path,0o755);
      for (const name of readdirSync(path)) publishStatic(join(path,name));
    } else if (st.isFile()) chmodSync(path,0o644);
    else throw Error(`Unexpected non-static asset: ${path}`);
  }
  const assets = join(operator, 'classic');
  mkdirSync(assets);
  // Independent, content-hashed entry. Do not replace the dashboard's dist
  // or pull its public demo/configuration assets into classic pages.
  const { output } = await build({
    root: project, configFile: join(project, 'vite.config.ts'), publicDir: false,
    build: { write: false, rollupOptions: { input: join(project, 'src/classic-entry.ts'),
      output: { entryFileNames: 'shell-[hash].js', assetFileNames: 'shell-[hash][extname]' } } },
  });
  const js = output.find(item => item.type === 'chunk' && item.isEntry);
  const css = output.find(item => item.fileName.endsWith('.css'));
  if (!js || !css || output.length !== 2) throw Error('Expected one classic module and one stylesheet');
  for (const item of output) writeFileSync(join(assets, item.fileName),
    item.type === 'chunk' ? item.code : item.source, { flag: 'wx' });
  publishStatic(operator);

  const urls = {js: `/operator/classic/${js.fileName}`, css: `/operator/classic/${css.fileName}`};
  const enhanced = [];
  for (const page of pages) {
    const path = join(root, page);
    if (!existsSync(path) || realpathSync(path) !== path || !lstatSync(path).isFile()) continue;
    const before = readFileSync(path);
    const after = enhanceHead(page, before, urls);
    if (after !== before) {
      writeFileSync(path, after);
      enhanced.push(page);
    }
  }
  return { root, enhanced };
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const { root, enhanced } = await packageOverlay();
  console.log(`Stock Pi-Star docroot with /operator/ and Svelte navigation in ${enhanced.length} classic templates: ${root}`);
}
