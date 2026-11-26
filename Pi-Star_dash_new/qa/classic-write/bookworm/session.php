<?php
require '/qa-write/gate.php';
// Single-worker fixture: recreate PHP views from native storage on every
// editor request, including the independent navigation after Apply.
if (in_array($_SERVER['PHP_SELF'], ['/admin/configure.php','/admin/expert/edit_dmrgateway.php'],true)) {
    exec('sudo refresh-native',$unused,$rc);
    if ($rc !== 0) { http_response_code(503); exit('Native settings unavailable'); }
}
