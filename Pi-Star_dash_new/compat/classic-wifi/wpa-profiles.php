<?php
// The classic form owns country and the SSID/PSK of simple personal/open
// profiles. Keep the original lines, including profiles the form cannot edit.
function classic_wifi_text($s, $min, $max) {
    return is_string($s) && strlen($s) >= $min && strlen($s) <= $max
        && preg_match('/\A[\x20-\x21\x23-\x5b\x5d-\x7e]*\z/', $s);
}

function classic_wifi_raw_key($s) {
    return is_string($s) && preg_match('/\A[0-9a-fA-F]{64}\z/', $s);
}

function classic_wifi_quoted($s) {
    return preg_match('/^"([^"]*)"$/D', $s, $m) ? $m[1] : null;
}

function classic_wifi_profile($lines, $start, $end) {
    $values = $owned = [];
    for ($i = $start + 1; $i < $end; $i++) {
        $line = trim($lines[$i]);
        if ($line === '') continue;
        if (!preg_match('/^(#?[\w-]+)\s*=\s*(.*)$/D', $line, $m)) {
            if ($line[0] === '#') continue;
            return null;
        }
        $k = $m[1];
        if ($k[0] === '#' && !in_array($k, ['#ssid', '#psk'], true)) continue;
        // Other authentication modes (EAP, SAE, WEP, etc.) and unfamiliar
        // options stay wholly opaque. Never infer "open" from an absent PSK.
        if (!in_array($k, ['#ssid', 'ssid', '#psk', 'psk', 'key_mgmt',
                'priority', 'bssid', 'scan_ssid', 'id_str', 'disabled',
                'scan_freq', 'freq_list', 'proto', 'pairwise', 'group'], true)
                || isset($values[$k])) return null;
        $values[$k] = $m[2];
        if (in_array($k, ['#ssid', 'ssid', '#psk', 'psk', 'key_mgmt'], true)) $owned[] = $i;
    }
    $mode = $values['key_mgmt'] ?? null;
    if ($mode === 'NONE' && !isset($values['psk'])) {
        $psk = '';
    } elseif (($mode === null || $mode === 'WPA-PSK') && isset($values['psk'])) {
        $psk = $values['psk'];
        if (!classic_wifi_raw_key($psk)) {
            $psk = classic_wifi_quoted($psk);
            if (!classic_wifi_text($psk, 8, 63)) return null;
        }
    } else {
        return null;
    }
    $v = $values['ssid'] ?? '';
    $ssid = classic_wifi_quoted($v);
    if ($ssid === null && preg_match('/\A(?:[0-9a-fA-F]{2}){1,32}\z/', $v)) $ssid = hex2bin($v);
    // A wildcard open profile in the classic format has only a #ssid comment.
    if (!isset($values['ssid']) && $psk === '' && ($values['#ssid'] ?? '') === '"*"') $ssid = '*';
    elseif ($ssid === '*' && $psk === '') return null; // literal SSID, not the classic wildcard
    if (!classic_wifi_text($ssid, 1, 32)) return null;

    // Only trust a plaintext hint when it matches the effective raw key.
    $hint = classic_wifi_quoted($values['#psk'] ?? '');
    if (classic_wifi_raw_key($psk) && classic_wifi_text($hint, 8, 63)
            && hash_pbkdf2('sha1', $hint, $ssid, 4096, 64) === strtolower($psk)) $psk = $hint;
    return compact('start', 'end', 'ssid', 'psk', 'owned', 'values');
}

function classic_wifi_parse($config) {
    if (strpos($config, "\0") !== false || preg_match('/\r(?!\n)/', $config))
        throw new RuntimeException('Unsupported configuration encoding.');
    $lines = explode("\n", $config);
    $country = $countryLine = $start = null;
    $profiles = [];
    $opaque = 0;
    foreach ($lines as $i => $raw) {
        $line = trim($raw);
        if ($line === '' || $line[0] === '#') continue;
        if ($start !== null && preg_match('/^}\s*(?:#.*)?$/D', $line)) {
            $p = classic_wifi_profile($lines, $start, $i);
            if ($p === null) $opaque++;
            else $profiles[] = $p;
            $start = null;
        } elseif ($start === null && preg_match('/^network=\{\s*(?:#.*)?$/D', $line)) {
            $start = $i;
        } elseif (!preg_match('/^([\w-]+)\s*=\s*(.*)$/D', $line, $m)
                || $m[1] === 'network' || preg_match('/^\{\s*(?:#.*)?$/D', $m[2])) {
            throw new RuntimeException('Unsupported configuration structure.');
        } elseif ($start === null && $m[1] === 'country') {
            if ($countryLine !== null || !preg_match('/^([A-Z]{2})\s*(?:#.*)?$/D', $m[2], $cc))
                throw new RuntimeException('Unsupported country setting.');
            $country = $cc[1];
            $countryLine = $i;
        }
    }
    if ($start !== null) throw new RuntimeException('Unterminated network block.');
    $revision = hash('sha256', $config);
    return compact('lines', 'profiles', 'country', 'countryLine', 'opaque', 'revision');
}

function classic_wifi_settings($ssid, $psk, $explicitWpa) {
    if ($psk === '') {
        return ["\t" . ($ssid === '*' ? '#ssid' : 'ssid') . "=\"$ssid\"", "\t#psk=\"\"", "\tkey_mgmt=NONE"];
    }
    $fields = ["\t#ssid=\"$ssid\"", "\tssid=" . bin2hex($ssid)];
    if (!classic_wifi_raw_key($psk)) {
        $fields[] = "\t#psk=\"$psk\"";
        $psk = hash_pbkdf2('sha1', $psk, $ssid, 4096, 64);
    }
    $fields[] = "\tpsk=$psk";
    if ($explicitWpa) $fields[] = "\tkey_mgmt=WPA-PSK";
    return $fields;
}

function classic_wifi_save($doc, $post) {
    if (($post['wifiRevision'] ?? null) !== $doc['revision'])
        throw new RuntimeException('Configuration changed. Reload before saving.');
    $country = $post['wifiCountryCode'] ?? null;
    if (!is_string($country) || !preg_match('/\A[A-Z]{2}\z/', $country))
        throw new RuntimeException('Invalid country code.');
    // The numeric suffix is stable for the lifetime of this form. Deleting a
    // row leaves a gap; new rows always start after the original visible rows.
    $rows = [];
    foreach ($post as $key => $value) {
        if (!preg_match('/^(ssid|psk)(.*)$/D', $key, $m)) continue;
        if (!preg_match('/^(?:0|[1-9]\d{0,8})$/D', $m[2])) throw new RuntimeException('Invalid row index.');
        $rows[$m[2]][$m[1]] = $value;
    }
    if (count($rows) > 32 || ($post['Networks'] ?? null) !== (string)count($rows))
        throw new RuntimeException('Incomplete form or too many networks.');
    foreach ($rows as $row) {
        if (!classic_wifi_text($row['ssid'] ?? null, 1, 32)
                || !(($row['psk'] ?? null) === '' || classic_wifi_text($row['psk'] ?? null, 8, 63)
                || classic_wifi_raw_key($row['psk'] ?? null))) throw new RuntimeException('Invalid SSID or PSK.');
    }
    $lines = $doc['lines'];
    if ($country !== $doc['country'] && $doc['countryLine'] !== null) {
        $i = $doc['countryLine'];
        $lines[$i] = preg_replace('/^(\s*country\s*=\s*)[A-Z]{2}/', '${1}' . $country, $lines[$i]);
    }
    foreach ($doc['profiles'] as $id => $p) {
        if (!isset($rows[$id])) {
            for ($i = $p['start']; $i <= $p['end']; $i++) unset($lines[$i]);
            continue;
        }
        ['ssid' => $ssid, 'psk' => $psk] = $rows[$id];
        unset($rows[$id]);
        if ($ssid === $p['ssid'] && $psk === $p['psk']) continue;
        if (($psk === '') !== ($p['psk'] === '')
                && array_intersect(['proto', 'pairwise', 'group'], array_keys($p['values'])))
            throw new RuntimeException('Cannot change authentication mode with custom cipher settings.');
        $settings = classic_wifi_settings($ssid, $psk, ($p['values']['key_mgmt'] ?? '') === 'WPA-PSK');
        foreach ($p['owned'] as $i) unset($lines[$i]);
        $lines[$p['owned'][0]] = implode("\n", $settings);
    }
    ksort($lines, SORT_NUMERIC);
    $config = implode("\n", $lines);
    if ($doc['countryLine'] === null) $config = "country=$country\n" . $config;
    $index = count($doc['profiles']) + $doc['opaque'];
    // Keep the classic numeric id_str/priority convention for additions;
    // never reuse an existing ID just because its row index changed.
    $usedIds = [];
    foreach ($lines as $line) {
        if (preg_match('/^\s*id_str\s*=\s*"([0-9]+)"\s*(?:#.*)?$/D', $line, $m)) $usedIds[$m[1]] = true;
        elseif (preg_match('/^\s*id_str\s*=\s*((?:[0-9A-Fa-f]{2})+)\s*(?:#.*)?$/D', $line, $m))
            $usedIds[hex2bin($m[1])] = true;
        elseif (preg_match('/^\s*id_str\s*=/', $line) && !preg_match('/^\s*id_str\s*=\s*"[^"]*"\s*(?:#.*)?$/D', $line))
            $usedIds['ambiguous'] = true;
    }
    $id = $index;
    foreach ($rows as $row) {
        if (substr($config, -1) !== "\n") $config .= "\n";
        while (isset($usedIds[$id])) $id++;
        $label = isset($usedIds['ambiguous']) ? '' : "\tid_str=\"$id\"\n";
        $priority = 100 - $index;
        $config .= "network={\n" . implode("\n", classic_wifi_settings($row['ssid'], $row['psk'], false))
            . "\n" . $label . "\tpriority=$priority\n\tscan_ssid=1\n}\n\n";
        $id++;
        $index++;
    }
    return $config;
}
