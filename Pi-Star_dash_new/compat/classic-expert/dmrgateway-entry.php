<?php
// DMRGateway consumes physical lines, including repeated rules. Keep the
// original byte stream; expose only values whose boundaries are unambiguous.
function dmrgw_rows($text, $flatSection = null)
{
    $rows = array();
    // For flat key=value files the section belongs only to the HTML form.
    // Keep all value offsets relative to the actual file, without a fake header.
    $section = $flatSection;
    $sectionLine = 0;
    foreach (preg_split('/\r\n|\n|\r/', $text, -1, PREG_SPLIT_OFFSET_CAPTURE) as $i => $pair) {
        list($line, $at) = $pair;
        if ($i === 0 && substr($line, 0, 3) === "\xef\xbb\xbf") {
            $line = substr($line, 3);
            $at += 3;
        }
        if (preg_match('/^\h*\[/', $line)) {
            $section = preg_match('/^\[([^\]\x00-\x1f]+)\][ \t]*(?:[;#].*)?$/D', $line, $m)
                ? $m[1] : null;
            $sectionLine = $i + 1;
            continue;
        }
        if ($section === null || !preg_match('/^([ \t]*([A-Za-z0-9_.-]+)[ \t]*=)(.*)$/D', $line, $m))
            continue;
        list(, $prefix, $key, $rhs) = $m;
        // A quoted value can contain comment characters. Ambiguous quoting
        // and unquoted semicolon-delimited options are left untouched.
        if (!preg_match('~^([ \t]*)("([^"]*)"|\'([^\']*)\'|([^"\'#;]*?))([ \t]*(?:[#;].*)?)$~D', $rhs, $v))
            continue;
        list(, $leading, $token) = $v;
        $quote = substr($token, 0, 1);
        if ($quote !== '"' && $quote !== "'") $quote = '';
        if ($quote === '' && preg_match('/^;/', $v[6]) && $token !== '') continue;
        $value = $quote === '' ? $token : substr($token, 1, -1);
        if (!preg_match('//u', $value) || preg_match('/[\x00-\x1f\x7f]/', $value)) continue;
        // If the value is empty, leave existing whitespace with its comment.
        $offset = $at + strlen($prefix) + ($token === '' ? 0 : strlen($leading)) + strlen($quote);
        $rows[$i + 1] = array('section' => $section, 'sectionLine' => $sectionLine, 'key' => $key, 'value' => $value,
            'offset' => $offset, 'quote' => $quote, 'length' => strlen($line));
    }
    return $rows;
}

function dmrgw_edit($text, $post, $body, $flatSection = null)
{
    // csrf_verify() has already validated and removed csrf_token.
    if (count($post) !== 3 || !isset($post['revision'], $post['values'], $post['complete'])
        || !is_string($post['revision']) || !is_array($post['values']) || $post['complete'] !== '1')
        throw new RuntimeException('Incomplete or invalid DMRGateway form.', 400);
    if (!hash_equals(hash('sha256', $text), $post['revision']))
        throw new RuntimeException('DMRGateway has changed. Reload the editor and try again.', 409);
    $rows = dmrgw_rows($text, $flatSection);
    dmrgw_check_fields($body, $rows);
    if (count($post['values']) !== count($rows))
        throw new RuntimeException('Missing or unexpected DMRGateway entries.', 400);
    // Original offsets remain valid when replacing from the end backwards.
    // Everything stays in memory until the entire submission is valid.
    foreach (array_reverse($rows, true) as $line => $row) {
        if (!isset($post['values'][$line]) || !is_string($post['values'][$line]))
            throw new RuntimeException('Invalid DMRGateway entry.', 400);
        $value = $post['values'][$line];
        if ($value === $row['value']) continue;
        if (!preg_match('//u', $value) || preg_match('/[\x00-\x1f\x7f]/', $value)
            || ($row['quote'] !== '' && strpos($value, $row['quote']) !== false)
            || ($row['quote'] === '' && (preg_match('/["\'#;]/', $value) || trim($value) !== $value))
            || $row['length'] - strlen($row['value']) + strlen($value) > 496)
            throw new RuntimeException('Value cannot be safely represented in this entry.', 400);
        $text = substr_replace($text, $value, $row['offset'], strlen($row['value']));
    }
    return $text;
}

function dmrgw_check_fields($body, $rows)
{
    // PHP collapses duplicate names and truncates $_POST at max_input_vars.
    // Check every wire name once, including the end marker after the last row.
    if (!is_string($body) || substr($body, -11) !== '&complete=1' || preg_match('/%(?![0-9a-fA-F]{2})/', $body))
        throw new RuntimeException('Incomplete or invalid DMRGateway form.', 400);
    $expected = array('csrf_token' => true, 'revision' => true, 'complete' => true);
    foreach ($rows as $line => $row) $expected['values['.$line.']'] = true;
    foreach (explode('&', $body) as $part) {
        $field = explode('=', $part, 2);
        $name = urldecode($field[0]);
        if (count($field) !== 2 || !isset($expected[$name]))
            throw new RuntimeException('Unexpected or repeated DMRGateway field.', 400);
        unset($expected[$name]);
    }
    if ($expected) throw new RuntimeException('Incomplete DMRGateway form.', 400);
}

function dmrgw_load_and_apply($flatSection = null)
{
    $saving = $_SERVER['REQUEST_METHOD'] === 'POST';
    if ($saving) {
        // Stable inode shared by this editor's workers; covers staging through
        // install even if two independent authenticated sessions submit at once.
        $lock = fopen(__FILE__, 'r');
        if (!$lock || !flock($lock, LOCK_EX | LOCK_NB))
            throw new RuntimeException('Another edit is in progress. Reload and try again.', 409);
    }
    $path = tempnam('/tmp', 'pistar-edit-');
    if ($path === false) throw new RuntimeException('Could not stage DMRGateway configuration.', 500);
    register_shutdown_function(function () use ($path) { @unlink($path); });
    $tmp = escapeshellarg($path);
    foreach (array('sudo cp /etc/dmrgateway ', 'sudo chown www-data:www-data ', 'sudo chmod 600 ') as $cmd) {
        exec($cmd.$tmp, $output, $status);
        if ($status !== 0) throw new RuntimeException('Could not stage DMRGateway configuration.', 500);
    }
    $text = file_get_contents($path);
    if ($text === false) throw new RuntimeException('Could not read DMRGateway configuration.', 500);
    if (!$saving) return $text;
    $next = dmrgw_edit($text, $_POST, file_get_contents('php://input'), $flatSection);
    if ($text === $next) return $text;
    if ($flatSection !== null) {
        // Match the stock flat editor's separate install source (sudoers).
        $path = tempnam('/tmp', 'pistar-edit-etc-');
        if ($path === false) throw new RuntimeException('Could not stage updated configuration.', 500);
        register_shutdown_function(function () use ($path) { @unlink($path); });
        $tmp = escapeshellarg($path);
    }
    if (file_put_contents($path, $next) !== strlen($next))
        throw new RuntimeException('Could not write staged DMRGateway configuration.', 500);
    exec('sudo mount -o remount,rw /', $output, $status);
    if ($status !== 0) throw new RuntimeException('Could not remount for DMRGateway update.', 500);
    try {
        exec('sudo install -m 644 -o root -g root '.$tmp.' /etc/dmrgateway', $output, $status);
        if ($status !== 0) throw new RuntimeException('Could not install DMRGateway configuration.', 500);
    } finally {
        exec('sudo mount -o remount,ro /', $output, $status);
        if ($status !== 0) throw new RuntimeException('Could not restore read-only mount.', 500);
    }
    exec('sudo systemctl restart dmrgateway.service', $output, $status);
    if ($status !== 0) throw new RuntimeException('Configuration saved; DMRGateway restart failed.', 500);
    return $next;
}

function dmrgw_html($s)
{
    return htmlspecialchars($s, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
}

function dmrgw_form($text, $apply, $flatSection = null)
{
    $sectionLine = null;
    echo '<form method="post" action="" accept-charset="UTF-8">'.csrf_field_html()
        .'<input type="hidden" name="revision" value="'.hash('sha256', $text).'" />'."\n";
    $endSection = "</table>\n".'<input type="submit" value="'.dmrgw_html($apply).'" /><br />'."\n";
    foreach (dmrgw_rows($text, $flatSection) as $line => $row) {
        if ($sectionLine !== $row['sectionLine']) {
            if ($sectionLine !== null) echo $endSection;
            $sectionLine = $row['sectionLine'];
            echo '<table><tr><th colspan="2">'.dmrgw_html($row['section'])."</th></tr>\n";
        }
        echo '<tr><td align="right"><label for="dmrgw-'.$line.'">'.dmrgw_html($row['key']).'</label></td>'
            .'<td><input type="text" id="dmrgw-'.$line.'" name="values['.$line.']" data-section="'.dmrgw_html($row['section'])
            .'" data-key="'.dmrgw_html($row['key']).'" value="'.dmrgw_html($row['value']).'" /></td></tr>'."\n";
    }
    if ($sectionLine !== null) echo $endSection;
    echo '<input type="hidden" name="complete" value="1" /></form>';
}
