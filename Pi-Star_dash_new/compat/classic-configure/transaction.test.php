<?php
if (PHP_SAPI !== 'cli' || getenv('CALLSIGN_FIXTURE') !== 'tmpfs' || !is_file('/.dockerenv')
    || file_exists('/etc/passwd') || getcwd() === false) exit(2);
require '/qa/callsign-apply.php';
function check($ok, $why) { if (!$ok) throw new Exception($why); }
// configure.php supplies this function; its actual hash is checked by the
// browser/disk oracle. Use a known result to isolate propagation failures.
function aprspass($call) { check($call === 'QA7TEST', 'unexpected callsign'); return 1188; }
putenv('PATH=/tmp:/usr/bin:/bin');
file_put_contents('/tmp/systemctl', "#!/bin/sh\n[ \"\$*\" = 'is-active --quiet mmdvmhost.service' ] && exit 0\nexit 3\n");
file_put_contents('/tmp/sudo', <<<'SH'
#!/bin/sh
case "$*" in
  'mount -o remount,rw /'|'mount -o remount,ro /'|sync) echo "$*" >> /tmp/log ;;
  'systemctl restart mmdvmhost.service') echo "$*" >> /tmp/log; exit 1 ;;
  'systemctl start dstarrepeater.service') echo "$*" >> /tmp/log; exit 1 ;;
  'touch /etc/dstar-radio.dstarrepeater') echo "$*" >> /tmp/log; touch "$2" ;;
  'rm -f -- /etc/dstar-radio.dstarrepeater') echo "$*" >> /tmp/log; rm -f -- "$4" ;;
  'systemctl stop dstarrepeater.service'|'systemctl stop mmdvmhost.service'|'systemctl start mmdvmhost.service')
    echo "$*" >> /tmp/log ;;
  'mv -nT -- /etc/dstar-radio.mmdvmhost /etc/dstar-radio.dstarrepeater'|'mv -nT -- /etc/dstar-radio.dstarrepeater /etc/dstar-radio.mmdvmhost')
    echo "$*" >> /tmp/log; shift; exec mv "$@" ;;
  'timedatectl set-timezone UTC'|'timedatectl set-timezone Europe/Berlin')
    echo "$*" >> /tmp/log; ln -snf "/usr/share/zoneinfo/$3" /etc/localtime ;;
  'install -m 644 -o root -g root '*)
    case "$9" in
      /etc/ircddbgateway|/etc/dstarrepeater|/etc/aprsgateway|/etc/mmdvmhost|/etc/dmrgateway|/var/www/dashboard/config/ircddblocal.php|/etc/timezone|/var/www/dashboard/config/config.php) ;;
      *) exit 99 ;;
    esac
    echo "install $9" >> /tmp/log
    if [ -e /tmp/fail ] && [ "$9" = /etc/mmdvmhost ]; then
      echo 'partial' >"$9"; rm /tmp/fail; exit 1
    fi
    if [ -e /tmp/fail-tz ] && [ "$9" = /var/www/dashboard/config/config.php ]; then
      echo 'partial' >"$9"; rm /tmp/fail-tz; exit 1
    fi
    cat "$8" >"$9" ;;
  *) exit 99 ;;
esac
SH
);
chmod('/tmp/sudo', 0700);
chmod('/tmp/systemctl', 0700);
$configs = array('gatewayCallsign' => 'N0OLD', 'ircddbHostname' => 'test.openquad.net', 'url' => 'https://www.qrz.com/db/N0OLD');
$mmdvm = array_fill(0, 19, array());
$mmdvm['General'] = array('Callsign' => 'N0OLD');
$initial = array(
    '/etc/ircddbgateway' => "gatewayCallsign=N0OLD\r\nircddbUsername=N0OLD\r\ndplusLogin=N0OLD   \r\nurl=".$configs['url'],
    '/etc/dstarrepeater' => "callsign=N0OLD  B\ngateway=N0OLD  G\nbeaconText=N0OLD  B\n",
    '/etc/aprsgateway' => "[General]\nCallsign=N0OLD\n[APRS-IS]\nPassword=9999\n[MQTT]\nPassword=private\n",
    '/etc/mmdvmhost' => "[General]\nCallsign=N0OLD\n[Info]\nURL=".$configs['url']."\n[FM]\nCallsign=OTHER\n",
    '/etc/dmrgateway' => "[Info]\r\nURL = \"".$configs['url']."\" ; keep\r\n[Network]\r\nTGRewrite=1,2\r\nTGRewrite=3,4\r\n",
    '/var/www/dashboard/config/ircddblocal.php' => "<?php\n\$callsign   = 'N0OLD'; // preserve\n",
);
foreach ($initial as $path => $bytes) file_put_contents($path, $bytes);
function apply_test_call()
{
    global $configs, $mmdvm;
    callsign_apply(array('confCallsign' => 'qa7test'), 'N0OLD', $configs, $mmdvm, array(),
        array('callsign' => 'N0OLD  B'), array('Pi-Star' => array('Version' => '4.3.9')));
}
touch('/tmp/fail');
try { apply_test_call(); throw new Exception('missing write failure'); }
catch (RuntimeException $e) {
    check($e->getCode() === 500 && strpos($e->getMessage(), 'Cannot install /etc/mmdvmhost') !== false, 'write failure not surfaced');
}
foreach ($initial as $path => $bytes) check(file_get_contents($path) === $bytes, "incomplete rollback: $path");
$log = file_get_contents('/tmp/log');
check(strpos($log, 'restart') === false && substr($log, -strlen("mount -o remount,ro /\n")) === "mount -o remount,ro /\n", 'bad recovery');
unlink('/tmp/log');
try { apply_test_call(); throw new Exception('missing restart failure'); }
catch (RuntimeException $e) {
    check($e->getCode() === 500 && $e->getMessage() === 'Callsign saved; restart failed for mmdvmhost.service.', 'restart failure not surfaced');
}
foreach ($initial as $path => $bytes) {
    $want = str_replace(array('N0OLD   ', 'N0OLD  B', 'N0OLD  G', 'N0OLD', 'Password=9999'),
        array('QA7TEST ', 'QA7TESTB', 'QA7TESTG', 'QA7TEST', 'Password=1188'), $bytes);
    check(file_get_contents($path) === $want, "incorrect bytes after restart failure: $path");
}
$dstar = '/etc/dstarrepeater';
$plan = callsign_targets('QA7TEST', $configs, $mmdvm, array(), array('callsign' => 'N0OLD  B'), array());
$custom = str_replace('beaconText=N0OLD  B', 'beaconText=Welcome to the club', $initial[$dstar]);
check(callsign_rewrite($custom, $dstar, $plan[$dstar], 'QA7TEST') ===
    "callsign=QA7TESTB\ngateway=QA7TESTG\nbeaconText=Welcome to the club\n", 'custom D-Star beacon was overwritten');
check(!glob('/tmp/pistar-cfg-*'), 'staging residue');
echo "PASS partial install rollback; RO restore; saved identity, auto URL and APRS after restart failure; custom D-Star beacon preserved\n";

require '/qa/full-apply.php';
$path='/etc/dmrgateway';
$raw="[Info]\r\nLocation = \"Old\" ; site\r\n[DMR Network 1]\r\n; custom\r\nTGRewrite0=91\r\nTGRewrite0=93\r\nDebug=off";
file_put_contents($path,$raw);
// Simulate the lossy serializer used during explicit platform provisioning.
// It retains only the last duplicate and turns `off` into an empty string.
file_put_contents('/tmp/serialized',"[Info]\nLocation=\"New Town\"\n[DMR Network 1]\nTGRewrite0=93\nDebug=\n");
apply_publish_ini('/tmp/serialized',$path);
check(file_get_contents($path) === str_replace('"Old"','"New Town"',$raw),'provisioning destroyed unrelated physical lines');
$log = file_get_contents('/tmp/log');
file_put_contents('/tmp/serialized',"[Info]\nLocation=\"New Town\"\n[DMR Network 1]\nTGRewrite0=93\nDebug=\n");
apply_publish_ini('/tmp/serialized',$path);
check(file_get_contents('/tmp/log') === $log,'no-op provisioning reinstalled config');
function config_writer_allowed_paths() {return array('/etc/ircddbgateway');}
$form='<form id="config"><input name="confDesc1" value="Old"><input name="toggle" value="OFF" type="hidden">'.
    '<input type="checkbox" name="toggle" value="ON" checked><select name="zone"><option>UTC</option></select>';
$stamp=apply_stamp($form,array($path));
$post=array('apply_snapshot'=>$stamp,'confDesc1'=>'New Town','toggle'=>'ON','zone'=>'UTC');
list($dirty,$hashes)=apply_changes($post);
check($dirty === array('confDesc1'=>'New Town'),'bad full form delta');
try {apply_changes(array_merge($post,array('apply_snapshot'=>$stamp.'x'))); throw new Exception('accepted modified snapshot');}
catch (RuntimeException $e) {check($e->getCode()===409,'wrong snapshot error');}
file_put_contents($path,$raw."\n; concurrent edit");
$before=array($path=>array('Info'=>array('Location'=>'Old')));
$after=array($path=>array('Info'=>array('Location'=>'New Town')));
try {apply_commit_models($before,$after,$hashes); throw new Exception('accepted stale form');}
catch (RuntimeException $e) {check($e->getCode()===409,'wrong stale file error');}
check(file_get_contents('/tmp/log')===$log && file_get_contents($path)===$raw."\n; concurrent edit",'stale form had side effects');
echo "PASS full form delta, tamper/stale rejection without writes, legacy INI merge with CRLF/comments/duplicate TGRewrite0, no-op install skipped\n";

$php='/var/www/dashboard/config/config.php';
$original="<?php\r\n  date_default_timezone_set( 'UTC' ); // keep\r\n";
file_put_contents('/etc/timezone',"UTC\n");
symlink('/usr/share/zoneinfo/UTC','/etc/localtime');
file_put_contents($php,$original);
$hashes=array('/etc/timezone'=>hash_file('sha256','/etc/timezone'),$php=>hash_file('sha256',$php));
unlink('/tmp/log');
touch('/tmp/fail-tz');
try {apply_commit_models(array(),array(),$hashes,'Europe/Berlin'); throw new Exception('accepted partial timezone write');}
catch (RuntimeException $e) {check($e->getCode()===500 && strpos($e->getMessage(),"Cannot install $php")!==false,'wrong timezone failure');}
check(readlink('/etc/localtime')==='/usr/share/zoneinfo/UTC' && file_get_contents('/etc/timezone')==="UTC\n" &&
    file_get_contents($php)===$original,'failed timezone update was not rolled back');
$log=file_get_contents('/tmp/log');
check(strpos($log,'timedatectl set-timezone Europe/Berlin')!==false && strpos($log,'timedatectl set-timezone UTC')!==false &&
    substr($log,-strlen("mount -o remount,ro /\n"))==="mount -o remount,ro /\n",'timezone cleanup incomplete');
try {apply_commit_models(array(),array(),$hashes,'Not/AZone'); throw new Exception('invalid timezone accepted');}
catch (RuntimeException $e) {check($e->getCode()===400 && file_get_contents('/tmp/log')===$log,'invalid zone had effects');}
echo "PASS invalid zone rejected; partial dashboard install restored UTC link, both files and read-only mount\n";

$old='/etc/dstar-radio.mmdvmhost'; $new='/etc/dstar-radio.dstarrepeater';
file_put_contents($old,"[Modem]\nHardware=test\n");
$inode=fileinode($old); $hashes=array($old=>hash_file('sha256',$old));
unlink('/tmp/log');
file_put_contents($new,'preexisting');
try {apply_controller_switch('DSTAR',$hashes); throw new Exception('accepted ambiguous markers');}
catch (RuntimeException $e) {check($e->getCode()===409 && !file_exists('/tmp/log') && file_get_contents($new)==='preexisting','preflight had effects');}
unlink($new);
try {apply_controller_switch('DSTAR',$hashes); throw new Exception('accepted failed D-Star start');}
catch (RuntimeException $e) {check($e->getCode()===500 && strpos($e->getMessage(),'Cannot start dstarrepeater')!==false,'wrong handoff error');}
check(!file_exists($new) && fileinode($old)===$inode && hash_file('sha256',$old)===$hashes[$old],'rollback lost marker');
check(file('/tmp/log',FILE_IGNORE_NEW_LINES)===array('mount -o remount,rw /','systemctl stop mmdvmhost.service',
    "mv -nT -- $old $new",'sync','systemctl start dstarrepeater.service','systemctl stop dstarrepeater.service',
    "mv -nT -- $new $old",'sync','systemctl start mmdvmhost.service','mount -o remount,ro /'),'unsafe controller rollback order');
echo "PASS ambiguous markers rejected without writes; failed D-Star activation restored original marker and service before remount-ro\n";

// First setup has no old controller to restart on failure.
unlink($old); unlink('/tmp/log');
file_put_contents('/tmp/systemctl',"#!/bin/sh\nexit 3\n");
symlink('/does-not-exist',$new);
try {apply_controller_switch('DSTAR',array()); throw new Exception('accepted dangling initial marker');}
catch (RuntimeException $e) {check($e->getCode()===409 && is_link($new) && !file_exists('/tmp/log'),'stale first setup had effects');}
unlink($new);
try {apply_controller_switch('DSTAR',array()); throw new Exception('accepted failed first activation');}
catch (RuntimeException $e) {check($e->getCode()===500 && strpos($e->getMessage(),'Cannot start dstarrepeater')!==false,'wrong first setup failure');}
check(!file_exists($old) && !file_exists($new) && file('/tmp/log',FILE_IGNORE_NEW_LINES)===array(
    'mount -o remount,rw /',"touch $new",'sync','systemctl start dstarrepeater.service',
    'systemctl stop dstarrepeater.service',"rm -f -- $new",'sync','mount -o remount,ro /'),'incorrect first setup rollback');
echo "PASS stale first setup refused; failed initial D-Star start removed only the new marker, without starting MMDVMHost\n";
