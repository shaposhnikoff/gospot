<?php
require __DIR__.'/dmrgateway-entry.php';

function check($ok, $description)
{
    if (!$ok) throw new Exception($description);
}
function form_body($post)
{
    return http_build_query(array('csrf_token' => 'session-token') + $post);
}
function check_rejected($text, $post, $code, $body = null)
{
    try {
        dmrgw_edit($text, $post, $body === null ? form_body($post) : $body);
    } catch (RuntimeException $e) {
        check($e->getCode() === $code, 'unexpected status');
        return;
    }
    throw new Exception('accepted invalid input');
}
function csrf_field_html() { return '<input name="csrf_token" />'; }

$text = "\xEF\xBB\xBF[General]\r\nEnable = yes  ; keep\nFlag=off\r\n[DMR Network 1]\r\n"
    ."Password=\"q&'<;#=p\" \r\nTGRewrite=1,9,1,9,1\r\n# TGRewrite=disabled\n"
    ."TGRewrite =  2,9,2,9,1  # keep\r\nOptions=\"Start=1;Foo=2\"\n"
    ."Odd=take;raw;bytes\n# legacy byte \xFF\nEmpty=  # note\r\n[DMR Network 1]\nName='café'";
ob_start();
dmrgw_form($text, 'Apply');
$html = ob_get_clean();
check(substr_count($html, '<form ') === 1 && substr_count($html, '</form>') === 1
    && substr_count($html, 'type="submit"') === 3, 'one form shared by physical section Apply buttons');
check(substr_count($html, '>TGRewrite</label>') === 2, 'both rewrite rows rendered');
check(substr_count($html, 'name="csrf_token"') === 1, 'CSRF in form');
check(strpos($html, 'q&amp;&#039;&lt;;#=p') !== false && strpos($html, 'value="yes"') !== false,
    'escaped literal form values');
check(strpos($html, 'data-section="General" data-key="Enable"') !== false, 'input identified by section and key');
check(substr($html, -strlen('name="complete" value="1" /></form>')) === 'name="complete" value="1" /></form>',
    'end-of-form truncation marker');
preg_match('/name="revision" value="([0-9a-f]{64})"/', $html, $revision);
preg_match_all('/name="values\[(\d+)\]"[^>]* value="([^"]*)"/', $html, $inputs, PREG_SET_ORDER);
$values = array();
foreach ($inputs as $input) $values[$input[1]] = html_entity_decode($input[2], ENT_QUOTES, 'UTF-8');
$post = array('revision' => $revision[1], 'values' => $values, 'complete' => '1');
check(dmrgw_edit($text, $post, form_body($post)) === $text, 'unchanged form is byte-identical');
$changed = $post;
$changed['values'][2] = 'no';
$changed['values'][8] = '2,12345,2,8,100';
$expected = str_replace(array('= yes', '2,9,2,9,1'), array('= no', '2,12345,2,8,100'), $text);
check(dmrgw_edit($text, $changed, form_body($changed)) === $expected,
    'one Apply changes two sections, targeting only second repeated directive');
$changed['values'][5] = 'better<&;#';
$changed['values'][12] = 'x';
$changed['values'][14] = '東京';
$expected = str_replace(array("q&'<;#=p", 'Empty= ', 'café'), array('better<&;#', 'Empty=x ', '東京'), $expected);
check(dmrgw_edit($text, $changed, form_body($changed)) === $expected, 'multiple length-changing edits preserve offsets');
check_rejected($text."\n", $changed, 409);
foreach (array(array(10, 'skip'), array(7, 'comment'), array(2, array('bad')), array(2, "x\r\nOther=bad"),
    array(2, "x\x00"), array(2, 'oops#comment'), array(2, str_repeat('x', 500)), array(5, 'invalid"')) as $bad) {
    $invalid = $changed;
    $invalid['values'][$bad[0]] = $bad[1];
    check_rejected($text, $invalid, 400);
}
$body = form_body($changed);
check_rejected($text, $changed, 400, 'values%5B8%5D=x&'.$body);
check_rejected($text, $changed, 400, 'revision=x&'.$body);
check_rejected($text, $changed, 400, substr($body, 0, -15));
$missing = $changed;
unset($missing['values'][3]);
check_rejected($text, $missing, 400);         // missing control on wire
check_rejected($text, $missing, 400, $body); // PHP dropped a posted value
unset($missing['complete']);
check_rejected($text, $missing, 400, $body); // max_input_vars truncation
echo "DMRGateway multi-field Apply, byte preservation and rejection tests passed\n";
