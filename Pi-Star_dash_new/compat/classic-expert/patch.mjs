import { createHash } from 'node:crypto';
import { lstatSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';

// Apply only to a fresh package copy. Pin the editor so changes to
// upstream's security gates or privileged write path require a new review.
export function patchExpert(root) {
  patchEditor(root, 'DMRGateway', 'dmrgw', '02f9353887ec6afb92d356e1443f2a6ce19dc28757757fc61236a35586f5bbe0');
  patchEditor(root, 'NXDNGateway', 'nxdngw', '035441ff7601f4c7efabef69cd668aae4e99b0790c624674cc48b35891e00bf0');
  patchEditor(root, 'YSFGateway', 'ysfgw', '43d4a78de0477c7d6f791dbc4e3c37a5ce806b61b659ac076a894f24386bc693');
  patchEditor(root, 'P25Gateway', 'p25gw', '9580bad2fb420a1e12ee37eebc84a7fb5a3c93c72a149010d280b236884ceae3');
  patchEditor(root, 'DAPNETGateway', 'dapnetgw', 'a62a625a4b1bfabb711027856634ae03ebd90e0acc89baff89cda2b90c5f06b1');
  patchEditor(root, 'MMDVMHost', 'mmdvm', 'd52794601e3cacf8c9f2b122086726f8a68b47ba49fad639d68ab1a5b770501e');
  patchEditor(root, 'DStarRepeater', 'dstar', 'da33885cd88e453f5af6a7f9de701c4b2939d4c64c5cce77470ace64361bb838', true);
  patchEditor(root, 'ircDDBGateway', 'ircddbgw', '00c18faa83e1b8114c48d72f5c3446b53a6bbc863cf6f59f0b886bc2cd4f3cfc', true);
  patchEditor(root, 'TimeServer', 'timesrv', '009bc699502524b778fe2f77e106480c22aa32fc1a912d10cc0e47e15a5100af', true);
  patchEditor(root, 'StarNetServer', 'starnet', '483c7601161530a3ff375bb0941bbbe67805177594c278026a372d67fd2e6340', true);
  writeFileSync(join(root, 'admin/expert/fulledit-text.php'),
    readFileSync(new URL('./full-text.php', import.meta.url)), {flag:'wx'});
  patchFullEdit(root, 'dmrgateway', '09bfeea49319a4f0645bc0e95d7c73fce695cd2e833acda8c4a1afeeae83a86a');
  patchFullEdit(root, 'wpaconfig', '5ec428f7fb1eaa02a79e6f6d892e8f7fbe9a3d911b63932b05f4640fbc248e53');
  patchFullEdit(root, 'pistar-remote', 'ae3237f18782fdb6458cb003a1b2d2b6f5f00e5427d8766177941cadb096a9bc');
  patchFullEdit(root, 'rssidat', '09439943d8956a17f169428058a2a5dc2cf18ee4438f28f9be180a1cba948566');
  patchTgifApiKey(root);
}

function readFullEdit(root, editor, digest) {
  const page = resolve(root, `admin/expert/fulledit_${editor}.php`);
  if (realpathSync(page) !== page || !lstatSync(page).isFile() || lstatSync(page).nlink !== 1)
    throw Error(`Unexpected Full Edit path: ${page}`);
  const source = readFileSync(page, 'latin1');
  const stock = source.replace(/<link rel="stylesheet" href="\/css\/pistar-modern\.css" \/>(?=<\/head>)/, '');
  if (createHash('sha256').update(stock, 'latin1').digest('hex') !== digest)
    throw Error(`Unknown ${editor} Full Edit; review before packaging`);
  return {page, source};
}

function patchTgifApiKey(root) {
  const {page, source} = readFullEdit(root, 'tgifapikey', '8a4e6c78f2f710a4057d812e0c7f8c9f34965f9d14dd700cc19c07583578dab4');
  // The writer is declared inside the >=4.3.9 branch, so unlike a
  // top-level PHP function it isn't available until execution reaches it.
  const submit = source.indexOf('//after the form submit');
  const writer = source.indexOf('//this is the function going to update your ini file', submit);
  const render = source.indexOf('\n//parse the ini file', writer);
  if (!(submit >= 0 && writer > submit && render > writer)) throw Error('Missing TGIF editor blocks');
  writeFileSync(page, source.slice(0,submit) + source.slice(writer,render+1) +
    source.slice(submit,writer) + source.slice(render+1), 'latin1');
}

function patchFullEdit(root, editor, digest) {
  const {page, source} = readFullEdit(root, editor, digest);
  const output = source.replace('charset=iso-8859-1', 'charset=UTF-8')
    .replace("require_once('../config/version.php');", `require_once('../config/version.php');
require_once __DIR__ . '/fulledit-text.php';
header('Content-Type: text/html; charset=UTF-8');`)
    .replace(/        \$fh = fopen\(\$filepath, 'w'\);[\s\S]*?fclose\(\$fh\);/, `        $original = file_get_contents($filepath);
        if ($original === false) { http_response_code(500); exit; }
        if (!is_string($_POST['data'])) { http_response_code(400); exit; }
        $data = pistar_full_edit_text($original, $_POST['data']);
        if (file_put_contents($filepath, $data) !== strlen($data)) { http_response_code(500); exit; }`)
    .replace(/\$fh = fopen\(\$filepath, 'r'\);[\s\S]*?fclose\(\$fh\);/, '$theData = file_get_contents($filepath);')
    // HTML drops the first literal newline immediately following <textarea>.
    .replace('rows="45"><?php', 'rows="45">\n<?php');
  writeFileSync(page, output, 'latin1');
}

function patchEditor(root, label, prefix, digest, flat = false) {
  const service = label.toLowerCase();
  const section = flat ? `'${service}'` : 'null';
  const page = resolve(root, `admin/expert/edit_${service}.php`);
  if (realpathSync(page) !== page || !lstatSync(page).isFile() || lstatSync(page).nlink !== 1)
    throw Error(`Unexpected Expert editor path: ${page}`);
  const source = readFileSync(page, 'latin1');
  const stock = source.replace(/<link rel="stylesheet" href="\/css\/pistar-modern\.css" \/>(?=<\/head>)/, '');
  if (createHash('sha256').update(stock, 'latin1').digest('hex') !== digest)
    throw Error(`Unknown ${label} Expert editor; review before packaging`);
  const output = source
    .replace(/\/\*\*[\s\S]*?\*\//, `/** ${label} byte-preserving editor (package compatibility correction). */`)
    .replace("require_once('../config/version.php');", `require_once('../config/version.php');
require_once __DIR__ . '/${service}-entry.php';
header('Content-Type: text/html; charset=UTF-8');
try {
    $gatewayText = ${prefix}_load_and_apply(${section});
} catch (RuntimeException $error) {
    http_response_code($error->getCode());
    $gatewayError = $error->getMessage();
}`)
    .replace('charset=iso-8859-1', 'charset=UTF-8')
    .replace(/\/\/\s*(?:Do some file wrangling|Stage a copy of \/etc\/ircddbgateway)[\s\S]+?echo "<\/form>";/, `if (isset($gatewayError)) {
    echo '<p role="alert">'.${prefix}_html($gatewayError).'</p>';
} else {
    ${prefix}_form($gatewayText, ${service === 'timeserver' ? "'Save Changes'" : "$lang['apply']"}, ${section});
}`);
  if (!output.includes(`${prefix}_form($gatewayText,`) || output.includes('function update_ini_file('))
    throw Error(`Failed to replace ${label} Quick Edit handler`);
  // Specialize at build time: each shipped helper has one literal INI and
  // one literal restart command, never a target selected by the request.
  const helper = readFileSync(new URL('./dmrgateway-entry.php', import.meta.url), 'utf8')
    .replaceAll('dmrgateway', service).replaceAll('DMRGateway', label).replaceAll('dmrgw', prefix);
  writeFileSync(join(root, `admin/expert/${service}-entry.php`), helper, { flag: 'wx' });
  writeFileSync(page, output, 'latin1');
}
