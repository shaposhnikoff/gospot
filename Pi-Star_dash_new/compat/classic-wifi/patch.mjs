import { createHash } from 'node:crypto';
import { constants, copyFileSync, lstatSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';

// Applied only to the audited classic page/row script, after buildClassic.
const skin = '<link rel="stylesheet" href="/css/pistar-modern.css" />';
function digest(text) { return createHash('sha256').update(text).digest('hex'); }
function between(text, start, end, replacement) {
  const a = text.indexOf(start);
  const b = text.indexOf(end, a + start.length);
  if (a < 0 || b < 0) throw Error('Missing classic WiFi patch anchor');
  return text.slice(0, a) + replacement + text.slice(b);
}

export function patchWifi(root) {
  const page = resolve(root, 'admin/wifi.php');
  const script = resolve(root, 'admin/wifi/functions.js');
  for (const file of [page, script]) {
    if (realpathSync(file) !== file || !lstatSync(file).isFile() || lstatSync(file).nlink !== 1)
      throw Error('Expected a fresh packaged WiFi file');
  }
  let php = readFileSync(page, 'utf8');
  let js = readFileSync(script, 'utf8');
  if (!php.includes(skin) || digest(php.replace(skin, '')) !== 'bda899349b7df94ceaa1a312c5e0f8cb00acd6e39d796777ccebe786d9640249'
      || digest(js) !== '0d444d13970eaf12c5c90baa4d80ef594e58ebfa0f4ba55042de71d51e038d25')
    throw Error('Unexpected classic WiFi sources: review the packaged compat patch');

  php = php.replace('$output = $return = 0;', '$output = 0;\n$return = [];')
    .replace("        $return = '';", '        $return = [];')
    .replace('functions.js?version=1.6', 'functions.js?version=classic-profile-1');
  php = between(php, "        exec('sudo cat /etc/wpa_supplicant/", '        $output = \'<form', `        require_once __DIR__ . '/wifi/wpa-profiles.php';
        $pipe = popen('sudo cat /etc/wpa_supplicant/wpa_supplicant.conf', 'r');
        $original = $pipe ? stream_get_contents($pipe) : false;
        if (!$pipe || pclose($pipe) !== 0 || $original === false) {
            echo 'Unable to read WiFi configuration; no changes saved.';
            break;
        }
        try {
            $doc = classic_wifi_parse($original);
        } catch (RuntimeException $e) {
            echo htmlspecialchars($e->getMessage(), ENT_QUOTES, 'UTF-8') . ' WiFi configuration NOT saved.';
            break;
        }
        $ssid = array_column($doc['profiles'], 'ssid');
        $psk = array_column($doc['profiles'], 'psk');
        $numSSIDs = count($ssid);
        $wifiCountry = $doc['country'] ?? 'JP';
        if ($doc['opaque']) echo 'Additional networks with custom settings are preserved unchanged.<br />';
`);
  php = php.replace('<input type="hidden" id="Networks" name="Networks" />',
    `<input type="hidden" name="wifiRevision" value="'.$doc['revision'].'" />
<input type="hidden" id="Networks" name="Networks" value="'.$numSSIDs.'" data-next="'.$numSSIDs.'" />`)
    .replace('id="wpa_conf_form">', 'id="wpa_conf_form" onsubmit="UpdateNetworks()">')
    .replace(' onmouseover="UpdateNetworks(this)"', '')
    .replace('        foreach($regDomains as $regDomain)', `        if (!in_array($wifiCountry, $regDomains ?? [], true)) $regDomains[] = $wifiCountry;
        foreach($regDomains as $regDomain)`);
  php = between(php, '        // Strict validation BEFORE', '            // wpa_supplicant.conf holds', `        try {
            $config = classic_wifi_save($doc, $_POST);
            // This now includes opaque profiles' secrets too. Publish a
            // complete, private (0600) file at the existing install path.
            $temp = tempnam('/tmp', 'wifi-');
            if ($temp === false || file_put_contents($temp, $config) !== strlen($config)
                    || !rename($temp, '/tmp/wifidata')) {
                if ($temp !== false) unlink($temp);
                throw new RuntimeException('Unable to stage WiFi configuration.');
            }
`);
  php = php.replace("sudo mount -o remount,ro /');\n            echo \"Wifi Settings Updated",
    `sudo mount -o remount,ro /', $status);
            if ($status !== 0) throw new RuntimeException('Unable to install WiFi configuration.');
            echo "Wifi Settings Updated`)
    .replace('?page=\\wlan0_info', '?page=wlan0_info')
    .replace("        }\n\n    } elseif(isset($_POST['Scan']))", `        } catch (RuntimeException $e) {
            echo '<b>WiFi configuration NOT saved.</b> ' . htmlspecialchars($e->getMessage(), ENT_QUOTES, 'UTF-8');
        }

    } elseif(isset($_POST['Scan']))`);

  // Append rather than rebuilding innerHTML (which loses unsaved input).
  // Networks is the live count; data-next is a separate monotonic row ID.
  js = between(js, 'function AddNetwork()', 'function CheckSSID', `function AddNetwork() {
    var counter = document.getElementById('Networks');
    var id = Number(counter.dataset.next);
    counter.dataset.next = id + 1;
    document.getElementById('networkbox').insertAdjacentHTML('beforeend',
        '<div id="Networkbox'+id+'" class="NetworkBoxes">Network '+id+
        '<input type="button" value="Delete" onclick="DeleteNetwork('+id+')" /><br />'+
        '<span class="tableft">SSID :</span><input type="text" id="ssid'+id+'" name="ssid'+id+'" onkeyup="CheckSSID(this)" /><br />'+
        '<span class="tableft">PSK :</span><input type="password" id="psk'+id+'" name="psk'+id+'" onkeyup="CheckPSK(this)" /></div>');
    UpdateNetworks();
    return id;
}
function AddScanned(network) {
    document.getElementById('ssid'+AddNetwork()).value = network;
}

`);
  js = js.replace('ssid.value.length>31', 'ssid.value.length>32')
    .replace("document.getElementById('Save').disabled = true;", "ssid.setCustomValidity('Maximum 32 bytes.');")
    .replace("document.getElementById('Save').disabled = false;", "ssid.setCustomValidity('');")
    .replace("document.getElementById('Save').disabled = true;", "psk.setCustomValidity('Minimum 8 characters.');")
    .replace("document.getElementById('Save').disabled = false;", "psk.setCustomValidity('');");
  copyFileSync(new URL('./wpa-profiles.php', import.meta.url), join(root, 'admin/wifi/wpa-profiles.php'), constants.COPYFILE_EXCL);
  writeFileSync(page, php);
  writeFileSync(script, js);
}
