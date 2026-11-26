#!/bin/bash
# Inside disposable Docker; do not mount the checkout or Docker's special
# /etc/hosts and /etc/hostname as restore targets. Use a second private root.
set -euo pipefail
test -e /.dockerenv && test ! -e /stand
mkdir -p /stand/{dev,root,run,var/www/dashboard,work,tmp,qa-write}
chmod 1777 /stand/tmp
cp -a /{bin,etc,lib,sbin,usr} /stand/
[[ ! -L /lib64 ]] || cp -a /lib64 /stand/
cp -a /dev/{null,zero,urandom,random} /stand/dev/
cp -a /run/qa-basic /stand/run/
cp -a /tmp/{qa-sessions,qa-server.log} /stand/tmp/
cp -a /var/www/dashboard/. /stand/var/www/dashboard/
suite=${1:-backup}
[[ $suite == backup || $suite == configure ]]
cp -a /qa-write/gate.php "/qa-write/$suite-fixture.sh" /stand/qa-write/
cp -a /work/DMRGateway.ini /work/effects /stand/work/
if [[ $suite == configure ]]; then
  mkdir /stand/qa-base
  cp -a /qa-base/. /stand/qa-base/
  if [[ -d /qa-write/configure ]]; then cp -a /qa-write/configure /stand/qa-write/; fi
fi
install -m755 "/qa-write/$suite-priv" /stand/usr/local/sbin/qa-priv
chroot /stand /bin/bash "/qa-write/$suite-fixture.sh"
