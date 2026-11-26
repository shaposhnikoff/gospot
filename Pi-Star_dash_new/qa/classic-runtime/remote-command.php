#!/usr/local/bin/php
<?php
// Mounted in place of the gateway client only in the read-only QA container.
$receipt = json_encode(['cwd'=>getcwd(), 'argv'=>$argv], JSON_UNESCAPED_SLASHES | JSON_THROW_ON_ERROR);
if (file_put_contents('/tmp/remote-command-audit', $receipt."\n", FILE_APPEND | LOCK_EX) === false) exit(1);
echo "QA recorded: ".$receipt."\n";
