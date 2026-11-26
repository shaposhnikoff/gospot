<?php
// GET-only catalog fixture. Never run a shell. Only stage read-only fixture
// inputs into an editor's existing tempnam file, as the unprivileged server.
if ($_SERVER['REQUEST_METHOD'] !== 'GET') { http_response_code(405); exit; }
function catalog_probe($cmd) {
    $tmp = "'(/tmp/pistar-edit-[a-zA-Z0-9]{6})'";
    if (preg_match("~^sudo cp (/etc/(?:mmdvmhost|dstarrepeater|ircddbgateway|timeserver|starnetserver|dmrgateway|ysfgateway|p25gateway|nxdngateway|dapnetgateway|pistar-remote|crontab|wpa_supplicant/wpa_supplicant.conf)|/usr/local/etc/RSSI.dat) $tmp$~D",$cmd,$m)
        && realpath($m[2])===$m[2] && is_file($m[2]) && copy($m[1],$m[2])) {
        // is_file above cached the empty tempnam inode. A real external cp
        // wouldn't populate PHP's stat cache before the editor's filesize().
        clearstatcache(true,$m[2]);
        return '';
    }
    if (preg_match("~^sudo (?:chown www-data:www-data|chmod 600) $tmp$~D",$cmd,$m)
        && realpath($m[1])===$m[1] && is_file($m[1])) return '';
    if (in_array($cmd,['pgrep dstarrepeaterd','pgrep MMDVMHost','pgrep ircddbgatewayd','pgrep timeserverd',
        "pgrep -f -a /usr/local/sbin/pistar-watchdog | sed '/pgrep/d'", "pgrep -f -a /usr/local/sbin/pistar-keeper | sed '/pgrep/d'"],true)) return '';
    return match ($cmd) {
        'df --block-size=1' => 'tmpfs 1000 100 900 10% /tmp',
        'sudo grep "country" /etc/wpa_supplicant/wpa_supplicant.conf' => 'country=GB',
        "grep \"RXFrequency\" /etc/mmdvmhost | awk -F \"=\" '{print \$2}'" => '430000000',
        "grep \"RXOffset\" /etc/mmdvmhost | awk -F \"=\" '{print \$2}'" => '0',
        default => throw new RuntimeException("Unmodelled catalog fixture probe: $cmd")
    };
}
function popen($cmd,$mode) {
    if ($cmd !== 'sudo cat /etc/wpa_supplicant/wpa_supplicant.conf' || $mode !== 'r')
        throw new RuntimeException('Unexpected pipe in catalog GET');
    return fopen('/etc/wpa_supplicant/wpa_supplicant.conf','r');
}
function pclose($fp) { return fclose($fp) ? 0 : 1; }
