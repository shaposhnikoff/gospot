<?php
// A browser serializes textarea line breaks as CRLF whatever it read on GET.
function pistar_full_edit_text($original, $posted)
{
    $pattern = '/\r\n|\r|\n/';
    $old = preg_split($pattern, $original);
    $new = preg_split($pattern, $posted);
    preg_match_all($pattern, $original, $matches);
    $ends = $matches[0];
    $delta = count($new) - count($old);
    // Anchor the common tail so inserting/deleting lines doesn't shift the
    // original endings of subsequent lines in a mixed-ending configuration.
    $tail = count($new);
    while ($delta && $tail > max(0, $delta) && $new[$tail-1] === $old[$tail-1-$delta]) --$tail;
    $out = '';
    foreach ($new as $i => $line) {
        $out .= $line;
        if ($i + 1 < count($new)) {
            $index = $i >= $tail ? $i - $delta : $i;
            $out .= $ends[$index] ?? ($ends[0] ?? "\n");
        }
    }
    return $out;
}
