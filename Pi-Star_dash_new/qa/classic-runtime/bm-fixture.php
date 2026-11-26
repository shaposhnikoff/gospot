<?php
// Stateful BM v2 subset on container loopback. No credentials in receipts.
$ctx = stream_context_create(['ssl'=>['local_cert'=>'/etc/ssl/certs/ca-certificates.crt','local_pk'=>'/run/qa-bm-key']]);
$server = stream_socket_server('tls://127.0.0.1:443', $errno, $why, STREAM_SERVER_BIND | STREAM_SERVER_LISTEN, $ctx);
if (!$server) throw new RuntimeException($why);
$token = parse_ini_file('/etc/bmapi.key', true)['key']['apikey'];
$base = '/v2/device/1234567';
$members = ['2/91'=>['slot'=>2,'talkgroup'=>91]];
$dynamic = [];
$dropScenario = in_array(getenv('QA_BM_KIND'), ['drop-dynamic','drop-dynamic-simplex'], true);
$failQso = getenv('QA_BM_KIND') === 'drop-qso-503';
$simplex = str_ends_with(getenv('QA_BM_KIND'), '-simplex');
$qsoScenario = str_starts_with(getenv('QA_BM_KIND'), 'drop-qso');
if ($qsoScenario) {
    $routes = $simplex ? [0=>'QA-CALL-000'] : [1=>'QA-CALL-101',2=>'QA-CALL-202'];
    file_put_contents('/tmp/bm-routes.json',json_encode($routes,JSON_FORCE_OBJECT|JSON_THROW_ON_ERROR));
}
if ($simplex) {
    $members = ['0/91'=>['slot'=>0,'talkgroup'=>91]];
    $dynamic = [['slot'=>0,'talkgroup'=>701]];
    if ($dropScenario) $dynamic[] = ['slot'=>0,'talkgroup'=>702];
} elseif ($dropScenario || $qsoScenario) {
    $members['1/92'] = ['slot'=>1,'talkgroup'=>92];
    $dynamic = [['slot'=>1,'talkgroup'=>701],['slot'=>2,'talkgroup'=>801],['slot'=>1,'talkgroup'=>702]];
}
while ($conn = stream_socket_accept($server,60)) {
    [$method,$path] = explode(' ',trim(fgets($conn)),3);
    $headers = [];
    while (($line=fgets($conn)) !== false && trim($line) !== '') {
        [$k,$v] = explode(':',$line,2);
        $headers[strtolower($k)] = trim($v);
    }
    $size = (int)($headers['content-length'] ?? 0);
    $body = $size > 0 && $size <= 256 ? json_decode(stream_get_contents($conn,$size),true) : null;
    $auth = ($headers['authorization'] ?? '') === "Bearer $token";
    $status = 400;
    $reply = ['ok'=>false];
    if ($method==='GET' && $path==="$base/profile") {
        $reply = ['staticSubscriptions'=>array_values($members),'dynamicSubscriptions'=>$dynamic];
        $status = 200;
    } elseif ($qsoScenario && $auth && $method==='GET' && $size===0 && preg_match("~^$base/action/dropCallRoute/([012])$~D",$path,$m) && isset($routes[$m[1]])) {
        if ($failQso) {
            $status = 503;
        } else {
            unset($routes[$m[1]]);
            file_put_contents('/tmp/bm-routes.json',json_encode($routes,JSON_FORCE_OBJECT|JSON_THROW_ON_ERROR));
            $reply = ['ok'=>true];
            $status = 200;
        }
    } elseif ($dropScenario && $auth && $method==='GET' && $size===0 && preg_match("~^$base/action/dropDynamicGroups/([012])$~D",$path,$m)) {
        $dynamic = array_values(array_filter($dynamic,fn($tg)=>$tg['slot'] !== (int)$m[1]));
        $reply = ['ok'=>true];
        $status = 200;
    } elseif ($auth && $method==='POST' && $path==="$base/talkgroup/" && ($headers['content-type'] ?? '')==='application/json'
              && in_array($body['slot'] ?? '', $simplex ? ['0'] : ['1','2'],true) && ctype_digit($body['group'] ?? '')) {
        $key = $body['slot'].'/'.$body['group'];
        if (!isset($members[$key])) {
            $members[$key] = ['slot'=>(int)$body['slot'],'talkgroup'=>(int)$body['group']];
            $status = 200;
        }
    } elseif ($auth && $method==='DELETE' && $size===0 && preg_match("~^$base/talkgroup/([012]/[0-9]+)$~D",$path,$m) && isset($members[$m[1]])) {
        unset($members[$m[1]]);
        $status = 200;
    }
    if ($method!=='GET') $reply = ['ok'=>$status===200];
    $receipt = compact('method','path','body','auth','status');
    file_put_contents('/tmp/bm-audit',json_encode($receipt,JSON_UNESCAPED_SLASHES)."\n",FILE_APPEND);
    $json = json_encode($reply);
    $reason = [200=>'OK',400=>'Bad Request',503=>'Service Unavailable'][$status];
    fwrite($conn,"HTTP/1.1 $status $reason\r\nContent-Type: application/json\r\nContent-Length: ".strlen($json)."\r\nConnection: close\r\n\r\n$json");
    fclose($conn);
}
