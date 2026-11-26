// Build the ordinary full-form planner from the pinned upstream field rules.
// These spans update daemon models/flat fields only. An existing controller
// has a dedicated handoff; initial hardware provisioning remains separate.
export function fullApply(page) {
  const oldMarkerBlock = page.indexOf('    // Change Radio Control Software');
  const nextBlock = page.indexOf('    // HostAP',oldMarkerBlock);
  if (oldMarkerBlock < 0 || nextBlock < 0) throw Error('Missing controller provisioning block');
  // All controller marker writes, including first setup, go through the
  // locked controller transaction rather than the legacy service lifecycle.
  page = page.slice(0,oldMarkerBlock)+page.slice(nextBlock);
  // Dispatch the independent PSK form before the full Apply's rw window.
  const pskStart = page.indexOf('    // AutoAP PSK Change.');
  const pskEnd = page.indexOf('    // Stop Cron', pskStart);
  if (pskStart < 0 || pskEnd < 0) throw Error('Missing stock Auto AP handler');
  page = (page.slice(0,pskStart)+page.slice(pskEnd))
    .replace('onkeyup="checkPsk(); return false;"','oninput="checkPsk(); checkPskMatch();"')
    .replace('name="autoapPsk" id="psk2" onkeyup="checkPskMatch(); return false;"',
             'name="autoapPskConfirm" id="psk2" oninput="checkPskMatch();"');
  const rules = [
    ['Change Dashboard Language.', 'Set the ircDDBGateway Remote Password and Port.'],
    ['Set the ircDDBGateway Defaut Reflector', 'Set random (working) CCS host.'],
    ['Set the Latitude','Set the URL.'],
    ['Set the URL.', 'Set the APRS Host'],
    ['Set the APRS Host','Clear timeserver modules'],
    ['Set the POCSAG Frequency','Set the Frequency for Duplex'],
    ['Set the ircDDB Callsign routing option.','Remove hostfiles.ysfupper'],
    ['Set the YSF2DMR Starting TG','Set Duplex'],
    ['Set DMR / CCS7 ID','Set DMR Master Server'],
    ['Set Talker Alias Option','Remove old settings'],
    ['Set DMR Hang Timers','Set the hardware type'],
    ['Set the MMDVMHost Display Type','Set the Hostname'],
  ];
  const mark = name => {
    const i = page.indexOf('\n    // '+name+'\n');
    const offset = i >= 0 ? i : page.indexOf('\n    // '+name);
    if (offset < 0) throw Error('Unknown stock setting block: '+name);
    return offset;
  };
  const names = new Set(['systemTimezone']), chunks = [];
  const writeStart = mark('Make the root filesystem writable');
  const writeEnd = mark('MMDVMHost config file wrangling');
  let cursor = writeStart;
  const provision = text => `\nif (!$dataApply) {\n${text}\n}\n`;
  for (const [start,end] of rules) {
    const a = mark(start), b = mark(end);
    if (a < cursor || b <= a || b > writeEnd) throw Error('Overlapping full Apply rules');
    if (a !== cursor) chunks.push(provision(page.slice(cursor,a)));
    cursor = b;
    // Each leading "Set ..." comment is an independent stock mapping, with
    // the complete submitted values available for companion controls.
    for (const block of page.slice(mark(start),mark(end)).split(/(?=\n    \/\/ Set )/)) {
      const code = block.replace(/^\s*\/\/.*$/gm,'');
      const keys = [...new Set([...code.matchAll(/\$_POST\['(\w+)'\]/g)].map(m=>m[1]))];
      if (!keys.length) { chunks.push(block); continue; }
      if (/\b(?:exec|system|unlink|file_put_contents|config_writer_stage_privileged_flat)\s*\(/.test(code))
        throw Error('Platform action in ordinary Apply planner: '+keys);
      // confCallsign supplies context for reflector and automatic URL rules;
      // an actual identity change must run the station identity workflow.
      const triggers = keys.filter(k=>k!=='confCallsign');
      triggers.forEach(k=>names.add(k));
      const literal = triggers.map(k=>`'${k}'`).join(',');
      chunks.push(`\nif (!$dataApply || array_intersect_key($dirty,array_flip(array(${literal})))) {\n${block}\n}\n`);
    }
  }
  chunks.push(provision(page.slice(cursor,writeEnd)));
  // The mappings above execute in their original order, in the real full POST
  // handler. No duplicate location handler, client dirty flags or special URL.
  chunks.push(`
    if ($dataApply) {
        ob_end_clean();
        try {
            apply_commit_models($before,apply_models(),$fileHashes,$dirty['systemTimezone'] ?? null,$dirty['dashboardLanguage'] ?? null,$modem);
            if ($switchMode !== null) apply_controller_switch($switchMode,$fileHashes);
            header('Location: /admin/configure.php',true,303);
        } catch (RuntimeException $e) {
            http_response_code($e->getCode());
            echo htmlspecialchars($e->getMessage(), ENT_QUOTES,'UTF-8');
        }
        exit;
    }
`);
  page = page.slice(0,writeStart)+chunks.join('')+page.slice(writeEnd);
  const form = '<form id="config"';
  const start = page.indexOf(form), end = page.indexOf('    </form>', start);
  if (start<0 || end<0) throw Error('Missing primary config form');
  page = page.slice(0,start)+'<?php ob_start(); ?>'+page.slice(start,end)+
    `<?php $form = ob_get_clean(); echo $form; ?>
    <input type="hidden" name="apply_snapshot" value="<?php echo apply_stamp($form,apply_paths()); ?>" />
`+page.slice(end);
  const init = `require_once __DIR__.'/full-apply.php';
if (!class_exists('DOMDocument')) {
    http_response_code(503);
    exit('The configuration form requires the PHP DOM extension (php-xml).');
}
$dataApply = false;
if ($_SERVER['PHP_SELF'] === '/admin/configure.php' && $_SERVER['REQUEST_METHOD'] === 'POST'
    && (isset($_POST['autoapPsk']) || isset($_POST['autoapPskConfirm']))) {
    try {
        apply_auto_ap_psk($_POST);
        header('Location: /admin/configure.php',true,303);
    } catch (RuntimeException $e) {
        http_response_code($e->getCode());
        echo '<p role="alert">'.htmlspecialchars($e->getMessage(),ENT_QUOTES,'UTF-8').'</p>'
            .'<a href="/admin/configure.php">Return to configuration</a>';
    }
    exit;
}
if ($_SERVER['PHP_SELF'] === '/admin/configure.php' && $_SERVER['REQUEST_METHOD'] === 'POST'
    && !(count($_POST) === 1 && (!empty($_POST['adminPassword']) || !empty($_POST['factoryReset'])))) {
    try {
        list($dirty,$fileHashes) = apply_changes($_POST);
        unset($_POST['apply_snapshot']);
        $switchMode = null;
        $firstController = !isset($fileHashes[$modemConfigFileMMDVMHost]) && !isset($fileHashes[$modemConfigFileDStarRepeater]);
        if ($firstController || isset($dirty['controllerSoft'])) {
            // With no markers, even the form's default radio needs to be initialized.
            $switchMode = $_POST['controllerSoft'] ?? null;
            if (!in_array($switchMode,array('MMDVM','DSTAR'),true))
                throw new RuntimeException('Unknown radio controller.',400);
            unset($dirty['controllerSoft']);
        }
        $modem = null;
        // This MMDVM UART profile needs no network, timer or D-Star provisioning.
        if (($dirty['confHardware'] ?? null) === 'zumspotusb' && $switchMode === null &&
            isset($fileHashes[$modemConfigFileMMDVMHost]) && !isset($fileHashes[$modemConfigFileDStarRepeater])) {
            $modem = $dirty['confHardware'];
        }
        $provisioning = array_diff_key($dirty,array_flip(array(${[...names].map(k=>`'${k}'`).join(',')})));
        if ($modem !== null) unset($provisioning['confHardware']);
        $dataApply = !$provisioning;
        if (!$dataApply && $switchMode !== null)
            throw new RuntimeException('Apply the controller switch separately from provisioning changes.',409);
        if ($dataApply) {
            if (!is_array($configmmdvm) || count($configmmdvm) <= 18) throw new RuntimeException('Unreadable MMDVMHost configuration.',409);
            $before = apply_models();
            if ($modem !== null) {
                $configmmdvm['Modem']['Port'] = $configmmdvm['Modem']['UARTPort'] = '/dev/ttyACM0';
                $configmmdvm['Modem']['Protocol'] = 'uart';
                $configmmdvm['General']['Duplex'] = $configmmdvm['DMR Network']['Slot1'] = '0';
                if (isset($configmmdvm['FM']['Enable'])) $configmmdvm['FM']['Enable'] = '0';
            }
            ob_start();
        }
    } catch (RuntimeException $e) {
        http_response_code($e->getCode());
        echo htmlspecialchars($e->getMessage(), ENT_QUOTES,'UTF-8');
        exit;
    }
}
`;
  const serializer = /exec\('sudo install -m 644 -o root -g root (\/tmp\/[a-zA-Z0-9]+\.tmp) (\/etc\/[a-z0-9]+)'\);/g;
  let writers = 0;
  page = page.replace(serializer, (_,tmp,dst) => {
    writers++;
    // This workflow already owns the rw window / service lifecycle. A write
    // failure must reach its cleanup rather than throwing past the ro remount.
    return `if (empty($iniError)) {
        try { apply_publish_ini('${tmp}','${dst}'); }
        catch (RuntimeException $e) { $iniError = $e->getMessage(); error_log('configure.php: '.$iniError); }
    }`;
  });
  if (writers !== 13) throw Error(`Review the ${writers} stock daemon INI writers`);
  // Explicit provisioning still has its own rw window. Do not reapply an
  // unchanged timezone just because the full form contains the select.
  page = page.replace("if (empty($_POST['systemTimezone']) != TRUE)",
                      "if (isset($dirty['systemTimezone']))");
  const reload = `echo '<script type="text/javascript">setTimeout(function() { window.location=window.location;},7500);</script>';`;
  if (!page.includes(reload)) throw Error('Missing full Apply completion');
  page = page.replace(reload, () => `if (!empty($iniError)) {
        echo '<p role="alert">Configuration save failed: '.htmlspecialchars($iniError,ENT_QUOTES,'UTF-8').'</p>';
    } else ${reload}`);
  return page.replace('$progname = basename', () => init+'$progname = basename');
}
