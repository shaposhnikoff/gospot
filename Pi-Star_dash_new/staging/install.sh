#!/bin/bash
# Run through scripts/deploy-pistar3.
set -euo pipefail
[[ $(hostname -s) == pistar3 && $EUID == 0 ]]
src=$(cd "$(dirname "$0")" && pwd)
base=/opt/gospot-stage
svc=gospot-stage.service
acct=gospot-stage
unit=/etc/systemd/system/$svc
action=$1
exec 9>/run/lock/gospot-stage.lock
flock -x 9

radio_ids() {
    for s in mmdvmhost dmrgateway; do
        local id
        id=$(systemctl show -p InvocationID --value "$s") || return 1
        [[ $id =~ ^[0-9a-f]{32}$ ]] || return 1
        echo "$s $id"
    done
}
before=$(radio_ids)
printf 'Before:\n%s\n' "$before"
finish() {
    local rc=$? after
    trap - EXIT
    after=$(radio_ids) || rc=1
    printf 'After:\n%s\n' "$after"
    [[ $before == "$after" ]] || { echo "Radio invocation ID changed" >&2; rc=1; }
    if [[ -n ${work:-} ]]; then rm -rf -- "$work"; fi
    exit "$rc"
}
trap finish EXIT

if [[ ! -f $base/.managed ]]; then
    if [[ $action == --remove && ! -e $base && ! -e $unit ]] && ! id "$acct" &>/dev/null; then exit 0; fi
    [[ $action != --remove && $action != --rollback && ! -e $base && ! -e $unit ]]
    ! getent passwd "$acct" >/dev/null || exit 1
    ! getent group "$acct" >/dev/null || exit 1
    [[ $(systemctl show "$svc" -p LoadState --value) == not-found ]]
    [[ -z $(ss -H -ltn 'sport = :8789') ]] || { echo 'Port 8789 occupied' >&2; exit 1; }
    install -d -m 0755 "$base" "$base/releases"
    touch "$base/.managed"
fi
if [[ $action == --remove ]]; then
    if [[ -f $unit ]]; then systemctl disable --now "$svc"; fi
    rm -f -- "$unit"
    systemctl daemon-reload
    rm -rf -- "$base"
    if getent passwd "$acct" >/dev/null; then userdel "$acct"; fi
    if getent group "$acct" >/dev/null; then groupdel "$acct"; fi
    exit
fi
if ! id "$acct" &>/dev/null; then
    useradd --system --user-group --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin "$acct"
fi
old=$(readlink "$base/current" || :)
if [[ $action == --rollback ]]; then
    target=$(readlink "$base/previous") || { echo "No previous staging release" >&2; exit 1; }
elif [[ $action == deploy || $action == --server-only ]]; then
    work=$(mktemp -d "$base/releases/.build.XXXXXXXX")
    chmod 755 "$work"
    install -m 0644 "$src/server.py" "$src/$svc" "$work/"
    if [[ $action == deploy ]]; then
        cp -R "$src/../dist" "$work/"
    elif [[ -n $old ]]; then
        cp -R "$base/$old/dist" "$work/"
    else
        install -D -m 0644 "$src/pending.html" "$work/dist/index.html"
    fi
    [[ -z $(find "$work/dist" ! -type f ! -type d -print -quit) && -s $work/dist/index.html ]]
    chmod -R u=rwX,go=rX "$work/dist"
    digest=$(cd "$work" && find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum | sha256sum)
    target="releases/${digest%% *}"
    if [[ ! -d $base/$target ]]; then mv -T "$work" "$base/$target"; work=; fi
else
    exit 2
fi

link_release() {
    ln -sfn "$1" "$base/.$2.new"
    mv -Tf "$base/.$2.new" "$base/$2"
}
activate() {
    local release=$1
    link_release "$release" current || return 1
    if ! cmp -s "$base/$release/$svc" "$unit"; then
        install -m 0644 "$base/$release/$svc" "$unit" && systemctl daemon-reload || return 1
    fi
    systemctl enable "$svc" && systemctl restart "$svc"
}
if [[ $target != "$old" ]] || ! systemctl is-active --quiet "$svc" || ! cmp -s "$base/$target/$svc" "$unit"; then
    if ! activate "$target"; then
        if [[ -n $old && $old != "$target" ]]; then activate "$old"; fi
        echo "Staging activation failed" >&2
        exit 1
    fi
fi
url=http://192.168.210.194:8789
check() {
    curl --noproxy '*' -fsS --max-time 5 --retry 4 --retry-connrefused --retry-delay 1 "$url/runtime.json" |
        python3 -c 'import json,sys; d=json.load(sys.stdin); p=d.pop("classicHttpsPort",None); sys.exit(d!={"backend":"gospot-go","wizardPort":8788} or (p is not None and not (type(p) is int and 1<=p<=65535)))' &&
    curl --noproxy '*' -fsS --max-time 5 "$url/api/status" |
        python3 -c 'import json,sys; sys.exit(not isinstance(json.load(sys.stdin),dict))' &&
    curl --noproxy '*' -fsS --max-time 5 "$url/" | cmp -s - "$base/$target/dist/index.html"
}
if ! check; then
    if [[ -n $old && $old != "$target" ]]; then activate "$old"; fi
    echo "Staging health check failed" >&2
    exit 1
fi
if [[ -n $old && $old != "$target" ]]; then link_release "$old" previous; fi
echo "Healthy: $url/ ($target)"
