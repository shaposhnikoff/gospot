import { createHash } from 'node:crypto';
import { lstatSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';

// Pin buildClassic's skinned copy; never patch the unskinned upstream page.
export function patchBackup(root) {
  const page = resolve(root, 'admin/config_backup.php');
  if (realpathSync(page) !== page || !lstatSync(page).isFile() || lstatSync(page).nlink !== 1)
    throw Error('Expected a fresh packaged backup page');
  let php = readFileSync(page, 'utf8');
  if (createHash('sha256').update(php).digest('hex') !==
      'fdd5cbd89914897be61b22f581a2c2d12164e46e4db90d8cc0fb41e8ca871555')
    throw Error('Unknown classic backup page; review before packaging');

  // Binary responses must precede ALL template output, including banners.
  php = php.replace(/        if \( \$_POST\["action"\] === "download" \) \{[\s\S]*?\n        \};\n/, '')
    .replace('// Load the language support', () => String.raw`function backup_checked($cmd, $message) {
    exec($cmd . ' 2>&1', $unused, $rc);
    if ($rc !== 0) throw new RuntimeException($message);
}

if ($_SERVER['PHP_SELF'] === '/admin/config_backup.php' && ($_POST['action'] ?? '') === 'download') {
    $dir = '/tmp/config_backup';
    $zipPath = '/tmp/config_backup.zip';
    try {
        backup_checked('sudo rm -rf ' . escapeshellarg($zipPath), 'Cannot clear old backup.');
        backup_checked('sudo rm -rf ' . escapeshellarg($dir), 'Cannot clear backup directory.');
        backup_checked('sudo mkdir ' . escapeshellarg($dir), 'Cannot create backup directory.');
        $files = array();
        foreach (backup_files() as $name => $meta) {
            if (!file_exists($meta[0])) continue;
            if ($name === 'dhcpcd.conf') {
                $dhcp = file($meta[0]);
                if ($dhcp === false) throw new RuntimeException('Cannot read dhcpcd.conf.');
                if (!preg_grep('/^[^#\n]*static ip_address[^#\n]*$/', $dhcp)) continue;
            }
            $dst = escapeshellarg($dir . '/' . $name);
            backup_checked('sudo cp ' . escapeshellarg($meta[0]) . ' ' . $dst, 'Cannot back up ' . $name . '.');
            $files[$name] = $dst;
        }
        if (!$files) throw new RuntimeException('No configuration files to back up.');
        backup_checked('sudo zip -j ' . escapeshellarg($zipPath) . ' ' . implode(' ', $files), 'Cannot create backup ZIP.');
        $size = filesize($zipPath);
        if (!$size || $size > 256 * 1024) throw new RuntimeException('Backup ZIP outside restorable size range.');
        if (class_exists('ZipArchive')) {
            $check = new ZipArchive();
            if ($check->open($zipPath, ZipArchive::CHECKCONS) !== true) throw new RuntimeException('Cannot verify backup ZIP.');
            $complete = $check->numFiles === count($files);
            foreach ($files as $name => $_) $complete = $complete && $check->locateName($name) !== false;
            if (!$check->close() || !$complete) throw new RuntimeException('Backup ZIP is incomplete.');
        }
        $bytes = file_get_contents($zipPath);
        if ($bytes === false || strlen($bytes) !== $size) throw new RuntimeException('Cannot read complete backup ZIP.');
        backup_checked('sudo rm -rf ' . escapeshellarg($dir), 'Cannot clear backup directory.');
        backup_checked('sudo rm -rf ' . escapeshellarg($zipPath), 'Cannot clear backup ZIP.');
    } catch (RuntimeException $e) {
        shell_exec('sudo rm -rf ' . escapeshellarg($dir) . ' 2>&1');
        shell_exec('sudo rm -rf ' . escapeshellarg($zipPath) . ' 2>&1');
        http_response_code(500);
        header('Content-Type: text/plain; charset=UTF-8');
        exit($e->getMessage());
    }
    $host = preg_replace('/[^A-Za-z0-9.-]/', '', exec('cat /etc/hostname'));
    $prefix = ($host === '' || $host === 'pi-star') ? '' : $host . '_';
    header('Content-Type: application/zip');
    header('Content-Disposition: attachment; filename="Pi-Star_Config_' . $prefix . date('Y-M-d') . '.zip"');
    header('Cache-Control: no-store');
    header('Content-Length: ' . strlen($bytes));
    echo $bytes;
    exit;
}

// Load the language support`);

  // Keep the admission, extraction and per-target install paths verbatim.
  php = php.replace(/          shell_exec\("sudo rm -rf " \. escapeshellarg\(\$target_dir\)[\s\S]*?shell_exec\("mkdir -p "[^\n]*/, () => String.raw`          try {
              backup_checked('sudo rm -rf ' . escapeshellarg($target_dir), 'Cannot clear restore directory.');
              backup_checked('rm -f ' . escapeshellarg($upload_path), 'Cannot clear staged upload.');
              backup_checked('mkdir -m 700 ' . escapeshellarg($target_dir), 'Cannot create restore directory.');
          } catch (RuntimeException $e) {
              echo '<tr><td>' . htmlspecialchars($e->getMessage(), ENT_QUOTES, 'UTF-8') . '</td></tr>';
              break;
          }`)
    .replace('          $zip = new ZipArchive();', `          if (!class_exists('ZipArchive')) {
              @unlink($upload_path);
              echo '<tr><td>Cannot open backup: PHP ZIP support unavailable.</td></tr>';
              break;
          }
          $zip = new ZipArchive();`)
    .replace("if ($name === false || $name === '') {\n                  continue;", `if ($name === false || $name === '') {
                  $zip->close();
                  @unlink($upload_path);
                  echo '<tr><td>Cannot read a ZIP entry name.</td></tr>';
                  break 2;`)
    .replace('          $zip->close();\n          @unlink($upload_path);', `          $closed = $zip->close();
          if (!unlink($upload_path) || !$closed) {
              echo '<tr><td>Could not finish unpacking the staged upload.</td></tr>';
              break;
          }`)
    .replace("shell_exec('sudo mount -o remount,rw / 2>&1');", `$errors = array();
          exec('sudo mount -o remount,rw / 2>&1', $unused, $rc);
          if ($rc !== 0) $errors[] = 'Cannot remount root read-write.';`)
    .replace("shell_exec('sudo mount -o remount,ro / 2>&1');", `exec('sudo mount -o remount,ro / 2>&1', $unused, $rc);
          if ($rc !== 0) $errors[] = 'Cannot remount root read-only.';`)
    .replace(/          \/\/ Tear down stale dstar-radio[\s\S]*?(?=          \$installed = 0;)/, () => String.raw`          $rp_target = '/etc/ircddbgateway';
          $readPassword = function ($path) {
              if (!file_exists($path)) return null;
              $lines = file($path, FILE_IGNORE_NEW_LINES);
              if ($lines === false) return false;
              foreach ($lines as $line) {
                  if (strpos($line, 'remotePassword=') === 0) return substr($line, strlen('remotePassword='));
              }
              return null;
          };
          $previous = isset($admit['ircddbgateway']) ? $readPassword($rp_target) : null;
          $written = array();
`)
    .replace('          shell_exec("sudo rm -rf " . escapeshellarg($target_dir) . " 2>&1");', `          exec('sudo rm -rf ' . escapeshellarg($target_dir) . ' 2>&1', $unused, $rc);
          if ($rc !== 0) $errors[] = 'Cannot clear restore directory.';`)
    .replace('                  continue;\n              }\n              // Per-file', `                  $errors[] = 'Extracted file missing: ' . $basename;
                  continue;
              }
              // Per-file`)
    .replace('$installed++;', '$installed++;\n                  $written[$basename] = true;')
    .replace('$output .= "  install failed for $basename: "', '$errors[] = "install failed for $basename: "')
    .replace('          // ----- Post-restore', () => String.raw`          // Retire only the known opposite of a successfully installed, unambiguous mode.
          foreach (array('dstar-radio.mmdvmhost' => 'dstar-radio.dstarrepeater',
                        'dstar-radio.dstarrepeater' => 'dstar-radio.mmdvmhost') as $mode => $other) {
              if (!isset($written[$mode]) || isset($admit[$other])) continue;
              $inactive = $allowlist[$other][0];
              if (!file_exists($inactive) && !is_link($inactive)) continue;
              if (!is_file($inactive) || is_link($inactive)) {
                  $errors[] = 'Unexpected inactive marker type: ' . $other;
                  continue;
              }
              exec('sudo rm -rf ' . escapeshellarg($inactive) . ' 2>&1', $unused, $rc);
              if ($rc !== 0) $errors[] = 'Could not retire inactive marker: ' . $other;
          }

          // ----- Post-restore`)
    .replace('if (is_readable($cfg_path))', "if (isset($written['config.php']) && is_readable($cfg_path))")
    .replace(/shell_exec\('sudo timedatectl set-timezone '[\s\S]*?' 2>&1'\);/, `exec('sudo timedatectl set-timezone ' . escapeshellarg($tz) . ' 2>&1', $unused, $rc);
                      if ($rc !== 0) $errors[] = 'Could not apply timezone.';`)
    .replace(/          \$rp_target = '\/etc\/ircddbgateway';\n          if \(is_readable\(\$rp_target\)\) \{[\s\S]*?(?=          exec\('sudo mount)/, () => String.raw`          if (isset($written['ircddbgateway'])) {
              $rp = $readPassword($rp_target);
              if ($previous === false || $rp === false) {
                  $errors[] = 'Could not check gateway password for dependent sync.';
              } elseif ($rp !== $previous && is_string($rp) && $rp !== '' && !preg_match('/[\x00\r\n]/', $rp)) {
                  if (!config_writer_stage_privileged_flat('/root/.Remote Control', 'password', $rp)
                          || config_writer_commit(false)) {
                      $errors[] = 'Could not synchronize Remote Control password.';
                  }
              }
          }

`)
    .replace('$output .= "Configuration Restore Complete.\\n";', () => String.raw`$output .= !$errors ? "Configuration Restore Complete.\n"
                              : "Configuration restore incomplete:\n" . implode("\n", $errors) . "\n";`);
  writeFileSync(page, php);
}
