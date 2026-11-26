#!/bin/sh
set -eu
php -v | head -1
mkdir /tmp/sessions
if [ "$1" = mmdvmhost ]; then
  cp /qa/fixtures/MMDVM.txt "/var/log/pi-star/MMDVM-$(date -u +%F).log"
else
  echo 'D-Star fixture: before polling' >"/var/log/pi-star/DStarRepeater-$(date -u +%F).log"
fi
if [ "$1" = dstarrepeater ]; then
  # These are the two sorted/filtered caches the D-Star partials consume.
  # For one local row their content is identical to the input header.
  cp /var/log/pi-star/Headers.log /tmp/lastheard.log
  cp /var/log/pi-star/Headers.log /tmp/worked.log
fi
address=127.0.0.1
if [ "${2:-}" = --pocsag ]; then
  echo 'M: 2020-01-02 12:00:15.000 Sending message in slot 3 to 1234567, "QA pager message"' >"/var/log/pi-star/DAPNETGateway-$(date -u +%F).log"
fi
case "${2:-}" in --operator-browser|--tgif-manager|--bm-manager) address=0.0.0.0 ;; esac
disabled=exec,shell_exec,system,passthru,popen,pcntl_exec,dl
if [ "${QA_REMOTE_MANAGER:-}" = 1 ]; then
  : >/tmp/remote-command-audit
else
  disabled=$disabled,proc_open
fi
urls=0
if [ "${2:-}" = --bm-manager ]; then
  urls=1
  php /qa/bm-fixture.php &
fi
if [ "${2:-}" = --tgif-static ] || [ "${2:-}" = --tgif-manager ]; then
  urls=1  # api.tgif.network resolves to the container's own TLS listener.
  php /qa/tgif-fixture.php "$2" &
  api_pid=$!
fi
if [ "${QA_CATALOG:-}" = 1 ]; then disabled=$disabled,pclose; fi
php -n -d disable_functions="$disabled" \
  -d auto_prepend_file=/qa/probes.php -d allow_url_fopen="$urls" \
  -d session.save_path=/tmp/sessions -d output_buffering=4096 \
  -d memory_limit=64M -d display_errors=0 -d log_errors=1 \
  -d max_execution_time=4 -d default_socket_timeout=1 -S "$address:8080" \
  >/tmp/server.log 2>&1 &
pid=$!
trap 'kill "$pid" 2>/dev/null || true' EXIT
if [ "$address" = 0.0.0.0 ]; then wait "$pid"; exit; fi
errors='PHP (Fatal|Parse) error|PHP (Warning|Notice|Deprecated):'
if ! php /qa/check.php "$1" "${2:-}" || grep -Eq "$errors" /tmp/server.log; then
  grep -E "$errors" /tmp/server.log | tail -10 >&2
  exit 1
fi
echo "PHP warning/fatal/parse log check: clean"
if [ "${2:-}" = --tgif-static ]; then wait "$api_pid"; fi
