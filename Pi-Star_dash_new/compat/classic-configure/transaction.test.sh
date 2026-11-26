#!/bin/sh
# Fault injection against disposable tmpfs targets. The production PHP helper
# is read-only; neither the host /etc nor a real service manager is available.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges \
    --user 33:33 --tmpfs /etc:rw,uid=33,gid=33,mode=700 \
    --tmpfs /var/www/dashboard/config:rw,uid=33,gid=33,mode=700 \
    --tmpfs /tmp:rw,exec,uid=33,gid=33,mode=700 \
    --mount "type=bind,source=$here,target=/qa,readonly" \
    -e CALLSIGN_FIXTURE=tmpfs pstar-classic-write-runtime:8.2 php -n /qa/transaction.test.php
