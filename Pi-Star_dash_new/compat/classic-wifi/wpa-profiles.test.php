<?php
require __DIR__ . '/wpa-profiles.php';
function check($ok, $message) { if (!$ok) throw new Exception($message); }
function refused($fn) {
    try { $fn(); } catch (RuntimeException $e) { return; }
    throw new Exception('Expected save/parse to fail');
}
function form($doc) {
    $post = ['wifiRevision' => $doc['revision'], 'Networks' => (string)count($doc['profiles']),
        'wifiCountryCode' => $doc['country'] ?? 'JP'];
    foreach ($doc['profiles'] as $i => $p) {
        $post['ssid'.$i] = $p['ssid'];
        $post['psk'.$i] = $p['psk'];
    }
    return $post;
}

$raw = str_repeat('Ab', 32);
$globals = "ctrl_interface=DIR=/run/wpa_supplicant GROUP=netdev\ncountry=GB  # original\nfreq_list=2412 2437 2462\n\n";
$opaque = "network={\n  ssid=\"Enterprise\"\n  key_mgmt=WPA-EAP\n  eap=TLS\n  id_str=\"3\"\n}\n";
$options = "\tbssid=02:00:00:00:00:01\n\tpriority=47\n";
$first = "network={\n  psk=\"old=password\"\n  ssid=\"old=ssid\"\n$options}\n";
$second = "network={\n  ssid=526177\n  #psk=\"stale hint\"\n  psk=$raw\n}\n";
$original = $globals . $opaque . $first . $second;
$doc = classic_wifi_parse($original);
$post = form($doc);
check($doc['opaque'] === 1 && $post['Networks'] === '2' && $post['ssid0'] === 'old=ssid'
    && $post['psk0'] === 'old=password' && $post['psk1'] === $raw, 'Block-scoped effective settings');
check(classic_wifi_save($doc, $post) === $original, 'No-op preserves original bytes');

// Original visible indexes do not include Enterprise; suffix 2 is an addition.
$add = $post;
$add['Networks'] = '3';
$add['ssid2'] = '0';
$add['psk2'] = $raw;
$saved = classic_wifi_save($doc, $add);
check(strpos($saved, $original) === 0 && substr_count($saved, "psk=$raw\n") === 2, 'Append and preserve raw key');
$reload = classic_wifi_parse($saved);
check(form($reload)['ssid2'] === '0' && form($reload)['psk2'] === $raw, 'Reload added SSID and raw key');
check(strpos(substr($saved, strlen($original)), 'id_str="4"') !== false, 'Avoid colliding with opaque ID');

// Delete the middle visible row, rename/update first, change remaining row
// to open, and add another row without reusing the deleted index.
$edit = form($reload);
unset($edit['ssid1'], $edit['psk1']);
$edit['ssid0'] = 'new=name';
$edit['psk0'] = 'pass=word';
$edit['psk2'] = '';
$edit['ssid3'] = '*';
$edit['psk3'] = '';
$edit['wifiCountryCode'] = 'FR';
$result = classic_wifi_save($reload, $edit);
$after = classic_wifi_parse($result);
check($after['opaque'] === 1 && strpos($result, str_replace('GB', 'FR', $globals) . $opaque) === 0, 'Global and opaque bytes');
check(strpos($result, $options) !== false && strpos($result, 'psk='.$raw) === false, 'Options and deletion');
check(array_column($after['profiles'], 'ssid') === ['new=name', '0', '*']
    && array_column($after['profiles'], 'psk') === ['pass=word', '', ''], 'No broken rows after edits');
check(strpos($result, 'psk=' . hash_pbkdf2('sha1', 'pass=word', 'new=name', 4096, 64)) !== false, 'SSID-dependent key');
check(classic_wifi_save($after, form($after)) === $result, 'Reload is stable');

$ambiguous = "network={\nssid=\"One\"\nssid=\"Two\"\nkey_mgmt=NONE\n}\n";
$doc2 = classic_wifi_parse($ambiguous . $opaque . $first);
$drop = form($doc2);
$drop['Networks'] = '0';
unset($drop['ssid0'], $drop['psk0']);
check(classic_wifi_save($doc2, $drop) === "country=JP\n" . $ambiguous . $opaque, 'Delete all editable only');
foreach (["network={\n", "network={\nnetwork={\n}\n}\n", "country=US\ncountry=DE\n", "blob-base64-foo={\nAA==\n}\n"] as $bad)
    refused(function () use ($bad) { classic_wifi_parse($bad); });
foreach ([['wifiRevision' => 'stale'], ['Networks' => '0'], ['psk0' => null], ['ssid0' => []],
        ['ssid0' => '"'], ['wifiCountryCode' => "US\n"], ['ssid00' => 'duplicate'], ['psk0' => 'short']] as $bad)
    refused(function () use ($doc, $post, $bad) { classic_wifi_save($doc, array_merge($post, $bad)); });
$cipher = classic_wifi_parse(str_replace($options, $options . "\tproto=RSN\n", $first));
refused(function () use ($cipher) { classic_wifi_save($cipher, array_merge(form($cipher), ['psk0' => ''])); });
echo "WiFi profile merge tests passed\n";
