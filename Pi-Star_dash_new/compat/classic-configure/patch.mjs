import { createHash } from 'node:crypto';
import { chmodSync, lstatSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { fullApply } from './full-apply.mjs';

// This is a release-copy patch, never a migration of the upstream checkout.
export function patchConfigure(root) {
  const page = resolve(root, 'admin/configure.php');
  const stockPage = fileURLToPath(new URL('../../../Pi-Star_DV_Dash/admin/configure.php', import.meta.url));
  if (page === stockPage ||
      realpathSync(page) !== page || !lstatSync(page).isFile() || lstatSync(page).nlink !== 1)
    throw Error('Expected a regular release copy of admin/configure.php');
  const source = readFileSync(page, 'latin1');
  const stock = source.replace(/<link rel="stylesheet" href="\/css\/pistar-modern\.css" \/>(?=<\/head>)/, '');
  if (createHash('sha256').update(stock, 'latin1').digest('hex') !==
    'cb8afcd14e7d828e56c95fdbb322b4733202a9bb9939c35c5dedb168e31b8480')
    throw Error('Unknown configure.php; review callsign propagation and security gates before packaging');

  // Snapshot the rendered form's defaults before scripts or autofill change
  // them. Compare successful controls, including hidden fields, radios and
  // repeated names. Never reset or replace the operator's live form.
  const output = source.replace('    </form>\n', match => match + `<script>
    (function (form) {
        if (!form || !window.FormData || !FormData.prototype.entries) return;
        var defaults = form.cloneNode(true);
        defaults.reset();
        var original = new FormData(defaults);
        function others(data) {
            return JSON.stringify(Array.from(data.entries()).filter(function (item) {
                return item[0] !== 'confCallsign' && item[0] !== 'dmrColorCode';
            }));
        }
        var previous = original.get('confCallsign'), oldColor = original.get('dmrColorCode'), baseline = others(original);
        var apply = window.submitform;
        window.submitform = function () {
            var action = form.action;
            var current = new FormData(form);
            if (others(current) === baseline) {
                var params = [];
                if (previous !== null && current.get('confCallsign') !== previous)
                    params.push('previousCallsign=' + encodeURIComponent(previous));
                if (oldColor !== null && current.get('dmrColorCode') !== oldColor)
                    params.push('previousColorCode=' + encodeURIComponent(oldColor));
                if (params.length) form.action = action + '?' + params.join('&');
            }
            try { apply(); } finally { form.action = action; }
        };
    })(document.getElementById('config'));
    </script>
`).replace('$progname = basename', `// The stock page, native CSRF and remote-password gates have already run.
if ($_SERVER['PHP_SELF'] === '/admin/configure.php' && $_SERVER['REQUEST_METHOD'] === 'POST'
        && (array_key_exists('previousCallsign', $_GET) || array_key_exists('previousColorCode', $_GET))) {
    require_once __DIR__.'/callsign-apply.php';
    try {
        callsign_apply($_POST, $_GET['previousCallsign'] ?? null, $configs, $configmmdvm, $configysfgateway, $configdstar, $configPistarRelease, $_GET['previousColorCode'] ?? null);
        header('Location: /admin/configure.php', true, 303);
    } catch (RuntimeException $error) {
        http_response_code($error->getCode());
        header('Content-Type: text/html; charset=UTF-8');
        echo '<p role="alert">'.htmlspecialchars($error->getMessage(), ENT_QUOTES, 'UTF-8').'</p>'
            .'<a href="/admin/configure.php">Return to configuration</a>';
    }
    exit;
}
$progname = basename`).replace("if (empty($_POST['dmrColorCode']) != TRUE )",
    "if (isset($_POST['dmrColorCode']) && is_string($_POST['dmrColorCode']) && preg_match('/^(?:[0-9]|1[0-5])$/D', $_POST['dmrColorCode']))");
  const helper = join(root, 'admin/callsign-apply.php');
  writeFileSync(helper, readFileSync(new URL('./callsign-apply.php', import.meta.url)), {flag: 'wx'});
  chmodSync(helper, 0o644);
  const full = join(root, 'admin/full-apply.php');
  writeFileSync(full, readFileSync(new URL('./full-apply.php', import.meta.url)), {flag:'wx'});
  chmodSync(full, 0o644);
  writeFileSync(page, fullApply(output), 'latin1');
}
