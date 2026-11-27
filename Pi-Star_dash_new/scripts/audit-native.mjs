import { lstatSync, readdirSync, readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

export function auditNative(root) {
  let files = 0;
  function inspect(path) {
    const info = lstatSync(path);
    if (info.isSymbolicLink()) throw Error(`Symlink in native artifact: ${path}`);
    if (/(?:^|\/)(?:classic|compat|docroot)(?:\/|$)|\.(?:php\d*|phtml|phar|inc)$/i.test(path.slice(root.length))) {
      throw Error(`Legacy runtime in native artifact: ${path}`);
    }
    if (info.isDirectory()) {
      for (const entry of readdirSync(path)) inspect(join(path, entry));
    } else if (info.isFile()) {
      const bytes = readFileSync(path);
      if (/<\?(?:php\b|=)/i.test(bytes.toString('latin1'))) throw Error(`PHP code in native artifact: ${path}`);
      if (path.endsWith('/DEBIAN/control') && /^(?:Pre-Depends|Depends|Recommends|Suggests):[^\n]*\bphp/im.test(bytes.toString())) {
        throw Error('PHP dependency in native artifact');
      }
      if ((path.includes('/systemd/') || path.includes('/DEBIAN/')) && /\b(?:php-fpm|php-cgi|\/php(?:\d(?:\.\d)?)?)\b/i.test(bytes.toString())) {
        throw Error(`PHP runtime command in native artifact: ${path}`);
      }
      files++;
    } else throw Error(`Non-regular file in native artifact: ${path}`);
  }
  inspect(root);
  if (!files) throw Error('Empty native artifact');
  return files;
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  if (!process.argv[2]) throw Error('Usage: node scripts/audit-native.mjs DIRECTORY');
  console.log(`PASS native artifact audit: ${auditNative(resolve(process.argv[2]))} files; no PHP or classic runtime`);
}
