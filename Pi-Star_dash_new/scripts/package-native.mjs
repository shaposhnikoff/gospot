import { chmodSync, cpSync, mkdirSync, mkdtempSync, readdirSync, statSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { auditNative } from './audit-native.mjs';

const project = fileURLToPath(new URL('../', import.meta.url));

export function packageNative({ dist = join(project, 'dist'), output = join(project, 'build') } = {}) {
  auditNative(dist);
  if (!statSync(join(dist, 'index.html')).isFile()) throw Error('Missing native index.html');
  mkdirSync(output, { recursive: true });
  const release = mkdtempSync(join(output, 'release-'));
  const ui = join(release, 'ui');
  cpSync(dist, ui, { recursive: true, errorOnExist: true, force: false });
  writeFileSync(join(ui, 'runtime.json'), JSON.stringify({ backend: 'pistar-go', wizardPort: 8788 }) + '\n');
  function publish(path) {
    const directory = statSync(path).isDirectory();
    chmodSync(path, directory ? 0o755 : 0o644);
    if (directory) for (const entry of readdirSync(path)) publish(join(path, entry));
  }
  publish(ui);
  auditNative(ui);
  return release;
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  console.log(`Native release: ${packageNative()}`);
}
