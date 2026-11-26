<?php
// Normal Apply receives the entire stock form. A signed before-image describes
// which values the operator actually saw, including defaults chosen by selects.
// Only keyed fingerprints cross the wire, never another copy of any password.
require_once __DIR__.'/callsign-apply.php';
const CLASSIC_LANGUAGE_FILE = '/var/www/dashboard/config/language.php';
function apply_auto_ap_psk($post)
{
    $key = $post['autoapPsk'] ?? null;
    if (!is_string($key) || !preg_match('/\A([\x20-\x7e]{8,63}|[0-9A-Fa-f]{64})\z/', $key)) {
        error_log('Pi-Star configure.php: AutoAP PSK rejected (must be 8-63 printable ASCII or 64-hex)');
        throw new RuntimeException('Invalid Auto AP key length or characters.',422);
    }
    if (count($post) !== 2 || ($post['autoapPskConfirm'] ?? null) !== $key)
        throw new RuntimeException('Auto AP key confirmation does not match.',422);

    $lock = fopen(__DIR__.'/callsign-apply.php','r');
    if (!$lock || !flock($lock, LOCK_EX | LOCK_NB)) throw new RuntimeException('Another save is in progress.',409);
    try {
        $path = '/etc/hostapd/hostapd.conf';
        if (!config_writer_stage_privileged_flat($path,'wpa_passphrase',$key) ||
            !config_writer_stage_privileged_flat($path,'wpa','2'))
            throw new RuntimeException('Cannot stage the Auto AP configuration.',500);
        if (callsign_command('sudo mount -o remount,rw /') !== 0)
            throw new RuntimeException('Cannot remount for Auto AP configuration.',500);
        try {
            if (config_writer_commit(false)) throw new RuntimeException('Cannot save the Auto AP configuration.',500);
        } finally {
            if (callsign_command('sudo mount -o remount,ro /') !== 0)
                throw new RuntimeException('Cannot restore read-only mount after Auto AP update.',500);
        }
    } finally {
        fclose($lock);
    }
}
function apply_model_names()
{
    return array('mmdvmhost'=>'configmmdvm', 'ysfgateway'=>'configysfgateway', 'dmrgateway'=>'configdmrgateway',
        'ysf2dmr'=>'configysf2dmr', 'ysf2p25'=>'configysf2p25', 'ysf2nxdn'=>'configysf2nxdn',
        'dmr2ysf'=>'configdmr2ysf', 'dmr2nxdn'=>'configdmr2nxdn','nxdngateway'=>'confignxdngateway',
        'm17gateway'=>'configm17gateway','p25gateway'=>'configp25gateway','dgidgateway'=>'configdgidgateway',
        'dapnetgateway'=>'configdapnetgw');
}
function apply_paths()
{
    return array_unique(array_merge(array_map(function ($n) { return '/etc/'.$n; }, array_keys(apply_model_names())),
        config_writer_allowed_paths(), array('/etc/timezone','/etc/localtime','/var/www/dashboard/config/config.php',CLASSIC_LANGUAGE_FILE,
            '/etc/dstar-radio.mmdvmhost','/etc/dstar-radio.dstarrepeater')));
}

// Both files are in /etc. Move the existing inode; never reconstruct the
// modem configuration, expand a wildcard, or overwrite an ambiguous marker.
function apply_move_marker($from,$to)
{
    if (callsign_command('sudo mv -nT -- '.escapeshellarg($from).' '.escapeshellarg($to)) !== 0) return false;
    clearstatcache(true);
    return !file_exists($from) && !is_link($from) && is_file($to) && !is_link($to);
}

function apply_controller_switch($mode,$hashes)
{
    $units = array('DSTAR'=>'dstarrepeater','MMDVM'=>'mmdvmhost');
    if (!isset($units[$mode])) throw new RuntimeException('Unknown radio controller.',400);
    $new = $units[$mode]; $old = $units[$mode === 'DSTAR' ? 'MMDVM' : 'DSTAR'];
    $from = '/etc/dstar-radio.'.$old; $to = '/etc/dstar-radio.'.$new;
    $lock = fopen(__DIR__.'/callsign-apply.php','r'); // Same lock as classic_commit.
    if (!$lock || !flock($lock,LOCK_EX | LOCK_NB)) throw new RuntimeException('Another save is in progress.',409);
    try {
        clearstatcache(true);
        $first = !isset($hashes[$from]) && !isset($hashes[$to]);
        $sourceMatches = $first ? (!file_exists($from) && !is_link($from)) :
            (isset($hashes[$from]) && realpath($from)===$from && is_file($from) && hash_file('sha256',$from)===$hashes[$from]);
        if (!$sourceMatches || isset($hashes[$to]) || file_exists($to) || is_link($to))
            throw new RuntimeException('Controller markers changed or are ambiguous. Reload before switching.',409);
        $status = callsign_command("systemctl is-active --quiet $old.service");
        if ($status !== 0 && $status !== 3) throw new RuntimeException("Cannot determine state of $old.",500);
        if ($first && $status === 0) throw new RuntimeException("Unexpected active controller: $old.",409);
        if (callsign_command("systemctl is-active --quiet $new.service") !== 3)
            throw new RuntimeException("Target controller $new is not inactive.",409);
        if (callsign_command('sudo mount -o remount,rw /') !== 0)
            throw new RuntimeException('Cannot remount for controller switch.',500);
        $stopped = $moved = $created = $starting = false; $error = '';
        try {
            if ($status === 0) {
                if (callsign_command("sudo systemctl stop $old.service") !== 0)
                    throw new RuntimeException("Cannot stop $old.");
                $stopped = true;
            }
            if ($first) {
                if (callsign_command('sudo touch '.escapeshellarg($to)) !== 0)
                    throw new RuntimeException('Cannot create initial controller marker.');
                $created = true;
            } else {
                if (!apply_move_marker($from,$to)) throw new RuntimeException('Cannot move controller marker.');
                $moved = true;
            }
            if (callsign_command('sudo sync') !== 0) throw new RuntimeException('Cannot sync controller marker.');
            $starting = true;
            if (callsign_command("sudo systemctl start $new.service") !== 0)
                throw new RuntimeException("Cannot start $new.");
        } catch (RuntimeException $e) {
            $error = $e->getMessage();
            // Never reactivate the old controller under the new marker, or
            // while a partially started new process might still own the modem.
            if ($starting && callsign_command("sudo systemctl stop $new.service") !== 0)
                $error .= " Could not stop $new for rollback.";
            elseif ($moved && !apply_move_marker($to,$from))
                $error .= ' Could not restore controller marker.';
            elseif ($created && callsign_command('sudo rm -f -- '.escapeshellarg($to)) !== 0)
                $error .= ' Could not remove initial controller marker.';
            else {
                if (($moved || $created) && callsign_command('sudo sync') !== 0) $error .= ' Rollback sync failed.';
                if ($stopped && is_file($from) && !file_exists($to) && !is_link($to) &&
                    callsign_command("sudo systemctl start $old.service") !== 0) $error .= " Could not restore $old.";
            }
        } finally {
            if (callsign_command('sudo mount -o remount,ro /') !== 0) $error .= ' Could not restore read-only mount.';
        }
        if ($error !== '') throw new RuntimeException(trim($error),500);
    } finally {
        fclose($lock);
    }
}
function apply_models()
{
    $models = array();
    foreach (apply_model_names() as $name=>$var) {
        if (!file_exists('/etc/'.$name)) continue;
        $models['/etc/'.$name] = $GLOBALS[$var] ?? false;
    }
    return $models;
}

function apply_commit_models($before, $after, $hashes, $zone=null, $language=null, $modem=null)
{
    $changes = array();
    foreach ($before as $path=>$old) {
        $new = $after[$path];
        if ($new === $old) continue;
        if (!is_array($old) || !is_array($new)) throw new RuntimeException("Unreadable $path.",409);
        $delta = apply_ini_delta($old,$new);
        if ($delta) $changes[$path] = $delta;
    }
    foreach (($GLOBALS['__config_writer_stage'] ?? array()) as $path=>$pairs) {
        if (!in_array($path,config_writer_allowed_paths(),true)) throw new RuntimeException('Unknown flat file.',500);
        $changes[$path][''] = $pairs;
    }
    if ($zone !== null) {
        if (!in_array($zone,DateTimeZone::listIdentifiers(),true))
            throw new RuntimeException('Unknown system timezone.',400);
        $changes['/etc/timezone'] = array();
        $changes['/var/www/dashboard/config/config.php'] = array();
    }
    if ($language !== null) {
        // Use only the value accepted and staged by the stock translation-file rule.
        if (($GLOBALS['__config_writer_stage_phpstr'][CLASSIC_LANGUAGE_FILE]['pistarLanguage'] ?? null) !== $language
            || !is_string($language) || !preg_match('/\A[A-Za-z][A-Za-z0-9_]+\z/', $language))
            throw new RuntimeException('Unknown dashboard language.',400);
        $changes[CLASSIC_LANGUAGE_FILE] = array();
    }
    if ($modem !== null) $changes['/etc/dstar-radio.mmdvmhost'] = array('Modem'=>array('Hardware'=>$modem));
    classic_commit($changes, function ($bytes,$path,$sections) use ($hashes,$zone,$language,$modem) {
        if (($hashes[$path] ?? null) !== hash('sha256',$bytes))
            throw new RuntimeException("$path changed; reload before saving.",409);
        if ($path === '/etc/dstar-radio.mmdvmhost') {
            if ($modem !== 'zumspotusb' || file_exists('/etc/dstar-radio.dstarrepeater') || is_link('/etc/dstar-radio.dstarrepeater'))
                throw new RuntimeException('Invalid modem or ambiguous controller markers.',409);
            if ($bytes === '') return "[Modem]\nHardware=$modem\n\n";
        }
        if ($path === CLASSIC_LANGUAGE_FILE) {
            $next = preg_replace_callback('~^(\h*\$pistarLanguage\h*=\h*)([\'"])[A-Za-z][A-Za-z0-9_]*\2(?=\h*;)~m',
                function ($m) use ($language) { return $m[1].$m[2].$language.$m[2]; },$bytes,-1,$count);
            if ($count !== 1) throw new RuntimeException('Cannot identify the dashboard language setting.',409);
            return $next;
        }
        if ($path === '/etc/timezone') return $zone."\n";
        if ($path === '/var/www/dashboard/config/config.php') {
            // Replace only the literal argument of the stock statement. Keep
            // indentation, quote style, comments and line endings intact.
            $next = preg_replace_callback('~^(\h*date_default_timezone_set\h*\(\h*)([\'"])[A-Za-z0-9_+./-]+\2(\h*\)\h*;)~m',
                function ($m) use ($zone) { return $m[1].$m[2].$zone.$m[2].$m[3]; },$bytes,-1,$count);
            if ($count !== 1) throw new RuntimeException('Cannot identify the dashboard timezone setting.',409);
            return $next;
        }
        return apply_lines($bytes,$path,$sections,in_array($path,config_writer_allowed_paths(),true));
    }, 'Configuration', $zone);
}

function apply_ini_delta($old,$new)
{
    $delta = array();
    foreach (array_unique(array_merge(array_keys($old),array_keys($new))) as $section) {
        $a = $old[$section] ?? array(); $b = $new[$section] ?? array();
        if (!is_array($a) || !is_array($b)) throw new RuntimeException('Expected a sectioned daemon INI.',409);
        foreach (array_unique(array_merge(array_keys($a),array_keys($b))) as $k) {
            $v = $b[$k] ?? null;
            if (($a[$k] ?? null) !== $v) $delta[$section][$k] = $v;
        }
    }
    return $delta;
}

// Also protect the stock provisioning writers (e.g. a modem switch). Keep
// their admission/line-count checks and install permissions; reconcile their
// normalized output with the physical source instead of installing it whole.
function apply_publish_ini($tmp, $path)
{
    $candidate = file_get_contents($tmp);
    if ($candidate === false) throw new RuntimeException("Unreadable candidate for $path.",500);
    if (file_exists($path)) {
        if (realpath($path) !== $path || !is_file($path)) throw new RuntimeException("Invalid $path.",409);
        $old = file_get_contents($path);
        $a = $old === false ? false : parse_ini_string($old,true);
        $b = parse_ini_string($candidate,true);
        if (!is_array($a) || !is_array($b)) throw new RuntimeException("Invalid INI for $path.",409);
        $delta = apply_ini_delta($a,$b);
        if (!$delta) return;
        // Keep the candidate's literal quoting for *changed* keys.
        $scope = '';
        foreach (preg_split('/\r\n|\n|\r/', $candidate) as $line) {
            if (preg_match('/^\[([^\]]+)\]/D',$line,$m)) $scope=$m[1];
            elseif (preg_match('/^([\w.-]+)=(.*)$/D',$line,$m) && isset($delta[$scope][$m[1]]))
                $delta[$scope][$m[1]]=$m[2];
        }
        $next = apply_lines($old,$path,$delta);
        if ($next === $old) return;
        if (file_put_contents($tmp,$next) !== strlen($next)) throw new RuntimeException("Cannot stage $path.",500);
    }
    if (!callsign_install($tmp,$path)) throw new RuntimeException("Cannot install $path.",500);
}

function apply_stamp($html, $paths)
{
    $doc = new DOMDocument();
    $doc->loadHTML('<meta charset="UTF-8">'.$html.'</form>', LIBXML_NONET | LIBXML_NOERROR | LIBXML_NOWARNING);
    $xp = new DOMXPath($doc);
    $fields = array();
    foreach ($xp->query('//form[@id="config"]//*[@name and not(@disabled)]') as $el) {
        $key = $el->getAttribute('name');
        if ($key === 'csrf_token') continue;
        if ($el->tagName === 'input') {
            $type = strtolower($el->getAttribute('type'));
            if (in_array($type, array('button','submit','reset'), true)) continue;
            if (in_array($type, array('radio','checkbox'), true) && !$el->hasAttribute('checked')) continue;
            $v = $el->getAttribute('value');
        } elseif ($el->tagName === 'select') {
            $opts = $el->getElementsByTagName('option');
            if (!$opts->length) continue;
            $chosen = $opts->item(0);
            foreach ($opts as $opt) if ($opt->hasAttribute('selected')) $chosen = $opt;
            if ($chosen->hasAttribute('disabled')) continue;
            $v = $chosen->hasAttribute('value') ? $chosen->getAttribute('value') : trim($chosen->textContent);
        } elseif ($el->tagName === 'textarea') {
            $v = $el->textContent;
        } else continue;
        $fields[$key] = preg_replace("/\r\n|\r|\n/", "\r\n", $v);
    }
    if (!isset($_SESSION['apply_key'])) $_SESSION['apply_key'] = random_bytes(32);
    $key = $_SESSION['apply_key'];
    $nonce = bin2hex(random_bytes(12));
    foreach ($fields as $k => $v) $fields[$k] = hash_hmac('sha256', $nonce.$k."\0".$v, $key);
    $files = array();
    foreach ($paths as $p) if (is_readable($p) && is_file($p)) $files[$p] = hash_file('sha256', $p);
    // The session holds just the signing key; separate browser tabs need no shared cursor.
    $payload = base64_encode(json_encode(array($nonce, $fields, $files)));
    return $payload.'.'.hash_hmac('sha256', $payload, $key);
}

function apply_changes($post)
{
    $key = $_SESSION['apply_key'] ?? null;
    $parts = is_string($post['apply_snapshot'] ?? null) ? explode('.', $post['apply_snapshot'], 2) : array();
    if (!$key || count($parts) !== 2 || !hash_equals(hash_hmac('sha256', $parts[0], $key), $parts[1]))
        throw new RuntimeException('Configuration form expired. Reload before saving.', 409);
    list($nonce, $before, $files) = json_decode(base64_decode($parts[0]), true);
    foreach ($files as $path => $sum) {
        if (!is_file($path) || !is_readable($path) || hash_file('sha256',$path) !== $sum)
            throw new RuntimeException('Station configuration changed. Reload before saving.',409);
    }
    unset($post['apply_snapshot'], $post['csrf_token']);
    $dirty = array();
    foreach ($post as $name => $value) {
        if (!is_string($value))
            throw new RuntimeException('Invalid configuration field.', 400);
        if (!isset($before[$name]) || !hash_equals($before[$name], hash_hmac('sha256', $nonce.$name."\0".$value, $key))) {
            if (strpbrk($value, "\r\n\0") !== false) throw new RuntimeException('Invalid configuration field.',400);
            $dirty[$name] = $value;
        }
    }
    foreach (array_diff_key($before, $post) as $name => $_) $dirty[$name] = '';
    return array($dirty, $files);
}

// Apply a semantic delta to physical lines. Unchanged (section,key) pairs
// never enter the edit set, so their comments, duplicates, quoting and CRLF
// survive even though PHP's INI parser cannot represent them.
function apply_lines($text, $path, $changes, $flat=false)
{
    $parts = preg_split('/(\r\n|\n|\r)/', $text, -1, PREG_SPLIT_DELIM_CAPTURE);
    $eol = $parts[1] ?? "\n";
    $section = '';
    $seen = $sections = array();
    for ($i=0; $i<count($parts); $i+=2) {
        if (!$flat && preg_match('/^\h*\[([^\]]+)\]/', $parts[$i], $m)) {
            $section = $m[1];
        } elseif (preg_match('/^(\h*([\w.-]+)\h*=\h*)(.*)$/D', $parts[$i], $m)
                  && array_key_exists($m[2], $changes[$section] ?? array())) {
            $k = $m[2];
            if (isset($seen[$section][$k])) throw new RuntimeException("Ambiguous $section/$k in $path.", 409);
            $seen[$section][$k] = true;
            $value = $changes[$section][$k];
            if ($value === null) {
                $parts[$i] = ''; if (isset($parts[$i+1])) $parts[$i+1] = '';
            } else {
                if (!preg_match('~^("[^"]*"|\'[^\']*\'|[^#;]*?)(\h*(?:[#;].*)?)$~D', $m[3], $v))
                    throw new RuntimeException("Cannot edit $section/$k in $path.", 409);
                $parts[$i] = $m[1].$value.$v[2];
            }
        }
        $sections[$section] = $i;
    }
    foreach ($changes as $scope => $kv) {
        $add = array();
        foreach ($kv as $k => $v) if (!isset($seen[$scope][$k]) && $v !== null) $add[] = "$k=$v";
        if (!$add) continue;
        if ($flat) throw new RuntimeException("Missing flat configuration key in $path.",409);
        $pos = $sections[$scope] ?? null;
        if ($pos === null) { $parts[] = $eol."[$scope]".$eol.implode($eol,$add).$eol; continue; }
        // Append within the correct section without shifting offsets for later sections.
        $parts[$pos] .= $eol.implode($eol,$add);
    }
    return implode('', $parts);
}
