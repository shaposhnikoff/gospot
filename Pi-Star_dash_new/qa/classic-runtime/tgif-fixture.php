<?php
// Loopback-only TGIF API. Mutations affect one in-memory membership only.
$ctx = stream_context_create(['ssl'=>['local_cert'=>'/etc/ssl/certs/ca-certificates.crt',
    'local_pk'=>'/run/qa-tgif-key']]);
$server = stream_socket_server('tls://127.0.0.1:443', $errno, $why, STREAM_SERVER_BIND | STREAM_SERVER_LISTEN, $ctx);
if (!$server) throw new RuntimeException($why);
$token = parse_ini_file('/etc/tgifapi.key',true)['key']['apikey'];
$manager = ($argv[1] ?? '') === '--tgif-manager';
$members = [['slot'=>1,'talkgroup'=>91]];
$ts2 = 99;
for ($i=0; $manager || $i<2; ++$i) {
    $conn = stream_socket_accept($server, $manager ? 60 : 10);
    if (!$conn) throw new RuntimeException('Missing TGIF request');
    if (!preg_match('~^(GET|POST|DELETE) (/v1/(static|dynamic)-talkgroups/1234567) HTTP/1\.[01]$~D',trim(fgets($conn)),$match))
        throw new RuntimeException('Unexpected TGIF method or path');
    [, $method, $path, $kind] = $match;
    $auth = false;
    $length = 0;
    while (($line=fgets($conn)) !== false && trim($line) !== '') {
        if (trim($line)==="Authorization: Bearer $token") $auth=true;
        if (preg_match('/^Content-Length: (\d+)$/i',trim($line),$m)) $length=(int)$m[1];
    }
    if (!$auth) throw new RuntimeException('Missing fixture bearer token');
    $detail = '';
    if ($method!=='GET') {
        if (!$manager || $kind!==getenv('QA_TGIF_KIND') || $length<1 || $length>256) throw new RuntimeException('Unexpected TGIF write');
        $pair=json_decode(stream_get_contents($conn,$length),true,4,JSON_THROW_ON_ERROR);
        if ($kind==='dynamic') {
            if ($method!=='POST' || $pair!==['slot'=>2,'talkgroup'=>($ts2===99 ? 777 : 4000)] || $ts2===0)
                throw new RuntimeException('Unexpected dynamic TG request');
            // TG4000 is the unlink command. An unlinked slot reads back as 0.
            $ts2=$pair['talkgroup']===4000 ? 0 : $pair['talkgroup'];
        } else {
            if ($pair!==['slot'=>1,'talkgroup'=>777]) throw new RuntimeException('Unexpected membership payload');
            if ($method==='POST' && count($members)===1) $members[]=$pair;
            elseif ($method==='DELETE' && ($members[1] ?? null)===$pair) array_pop($members);
            else throw new RuntimeException('Unexpected membership transition');
        }
        $detail=' '.json_encode($pair);
    } elseif (!$manager && $kind!==['static','dynamic'][$i]) throw new RuntimeException('Unexpected TGIF read order');
    $data = $kind==='static' ? ['memberships'=>$members] : ['ts1_talkgroup'=>0,'ts2_talkgroup'=>$ts2];
    $receipt = "Local HTTPS fixture: $method $path$detail, test bearer matched -> 200\n";
    if ($manager) file_put_contents('/tmp/tgif-audit',$receipt,FILE_APPEND);
    $body = json_encode(['ok'=>true]+$data);
    if ($manager && $method==='GET') file_put_contents("/tmp/tgif-last-$kind.json",$body);
    fwrite($conn,"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: ".strlen($body)."\r\nConnection: close\r\n\r\n".$body);
    fclose($conn);
    echo $receipt;
}
