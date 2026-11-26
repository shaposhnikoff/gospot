<?php
// This client runs *inside* the offline container. Paths come only from the
// test, never form actions, redirects, external links or a configured peer.
function expect($condition, $message) {
    if (!$condition) throw new RuntimeException($message);
}
function request($path, $status = 200, $type = 'text/html', $body = null, $cookie = '') {
    $method = $body === null ? 'GET' : 'POST';
    $ctx = stream_context_create(['http' => [
        'method' => $method, 'user_agent' => 'Desktop-fixture', 'timeout' => 5, 'follow_location' => 0, 'ignore_errors' => true,
        'header' => ($cookie ? "Cookie: $cookie\r\n" : '') .
            ($body !== null ? "Content-Type: application/x-www-form-urlencoded\r\n" : ''),
        'content' => $body ?? '',
    ]]);
    $text = file_get_contents("http://127.0.0.1:8080$path", false, $ctx, 0, 2_000_001);
    $line = $http_response_header[0] ?? 'no HTTP response';
    expect(str_contains($line, " $status "), "$method $path: $line; expected $status");
    expect(is_string($text) && strlen($text) <= 2_000_000, "$path: response missing or oversized");
    $headers = implode("\n", $http_response_header);
    expect(stripos($headers, "Content-Type: $type") !== false, "$path: unexpected content type");
    return [$text, $headers];
}
function contains($body, $needles, $path) {
    foreach ($needles as $needle) expect(str_contains($body, $needle), "$path: missing $needle");
}
function page($path, ...$needles) {
    [$body, $headers] = request($path);
    contains($body, ['href="/css/pistar-modern.css"', '</html>', ...$needles], $path);
    contains($headers, ['X-Content-Type-Options: nosniff'], $path);
    static $seen = [];
    foreach (['css' => ['href','text/css'], 'js' => ['src','application/javascript']] as $ext => [$attr, $mime]) {
        expect(preg_match("~$attr=\"(/operator/classic/shell-[\\w-]+\\.$ext)\"~", $body, $match), "$path: missing classic $ext");
        if (!isset($seen[$match[1]])) {
            $asset = request($match[1], 200, $mime)[0];
            expect(strlen($asset) > 0, "$path: empty $ext");
            $seen[$match[1]] = true;
        }
    }
    return [$body, $headers];
}
for ($i = 0; $i < 40; $i++) {
    if ($socket = @fsockopen('127.0.0.1', 8080, $errno, $reason, 0.1)) { fclose($socket); break; }
    usleep(25_000);
}
$mmdvm = $argv[1] === 'mmdvmhost';
if (($argv[2] ?? '') === '--auto-ap') {
    expect(is_dir('/sys/class/net/wlan0'), 'Missing fixture wlan0');
    $path = '/admin/configure.php';
    [$html,$headers] = request($path);
    $doc = new DOMDocument();
    $doc->loadHTML($html, LIBXML_NONET | LIBXML_NOWARNING | LIBXML_NOERROR);
    $xp = new DOMXPath($doc);
    $mode = $mmdvm ? 'MMDVM' : 'DSTAR';
    expect($xp->query("//input[@name='controllerSoft' and @checked and @value='$mode']")->length===1, 'Wrong controller selected');
    $form = "//form[@id='autoApPassForm' and @method='post' and @action='$path']";
    expect($xp->query($form)->length===1, 'Missing Auto AP form');
    foreach (['psk1'=>'autoapPsk','psk2'=>'autoapPskConfirm'] as $id=>$name) {
        $field = "$form//input[@id='$id' and @name='$name' and @type='password' and not(@disabled)]";
        expect($xp->query($field)->length===1 && $xp->query($field)[0]->getAttribute('value')==='', "Missing or prefilled $id");
    }
    $fields = $xp->query("$form//input[@name='csrf_token' and @type='hidden']");
    expect($fields->length===1 && preg_match('/^[a-f0-9]{64}$/D',$token=$fields[0]->getAttribute('value')), 'Invalid Auto AP CSRF field');
    expect(preg_match('/Set-Cookie: PHPSESSID=([a-zA-Z0-9,-]+);/i',$headers,$sid), 'Missing session cookie');
    session_save_path('/tmp/sessions');
    session_id($sid[1]);
    expect(session_start(['read_and_close'=>true]) && hash_equals($_SESSION['csrf_token'] ?? '',$token), 'Auto AP token does not match PHP session');
    echo "PASS $mode GET $path (#autoApPassForm): HTTP 200, blank password fields #psk1 and #psk2, hidden CSRF matches PHP session; no form submitted\n";
    exit;
}
if (($argv[2] ?? '') === '--tgif-static') {
    expect($mmdvm && gethostbyname('api.tgif.network')==='127.0.0.1','Expected isolated TGIF fixture');
    $token = parse_ini_file('/etc/tgifapi.key', true)['key']['apikey'];
    foreach (['/admin/','/mmdvmhost/tgif_static_links.php'] as $path) {
        [$html] = request($path);
        expect(!str_contains($html,$token), 'Fixture bearer token disclosed in response');
        $doc = new DOMDocument();
        $doc->loadHTML($html, LIBXML_NONET | LIBXML_NOWARNING | LIBXML_NOERROR);
        $xp = new DOMXPath($doc);
        $initial = $path === '/admin/';
        $scope = $initial ? '//*[@id="tgifStaticConnects"]' : '';
        if ($initial) {
            expect($xp->query($scope)->length===1, 'Missing TGIF anchor');
            contains($html, ['$("#tgifStaticConnects").load("/mmdvmhost/tgif_static_links.php"'], $path);
            $form = '//form[@method="post" and .//input[@name="tgifStaticTalkgroup"]]';
            foreach (['csrf_token','tgifStaticModify','tgifDynamicModify'] as $field)
                expect($xp->query("$form//input[@name='$field']")->length===1, "Missing TGIF form field $field");
        }
        $table = $scope.'//b[normalize-space(.)="Active TGIF Connections"]/following-sibling::table[1]';
        expect($xp->query($table)->length===1 && $xp->query("$table//th")->length===4, 'Missing TGIF status table');
        $cells = array_map(fn($td)=>trim($td->textContent), iterator_to_array($xp->query("$table//td")));
        expect($cells === ['tgif.network','1234567',$initial?'…':'TG91(1)',$initial?'…':'TG99(2)'], 'Unexpected TGIF cells: '.json_encode($cells));
        echo "PASS GET $path: HTTP 200, ".($initial?'#tgifStaticConnects + manager form':'AJAX status').", row=".json_encode($cells,JSON_UNESCAPED_UNICODE)."; no token in HTML\n";
    }
    exit;
}
if (($argv[2] ?? '') === '--dstar-network') {
    $cfg = parse_ini_file('/etc/mmdvmhost', true);
    expect($mmdvm && is_file('/etc/dstar-radio.mmdvmhost') && !file_exists('/etc/dstar-radio.dstarrepeater')
        && $cfg['D-Star']['Enable'] === '1' && $cfg['D-Star Network']['Enable'] === '1', 'Unexpected D-Star Network fixture');
    echo "Fixture: MMDVMHost controller, D-Star.Enable=1, D-Star Network.Enable=1\n";
    foreach ([
        ['/', 'cssConnects', ['N0DEMO B', 'QA7PEER', 'CCS', 'Incoming', '12:04:12 Jan 2nd']],
        ['/admin/', 'refLinks', ['N0DEMO B', 'NONE', 'No', 'Never', 'Up', 'DCS023 C', 'DCS', 'Outgoing', '12:05:34 Jan 2nd']]
    ] as [$path, $anchor, $row]) {
        [$html] = request($path);
        $doc = new DOMDocument();
        $doc->loadHTML($html, LIBXML_NONET | LIBXML_NOWARNING | LIBXML_NOERROR);
        $xp = new DOMXPath($doc);
        $section = "//*[@id='$anchor']";
        $table = "$section//table";
        expect($xp->query($section)->length === 1 && $xp->query($table)->length === 1
            && $xp->query("$table//th")->length === count($row), "Missing or unexpected table at $path#$anchor");
        $cells = array_map(fn($cell) => trim(str_replace("\u{a0}", ' ', $cell->textContent)),
            iterator_to_array($xp->query("$table//tr/td")));
        expect($cells === $row, "Incorrect connection data at $path#$anchor: ".json_encode($cells));
        echo "PASS GET $path (#$anchor): HTTP 200, ".count($cells)."-column table, row=".json_encode($cells)."\n";
    }
    exit;
}
if (($argv[2] ?? '') === '--pocsag') {
    $cfg = parse_ini_file('/etc/mmdvmhost', true);
    expect($mmdvm && $cfg['POCSAG']['Enable'] === '1' && $cfg['POCSAG Network']['Enable'] === '1'
           && $cfg['POCSAG']['Frequency'] === '439987500', 'Unexpected POCSAG fixture');
    echo "Fixture: MMDVMHost, POCSAG.Enable=1, POCSAG Network.Enable=1, Frequency=439987500 Hz\n";
    foreach (['/', '/admin/configure.php'] as $path) {
        [$html] = request($path);
        $doc = new DOMDocument();
        $doc->loadHTML($html, LIBXML_NONET | LIBXML_NOWARNING | LIBXML_NOERROR);
        $xp = new DOMXPath($doc);
        if ($path === '/') {
            $table = '//*[@id="Pages"]//table';
            expect($xp->query($table)->length === 1 && $xp->query("$table//th")->length === 4, 'Missing #Pages message table');
            $cells = array_map(fn($cell) => trim($cell->textContent), iterator_to_array($xp->query("$table//tr/td")));
            expect(count($cells) === 4 && array_slice($cells,1) === ['Slot 3','1234567','QA pager message'], 'Incorrect pager row: '.json_encode($cells));
            echo "PASS GET / (#Pages): HTTP 200, four-column table, row=".json_encode($cells)."\n";
        } else {
            $inputs = $xp->query('//form[@id="config"]//input[@id="pocsagFrequency" and @name="pocsagFrequency" and @type="text"]');
            expect($inputs->length === 1 && $inputs[0]->getAttribute('value') === '439.987500', 'Incorrect POCSAG frequency field');
            echo "PASS GET /admin/configure.php (#pocsagFrequency): HTTP 200, input value=439.987500 MHz\n";
        }
    }
    exit;
}
$api = '/api/last_heard.php';
$expected = [];
if ($mmdvm) {
    $keys = explode(' ', 'time_utc mode callsign callsign_suffix target src duration loss bit_error_rate rssi');
    $expected[] = array_combine($keys, ['2020-01-02 12:00:10','DMR Slot 2','N0RF','','TG 9','RF','3.0','0%','0.2%','']);
    $expected[] = array_combine($keys, ['2020-01-02 12:00:01','DMR Slot 2','N0NET','','TG 91','Net','2.0','1%','0.0%','']);
}
foreach (['' => $expected, '?num_transmissions=1' => array_slice($expected, 0, 1), '?num_transmissions=0' => []] as $q => $rows) {
    [$json] = request($api.$q, 200, 'application/json');
    expect(json_decode($json, true, 8, JSON_THROW_ON_ERROR) === $rows, "$api$q: unexpected JSON: $json");
    echo "GET $api$q: 200 application/json ".trim($json)."\n";
}
echo "GET $api: ".count($expected)." fixture row(s); limits 1 and 0 OK\n";
$call = $mmdvm ? 'N0RF' : 'N0DSTAR';
$lh = $mmdvm ? '/mmdvmhost/lh.php' : '/dstarrepeater/last_herd.php';
$tx = $mmdvm ? '/mmdvmhost/localtx.php' : '/dstarrepeater/local_tx.php';
[$public] = page('/', $call, 'N0DEMO', $lh, $tx, '/admin/configure.php', 'class="footer"');
expect(!str_contains($public, 'id="sysInfo"'), 'admin block rendered on public index');
page('/admin/', $call, '/admin/live_modem_log.php', 'id="sysInfo"', 'Desktop fixture', 'class="footer"');
foreach ([$lh, $tx] as $partial) contains(request($partial)[0], [$call, '</table>'], $partial);
echo "GET /, /admin/, $lh, $tx: mode-specific content OK\n";
$operator = request('/operator/')[0];
contains($operator, ['id="app"'], '/operator/');
expect(preg_match('~src="(?:\./|/operator/)(assets/[\w.-]+\.js)"~', $operator, $entry), 'operator entry missing');
expect(request('/operator/'.$entry[1], 200, 'application/javascript')[0] !== '', 'empty operator entry');
echo "GET /operator/, its module and classic shell JS/CSS: OK\n";
page('/admin/expert/', 'edit_mmdvmhost.php', 'Expert Editors');
$viewer = '/admin/live_modem_log.php';
[, $headers] = page($viewer, "$viewer?ajax", 'id="tail"');
expect(preg_match('/Set-Cookie: (PHPSESSID=[^;]+);/i', $headers, $session), 'Live Logs session missing');
$poll = fn() => request("$viewer?ajax", 200, 'text/html', null, $session[1])[0];
$log = '/var/log/pi-star/'.($mmdvm ? 'MMDVM' : 'DStarRepeater').'-'.gmdate('Y-m-d').'.log';
expect(is_file($log) && filesize($log) > 0 && $poll() === '', 'Live Logs did not start at EOF');
$line = "QA $argv[1]: new transmission\n";
expect(file_put_contents($log, $line, FILE_APPEND) === strlen($line), 'Cannot append fixture log');
expect($poll() === nl2br($line), 'Live Logs did not return exactly the newly appended line');
expect($poll() === '', 'Live Logs duplicated an already consumed line');
echo "GET $viewer?ajax (same session): 200 empty at EOF -> 200 \"$argv[1]: new transmission\" (new line verified) -> 200 empty\n";
foreach (['/css/pistar-css.php','/css/pistar-css-mini.php','/admin/css/pistar-css.php',
          '/css/nice-select.min.css','/css/pistar-modern.css'] as $style) {
    contains(request($style, 200, 'text/css')[0], ['{','}'], $style);
}
foreach (['/jquery.min.js','/jquery-timing.min.js','/functions.js','/nice-select.min.js'] as $js) {
    contains(request($js, 200, 'application/javascript')[0], ['function'], $js);
}
expect(str_starts_with(request('/admin/images/reboot.png', 200, 'image/png')[0], "\x89PNG"), 'image missing');
echo "GET expert menu, live log viewer, 5 stylesheets, 4 scripts, admin image alias: OK\n";
foreach (['/admin/power.php','/admin/config_backup.php','/admin/configure.php','/admin/update.php','/admin/wifi.php?page=wlan0_info'] as $form) {
    [$body, $headers] = page($form, "action=\"$form\"", 'method="post"');
    $doc = new DOMDocument();
    $doc->loadHTML($body, LIBXML_NONET | LIBXML_NOWARNING | LIBXML_NOERROR);
    $xpath = new DOMXPath($doc);
    $inputs = $xpath->query("//form[@action='$form' and @method='post']//input[@type='hidden' and @name='csrf_token']");
    expect($inputs->length > 0 && preg_match('/^[a-f0-9]{64}$/D', $inputs[0]->getAttribute('value')), "$form: form token missing");
    if ($form === '/admin/configure.php') {
        contains($body, ['id="config"', 'name="controllerSoft" value="'.($mmdvm ? 'MMDVM' : 'DSTAR').'" checked="checked"'], $form);
    }
    expect(preg_match('/Set-Cookie: (PHPSESSID=[^;]+);[^\n]*HttpOnly[^\n]*SameSite=Lax/i', $headers, $match),
        "$form: missing session cookie or flags");
    // No action fields, credentials, files, or valid token are ever supplied.
    [$rejected, $rejectHeaders] = request($form, 403, 'text/html', '', $match[1]);
    contains($rejected, ['<h1>403 Forbidden</h1>','/css/pistar-modern.css'], $form);
    contains($rejectHeaders, ['no-store'], $form);
    request($form, 403, 'text/html', 'csrf_token=fixture-mismatch', $match[1]);
    [$unused, $expiredHeaders] = request($form, 303, 'text/html', '');
    contains($expiredHeaders, ["Location: $form"], $form);
    echo "GET $form: form + session; missing / wrong / fresh-session POST: 403 / 403 / 303\n";
}
