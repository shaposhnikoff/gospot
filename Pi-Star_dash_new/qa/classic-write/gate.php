<?php
// CLI SAPI's development server has no Apache admin auth. Supply only that
// gate; the actual application bootstraps and verifies its own CSRF session.
if (($_SERVER['PHP_AUTH_USER'] ?? '') !== 'qa' ||
    !hash_equals(trim(file_get_contents('/run/qa-basic')), $_SERVER['PHP_AUTH_PW'] ?? '')) {
    header('WWW-Authenticate: Basic realm="Disposable dashboard QA"');
    http_response_code(401);
    exit;
}
$route = $_SERVER['REQUEST_URI'] ?? '';
if (parse_url($route, PHP_URL_PATH) === '/admin/configure.php') $route = '/admin/configure.php';
if (in_array($route, ['/admin/expert/edit_p25gateway.php','/admin/expert/fulledit_apikeys.php','/admin/expert/fulledit_tgifapikey.php','/admin/expert/fulledit_dapnetapi.php','/admin/expert/fulledit_bmapikey.php','/admin/expert/fulledit_cron.php','/admin/wifi.php?page=wpa_conf','/admin/expert/fulledit_rssidat.php','/admin/expert/fulledit_pistar-remote.php','/admin/expert/fulledit_wpaconfig.php','/admin/expert/fulledit_dmrgateway.php','/admin/expert/edit_dmrgateway.php','/admin/expert/edit_nxdngateway.php','/admin/expert/edit_dapnetgateway.php','/admin/expert/edit_mmdvmhost.php','/admin/expert/edit_dstarrepeater.php','/admin/expert/edit_ircddbgateway.php','/admin/expert/edit_timeserver.php','/admin/expert/edit_starnetserver.php','/admin/config_backup.php','/admin/configure.php','/admin/power.php','/admin/update.php','/admin/expert/upgrade.php'], true)) {
    $token = $_POST['csrf_token'] ?? null;
    if ($route === '/admin/configure.php' && $_SERVER['REQUEST_METHOD'] === 'POST') {
        // Record only the synthetic station ID and count, never other form values.
        file_put_contents('/tmp/qa-configure-receipt', json_encode([
            $_POST['confCallsign'] ?? null, count($_POST)
        ]));
    }
    if ($route === '/admin/config_backup.php' && ($_POST['action'] ?? null) === 'restore' &&
        is_uploaded_file($_FILES['fileToUpload']['tmp_name'] ?? '')) {
        file_put_contents('/tmp/qa-upload.sha256', hash_file('sha256', $_FILES['fileToUpload']['tmp_name']));
    }
    register_shutdown_function(static function () use ($token, $route) {
        $registered = $_SESSION['csrf_token'] ?? null;
        $valid = is_string($registered) && is_string($token) && hash_equals($registered, $token);
        file_put_contents('/tmp/qa-flow', json_encode([$route, $_SERVER['REQUEST_METHOD'],
            http_response_code(), substr(hash('sha256', session_id()), 0, 16), $valid]) . "\n", FILE_APPEND|LOCK_EX);
    });
}
