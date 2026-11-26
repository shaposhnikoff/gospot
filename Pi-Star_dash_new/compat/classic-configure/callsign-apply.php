<?php
// Included only by the packaged configure.php after its normal request gates.
// Paths, services and keys come from this table, never from a form field.
// Existing fields only: configure.php's defaults/migrations belong to full Apply.
function callsign_targets($call, $configs, $mmdvm, $ysf, $dstar, $release)
{
    $old = $configs['gatewayCallsign'];
    $module = $configs['repeaterBand1'] ?? substr($dstar['callsign'] ?? '', 7);
    if (file_exists('/etc/dstarrepeater') && !preg_match('/^[A-Z]$/D', $module))
        throw new RuntimeException('Cannot determine the current D-Star repeater module.', 409);
    $rpt = str_pad($call, 7).$module;
    $pass = (string)aprspass($call);
    // For descriptions and names preserve operator-written text. Only rename
    // the standard value actually derived from the old station identity.
    $name = array($old.'_Pi-Star', $call.'_Pi-Star');
    $legacyAprs = array('Password' => $pass, 'Description' => $name);
    $general = array('Callsign' => $call);
    $ysfNet = array('YSF Network' => $general, 'Info' => array('Description' => $name), 'aprs.fi' => $legacyAprs);
    $irc = array('gatewayCallsign' => array($old, $call), 'dplusLogin' => str_pad($call, 8),
        'repeaterCall1' => $call, 'aprsPassword' => $pass);
    if (strpos($configs['ircddbHostname'] ?? '', 'openquad.net') !== false)
        $irc['ircddbUsername'] = $call;
    $targets = array(
        '/etc/ircddbgateway' => array('' => $irc),
        '/etc/dstarrepeater' => array('' => array('callsign' => $rpt, 'gateway' => str_pad($call, 7).'G',
            'beaconText' => array(str_pad($old, 7).$module, $rpt))),
        '/etc/timeserver' => array('' => array('callsign' => $call)),
        '/etc/starnetserver' => array('' => array('callsign' => $call, 'ircddbUsername' => $call)),
        '/etc/mmdvmhost' => array('General' => $general),
        '/etc/ysfgateway' => array('General' => $general, 'Info' => array('Name' => $name), 'aprs.fi' => $legacyAprs),
        '/etc/ysf2dmr' => $ysfNet, '/etc/ysf2nxdn' => $ysfNet, '/etc/ysf2p25' => $ysfNet,
        '/etc/dmr2ysf' => array('YSF Network' => $general),
        '/etc/dgidgateway' => array('General' => $general, 'Info' => array('Description' => $name)),
        '/etc/p25gateway' => array('General' => $general),
        '/etc/nxdngateway' => array('General' => $general, 'aprs.fi' => $legacyAprs),
        '/etc/m17gateway' => array('General' => $general),
        '/var/www/dashboard/config/ircddblocal.php' => array(),
    );
    if (isset($ysf['General']['Suffix'])) {
        $suffix = '_'.$ysf['General']['Suffix'];
        foreach (array('/etc/ysfgateway', '/etc/nxdngateway') as $path)
            $targets[$path]['APRS']['Description'] = array($name[0].$suffix, $name[1].$suffix);
    }
    if (($release['Pi-Star']['Version'] ?? '') >= '4.1.4')
        $targets['/etc/aprsgateway'] = array('General' => $general, 'APRS-IS' => array('Password' => $pass));
    // Same auto/manual choice the stock radio renders, derived from disk.
    if (strpos($configs['url'] ?? '', 'www.qrz.com/db/'.$mmdvm['General']['Callsign']) !== false) {
        $url = 'https://www.qrz.com/db/'.$call;
        $targets['/etc/ircddbgateway']['']['url'] = $url;
        foreach (array('/etc/mmdvmhost', '/etc/ysf2dmr', '/etc/ysf2nxdn', '/etc/ysf2p25', '/etc/dmrgateway') as $path)
            $targets[$path]['Info']['URL'] = $url;
    }
    return $targets;
}

// Edit values in physical lines. Keep ordering, comments, quotes, line
// endings, duplicate unrelated keys and the exact final newline.
function callsign_rewrite($text, $path, $sections, $call)
{
    if ($path === '/var/www/dashboard/config/ircddblocal.php') {
        $next = preg_replace_callback('/^(\$callsign\h*=\h*\')[^\r\n\']*(\'\h*;)/m',
            function ($m) use ($call) { return $m[1].$call.$m[2]; }, $text, -1, $count);
        if ($count !== 1) throw new RuntimeException("Cannot identify the callsign in $path.", 409);
        return $next;
    }
    $seen = array();
    $section = '';
    $lines = preg_split('/(\r\n|\r|\n)/', $text, -1, PREG_SPLIT_DELIM_CAPTURE);
    for ($i = 0; $i < count($lines); $i += 2) {
        $line = $lines[$i];
        if (preg_match('/^\h*\[([^]]+)\]/', $line, $m)) {
            $section = $m[1];
            continue;
        }
        if (!preg_match('/^(\h*([a-zA-Z0-9_]+)\h*=\h*)(.*)$/D', $line, $m)) continue;
        list(, $prefix, $key, $rhs) = $m;
        if (!isset($sections[$section][$key])) continue;
        if (isset($seen[$section][$key]) ||
            !preg_match('~^("([^"]*)"|\'([^\']*)\'|([^"\'#;]*?))(\h*(?:[#;].*)?)$~D', $rhs, $v))
            throw new RuntimeException("Ambiguous $key in $path; review before saving.", 409);
        $seen[$section][$key] = true;
        $value = $sections[$section][$key];
        $token = $v[1];
        $quote = substr($token, 0, 1);
        if ($quote !== '"' && $quote !== "'") $quote = '';
        if (is_array($value)) {
            if (($quote ? substr($token, 1, -1) : $token) !== $value[0]) {
                if ($key === 'gatewayCallsign') throw new RuntimeException('Station identity changed; reload before saving.', 409);
                if ($key === 'ColorCode') throw new RuntimeException('DMR Color Code changed; reload before saving.', 409);
                continue;
            }
            $value = $value[1];
        }
        // Unquoted dplusLogin's old right-padding is the value, not a
        // separator. Keep exactly eight characters even after a shorter call.
        $tail = $v[5];
        if ($key === 'dplusLogin' && !$quote) {
            $prefix = rtrim($prefix, " \t");
            if (trim($tail) === '') $tail = '';
        }
        $lines[$i] = $prefix.$quote.$value.$quote.$tail;
    }
    $required = array(
        '/etc/ircddbgateway' => array('' => array('gatewayCallsign')),
        '/etc/mmdvmhost' => isset($sections['General']['Callsign']) ? array('General' => array('Callsign')) : array(),
        '/etc/dstarrepeater' => array('' => array('callsign', 'gateway')),
        '/etc/aprsgateway' => array('General' => array('Callsign'), 'APRS-IS' => array('Password')),
    );
    if ($path === '/etc/mmdvmhost' && isset($sections['DMR']['ColorCode']))
        $required[$path]['DMR'] = array('ColorCode');
    foreach (($required[$path] ?? array()) as $scope => $keys)
        foreach ($keys as $key) if (!isset($seen[$scope][$key]))
            throw new RuntimeException("Missing $key in $path; review before saving.", 409);
    return implode('', $lines);
}

function callsign_command($command)
{
    exec($command, $unused, $status);
    return $status;
}

function callsign_stage($text, &$temps, $prefix='pistar-cfg-')
{
    $tmp = tempnam('/tmp', $prefix);
    if ($tmp === false) throw new RuntimeException('Cannot create staging file.', 500);
    $temps[] = $tmp;
    if (file_put_contents($tmp, $text) !== strlen($text))
        throw new RuntimeException('Cannot write staging file.', 500);
    return $tmp;
}

function callsign_install($from, $to)
{
    return callsign_command('sudo install -m 644 -o root -g root '.escapeshellarg($from).' '.escapeshellarg($to)) === 0;
}

function callsign_apply($post, $previous, $configs, $mmdvm, $ysf, $dstar, $release, $previousColor = null)
{
    // csrf_verify() has already checked and removed the native csrf_token.
    if ($previous !== null && (!is_string($previous) || !isset($post['confCallsign'])
        || !is_string($post['confCallsign']) || !preg_match('/^[a-z0-9]{1,7}$/iD', $post['confCallsign'])))
        throw new RuntimeException('Expected a callsign of 1–7 letters or digits.', 400);
    if (!is_array($mmdvm) || count($mmdvm) <= 18 || !isset($mmdvm['General']['Callsign']))
        throw new RuntimeException('Unable to read source configuration file(s).', 409);
    if ($previous !== null && ($configs['gatewayCallsign'] ?? null) !== $previous)
        throw new RuntimeException('Station identity changed; reload before saving.', 409);
    $call = $previous !== null ? strtoupper($post['confCallsign']) : null;
    $targets = $call !== null ? callsign_targets($call, $configs, $mmdvm, $ysf, $dstar, $release) : array();
    if ($previousColor !== null) {
        $color = $post['dmrColorCode'] ?? null;
        foreach (array($previousColor, $color) as $code)
            if (!is_string($code) || !preg_match('/^(?:[0-9]|1[0-5])$/D', $code))
                throw new RuntimeException('Expected a DMR Color Code between 0 and 15.', 400);
        $targets['/etc/mmdvmhost']['DMR']['ColorCode'] = array($previousColor, $color);
    }
    classic_commit($targets, function ($text, $path, $sections) use ($call) {
        return callsign_rewrite($text, $path, $sections, $call);
    }, $call !== null ? 'Callsign' : 'Color Code');
}

// Both scoped edits and a regular, multi-field Apply share the same file lock,
// rollback, mount boundary and active-consumer restart policy.
function classic_commit($targets, $rewrite, $label, $zone=null)
{
    $lock = fopen(__FILE__, 'r');
    if (!$lock || !flock($lock, LOCK_EX | LOCK_NB))
        throw new RuntimeException('Another save is in progress; reload and retry.', 409);
    $temps = $changes = $active = array();
    try {
        $oldZone = null;
        foreach ($targets as $path => $sections) {
            if (!file_exists($path) && !is_link($path) && !in_array($path, array(
                '/etc/ircddbgateway', '/etc/mmdvmhost', '/etc/dstar-radio.mmdvmhost', '/var/www/dashboard/config/ircddblocal.php',
                '/etc/timezone','/var/www/dashboard/config/config.php','/var/www/dashboard/config/language.php'), true)) continue;
            if (!is_file($path) || realpath($path) !== $path || ($text = @file_get_contents($path)) === false)
                throw new RuntimeException("Cannot read $path safely.", 500);
            $next = $rewrite($text, $path, $sections);
            if ($path === '/etc/timezone') {
                $oldZone = trim($text);
                if (!in_array($oldZone,DateTimeZone::listIdentifiers(DateTimeZone::ALL_WITH_BC),true)
                    || !is_file('/etc/localtime') || hash_file('sha256','/etc/localtime') !==
                       hash_file('sha256','/usr/share/zoneinfo/'.$oldZone))
                    throw new RuntimeException('System timezone changed; reload before saving.',409);
            }
            if ($text === $next) continue;
            // The stock installer publishes these configs as 0644. Do not
            // widen read access on a node that has deliberately restricted one.
            if ((fileperms($path) & 0444) !== 0444)
                throw new RuntimeException("Cannot install $path without changing its read permissions.", 409);
            $prefix = $path === '/etc/timezone' ? 'pistar_tz_' :
                ($path === '/var/www/dashboard/config/config.php' ? 'pistar_cfg_' : 'pistar-cfg-');
            if ($path === '/var/www/dashboard/config/language.php') $prefix = 'pistar_cw_';
            $changes[$path] = array(callsign_stage($text,$temps,$prefix), callsign_stage($next,$temps,$prefix));
            if (dirname($path) !== '/etc' || $path === '/etc/timezone') continue;
            $unit = $path === '/etc/dstar-radio.mmdvmhost' ? 'mmdvmhost.service' : basename($path).'.service';
            if (isset($checked[$unit])) continue;
            $checked[$unit] = true;
            $status = callsign_command('systemctl is-active --quiet '.escapeshellarg($unit));
            if ($status === 0) $active[] = $unit;
            elseif ($status !== 3 && $status !== 4)
                throw new RuntimeException("Cannot check $unit; no files installed.", 500);
        }
        if (!$changes) return;
        if (callsign_command('sudo mount -o remount,rw /') !== 0)
            throw new RuntimeException('Cannot remount for configuration update.', 500);
        $attempted = array();
        $failure = '';
        $zoneAttempted = false;
        try {
            if ($zone !== null && $zone !== $oldZone) {
                // Some timedated versions also update /etc/timezone.
                if (file_get_contents('/etc/timezone') !== file_get_contents($changes['/etc/timezone'][0]))
                    throw new RuntimeException('System timezone changed during save.',409);
                $zoneAttempted = true;
                if (callsign_command('sudo timedatectl set-timezone '.escapeshellarg($zone)) !== 0)
                    throw new RuntimeException('Cannot set the system timezone.',500);
            }
            foreach ($changes as $path => $pair) {
                $current = file_get_contents($path);
                if ($current !== file_get_contents($pair[0]) &&
                    !($path === '/etc/timezone' && $zoneAttempted && $current === file_get_contents($pair[1])))
                    throw new RuntimeException("$path changed during save.", 409);
                $attempted[] = $path;
                if (!callsign_install($pair[1], $path))
                    throw new RuntimeException("Cannot install $path.", 500);
            }
        } catch (RuntimeException $error) {
            $failure = $error->getMessage();
            if ($zoneAttempted) {
                if (callsign_command('sudo timedatectl set-timezone '.escapeshellarg($oldZone)) !== 0)
                    $failure .= ' Could not restore the system timezone.';
                if (!in_array('/etc/timezone',$attempted,true)) $attempted[] = '/etc/timezone';
            }
            foreach (array_reverse($attempted) as $path) {
                $original = $changes[$path][0];
                if (@file_get_contents($path) !== file_get_contents($original) && !callsign_install($original, $path))
                    $failure .= " Could not restore $path.";
            }
        } finally {
            if (callsign_command('sudo sync') !== 0) $failure .= ' Filesystem sync failed.';
            if (callsign_command('sudo mount -o remount,ro /') !== 0) $failure .= ' Could not restore read-only mount.';
        }
        if ($failure !== '') throw new RuntimeException(trim($failure).' Services were not restarted.', 500);
        // The saved set is consistent. Report activation failures separately;
        // reverting files now could disagree with daemons already restarted.
        $failed = array();
        foreach ($active as $unit)
            if (callsign_command('sudo systemctl restart '.escapeshellarg($unit)) !== 0) $failed[] = $unit;
        if ($failed) throw new RuntimeException($label.' saved; restart failed for '.implode(', ', $failed).'.', 500);
    } finally {
        foreach ($temps as $tmp) @unlink($tmp);
        fclose($lock);
    }
}
