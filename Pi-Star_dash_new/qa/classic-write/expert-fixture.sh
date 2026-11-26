#!/bin/sh
# Run as root in the disposable PHP container. /work/DMRGateway.ini is
# copied from image/stage-mmdvm/00-install/files by the host runner.
set -eu
test -e /.dockerenv
test -f /var/www/dashboard/admin/expert/edit_dmrgateway.php
test -r /etc/pistar-release
install -m 644 /work/DMRGateway.ini /etc/dmrgateway
cat >>/etc/dmrgateway <<'INI'

[DMR Network 2]
# Standby network: both rewrite rules are significant to DMRGateway.
Enabled=0
Name="QA standby"
Address=standby.invalid
Port=62031
TGRewrite=1,91,1,91,1
TGRewrite=2,92,2,92,1
Options="StartRef=4000;RelinkTime=15"
Debug=off
INI
