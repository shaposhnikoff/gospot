<?php
// PHP 8 permits a disabled builtin to be defined in userland. Never pass
// commands to a shell. Fail the run on an unrecognised probe, even on a GET.
if (getenv('QA_CATALOG') === '1') require '/qa/catalog-probes.php';
function exec($command, &$output = null, &$status = null) {
    $data = [
        'cat /etc/hostname' => 'fixture',
        'cat /sys/class/thermal/thermal_zone0/temp' => '42000',
        'hostname -I' => '127.0.0.1',
        'uptime -p' => 'up 1 minute',
        '/usr/local/bin/platformDetect.sh' => 'Desktop fixture',
        'ps -eo comm' => 'php',
        'ps -eo args' => 'php',
        'pgrep pistar-keeper' => '',
        'ifconfig wlan0' => 'wlan0 HWaddr 02:00:00:00:00:01',
        'iwconfig wlan0' => '',
        'iw dev wlan0 link' => '',
        'ls /dev/ | egrep -h "ttyA|ttyUSB"' => '',
        'timedatectl list-timezones' => 'UTC',
        'cat /etc/timezone' => 'UTC',
        'grep "80 80" /usr/local/sbin/pistar-upnp.service | cut -c 1' => '#',
        'grep "10022 10022" /usr/local/sbin/pistar-upnp.service | cut -c 1' => '#',
        'grep "22 22" /usr/local/sbin/pistar-upnp.service | cut -c 1' => '#',
        'grep "pistar-upnp.service" /etc/crontab | cut -c 1' => '#',
        'ip -j addr show 2>/dev/null' => '[]',
        '(grep -v "  /TIME" /var/log/pi-star/Headers.log|sort -r -k7,7|sort -u -k7,8|sort -r|head -20 >/tmp/lastheard.log) 2>&1 &' => '',
        '(grep "Repeater header" /var/log/pi-star/Headers.log|sort -r -k7,7|sort -u -k7,8|sort -r >/tmp/worked.log) 2>&1 &' => '',
    ];
    if (getenv('QA_REMOTE_MANAGER') === '1' && preg_match('~^cd /var/log/pi-star && sudo /usr/local/bin/RemoteCommand ([0-9]+(?: [A-Za-z0-9]+(?:-[A-Za-z0-9]+)*)+)$~D',$command,$m)) {
        // Run the logging-only fixture without sudo, a shell, or any gateway.
        $args = array_merge(['/usr/local/bin/RemoteCommand'], explode(' ', $m[1]));
        $process = proc_open($args, [0=>['file','/dev/null','r'], 1=>['pipe','w']], $pipes, '/var/log/pi-star');
        if (!is_resource($process)) throw new RuntimeException('Could not start fixture RemoteCommand');
        $result = trim(stream_get_contents($pipes[1]));
        fclose($pipes[1]);
        if (proc_close($process) !== 0) throw new RuntimeException('Fixture RemoteCommand failed');
    } elseif (getenv('QA_DSTAR_MANAGER') === '1' && str_starts_with($command,'sudo remotecontrold ')) {
        file_put_contents('/tmp/remotecontrold-audit',$command."\n",FILE_APPEND|LOCK_EX);
        $result = 'QA recorded: '.$command;
    } elseif ($command === 'grep "CCS link" /var/log/pi-star/Links.log | wc -l') {
        $result = (string)count(preg_grep('/CCS link/',file('/var/log/pi-star/Links.log')));
    } elseif (preg_match('~^grep "MMDVM protocol version" /var/log/pi-star/MMDVM-\d{4}-\d{2}-\d{2}\.log \| tail -1$~D', $command)) {
        $result = '';
    } else {
        $result = $data[$command] ?? (function_exists('catalog_probe') ? catalog_probe($command) :
            throw new RuntimeException("Unmodelled fixture probe: $command"));
    }
    if (!is_array($output)) $output = [];
    if ($result !== '') $output[] = $result;
    $status = 0;
    return $result;
}
