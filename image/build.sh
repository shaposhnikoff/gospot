#!/usr/bin/env bash
# Build the Raspberry Pi OS image of the product locally with Docker and a pinned
# pi-gen: stage0..stage2 (Lite) plus stage-local, which installs the local
# radio and dashboard packages with apt. Never run in CI. Every path, name
# and port comes from the settings file named in image/settings.path.
set -euo pipefail

usage() {
	cat <<EOF
Usage: $0 [--arch ARCH] [--prepare-only]
  --arch ARCH      image architecture (default IMAGE_ARCH of the settings; only armhf for now)
  --prepare-only   fetch pi-gen, build the packages and prepare the Docker context, do not build
RADIO_DEB=FILE and DASHBOARD_DEB=FILE use already built packages instead of building them.
Builds go to a new directory under IMAGE_BUILD_PARENT of the settings (override with BUILD_PARENT).
EOF
}
prepare=0
arch=
while (( $# )); do
	case $1 in
		--prepare-only) prepare=1 ;;
		--arch) arch=${2:?--arch needs a value}; shift ;;
		--help|-h) usage; exit 0 ;;
		*) usage >&2; exit 2 ;;
	esac
	shift
done

image=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo=$(cd -- "$image/.." && pwd -P)
settings=$(realpath -e "$image/$(cat "$image/settings.path")")
dash=$(dirname "$settings")
# shellcheck source=/dev/null
source "$settings"
arch=${arch:-$IMAGE_ARCH}
suite=$IMAGE_SUITE
case $arch in
	armhf) pigen_rev=$IMAGE_PIGEN_REV_ARMHF ;;
	arm64) echo "arm64 is a later stage: pin IMAGE_PIGEN_REV_ARM64 (pi-gen arm64 branch) and build arm64 packages first" >&2; exit 2 ;;
	*) echo "unknown architecture: $arch" >&2; exit 2 ;;
esac
[[ $pigen_rev =~ ^[0-9a-f]{40}$ ]] || { echo "pi-gen revision for $arch must be a full commit id" >&2; exit 1; }

parent=$(cd -- "${BUILD_PARENT:-$IMAGE_BUILD_PARENT}" && pwd -P)
[[ $parent =~ ^/[a-zA-Z0-9_./-]*$ ]] || { echo "Use a simple build path without spaces." >&2; exit 1; }
case $parent/ in "$repo"/*) echo "Build outside the repository (IMAGE_BUILD_PARENT)." >&2; exit 1 ;; esac
started=$(date +%s)

# deb_from_build LOG DIR: the package path a build-deb printed last ("sha256  path").
deb_from_build() { local rel; rel=$(awk 'END { print $2 }' "$1"); [[ $rel == *.deb ]] && echo "$2/$rel"; }
build=$(mktemp -d "$parent/$IMAGE_NAME-build.XXXXXXXXXX")
echo "Build directory: $build"
if [[ -z ${RADIO_DEB:-} ]]; then
	"$repo/radio/build-deb" "$suite" | tee "$build/radio-build-deb.log"
	RADIO_DEB=$(deb_from_build "$build/radio-build-deb.log" "$repo/radio")
fi
if [[ -z ${DASHBOARD_DEB:-} ]]; then
	"$dash/scripts/build-deb" | tee "$build/dashboard-build-deb.log"
	DASHBOARD_DEB=$(deb_from_build "$build/dashboard-build-deb.log" "$dash")
fi
radio_deb=$(realpath -e "$RADIO_DEB")
dashboard_deb=$(realpath -e "$DASHBOARD_DEB")
for spec in "$RADIO_PACKAGE:$radio_deb" "$DASHBOARD_PACKAGE:$dashboard_deb"; do
	name=${spec%%:*} deb=${spec#*:}
	[[ $(dpkg-deb -f "$deb" Package) == "$name" ]] || { echo "$deb is not $name" >&2; exit 1; }
	[[ $(dpkg-deb -f "$deb" Architecture) == "$arch" ]] || { echo "$deb is not $arch" >&2; exit 1; }
done

id=${build##*.}
id=${id,,}
pigen=$build/pi-gen
git init -q "$pigen"
git -C "$pigen" fetch -q --depth=1 "$IMAGE_PIGEN_REPO" "$pigen_rev"
git -C "$pigen" checkout -q --detach FETCH_HEAD
[[ $(git -C "$pigen" rev-parse HEAD) == "$pigen_rev" ]]
grep -q "^export RELEASE=\${RELEASE:-$suite}" "$pigen/build.sh" || { echo "pinned pi-gen does not default to $suite" >&2; exit 1; }
grep -q "^export ARCH=$arch$" "$pigen/build.sh" || { echo "pinned pi-gen does not build $arch" >&2; exit 1; }
# Optional build-time Raspbian mirror: debootstrap and apt use it while
# building; after the final dist-upgrade the image gets pi-gen's source back.
if [[ -n $IMAGE_BUILD_RASPBIAN_MIRROR ]]; then
	sources=$pigen/stage0/00-configure-apt/files/raspbian.sources
	upstream=$(sed -n 's/^URIs: //p' "$sources")
	[[ -n $upstream ]] && grep -qF " $upstream" "$pigen/stage0/prerun.sh" || { echo "pi-gen Raspbian source changed; update the mirror rewrite" >&2; exit 1; }
	sed -i "s|$upstream|$IMAGE_BUILD_RASPBIAN_MIRROR|" "$pigen/stage0/prerun.sh" "$sources"
	cat >"$pigen/export-image/02-set-sources/02-run.sh" <<EOF
#!/bin/bash -e
sed -i 's|^URIs: $IMAGE_BUILD_RASPBIAN_MIRROR\$|URIs: $upstream|' "\${ROOTFS_DIR}/etc/apt/sources.list.d/raspbian.sources"
grep -qx 'URIs: $upstream' "\${ROOTFS_DIR}/etc/apt/sources.list.d/raspbian.sources"
find "\${ROOTFS_DIR}/var/lib/apt/lists/" -type f -delete
EOF
	chmod 755 "$pigen/export-image/02-set-sources/02-run.sh"
fi

stage=$pigen/stage-local
cp -a "$image/stage-local" "$stage"
rm -rf "$stage"/*/tests
install -d "$stage/00-packages/files"
install -m 0644 "$radio_deb" "$dashboard_deb" "$stage/00-packages/files/"
# The stage reads the installed settings file: the dashboard package's
# conffile named like the repository copy, checked against that copy.
settings_file=$(dpkg-deb --ctrl-tarfile "$dashboard_deb" | tar -xO ./conffiles | grep -x "/.*/$(basename "$settings")")
[[ $(wc -l <<<"$settings_file") == 1 ]] || { echo "$DASHBOARD_PACKAGE has no conffile $(basename "$settings")" >&2; exit 1; }
printf 'SETTINGS_FILE=%s\n' "$settings_file" >"$stage/image.env"
install -m 0644 "$settings" "$stage/settings.conf"
for sub in "$stage"/[0-9][0-9]-*/; do
	compgen -G "${sub}[0-9][0-9]-run*.sh" >/dev/null || { echo "Incomplete custom stage: $sub" >&2; exit 1; }
done
# Lite is not exported on its own: stage-local has the only EXPORT_IMAGE.
touch "$pigen/stage2/SKIP_IMAGES"



cat >"$pigen/config" <<EOF
IMG_NAME=$IMAGE_NAME
RELEASE=$suite
TARGET_HOSTNAME=$IMAGE_HOSTNAME
FIRST_USER_NAME=$GOSPOT_LINUX_USER
FIRST_USER_PASS=$IMAGE_DEFAULT_PASSWORD
DISABLE_FIRST_BOOT_USER_RENAME=1
ENABLE_SSH=1
ENABLE_CLOUD_INIT=$IMAGE_ENABLE_CLOUD_INIT
STAGE_LIST="/pi-gen/stage0 /pi-gen/stage1 /pi-gen/stage2 /pi-gen/stage-local"
DEPLOY_COMPRESSION=$IMAGE_COMPRESSION
CONTAINER_NAME=$IMAGE_NAME-build-$id
EOF
# A private Docker image tag per build, so parallel builds do not collide.
tag=$IMAGE_NAME-pigen:$id
sed -i -e "s/ -t pi-gen / -t $tag /" -e "s/^  pi-gen \\\\$/  $tag \\\\/" "$pigen/build-docker.sh"
[[ $(grep -c "$tag" "$pigen/build-docker.sh") == 2 ]] || { echo "pi-gen build-docker.sh changed; update the tag rewrite" >&2; exit 1; }

{
	echo "pi-gen $IMAGE_PIGEN_REPO $pigen_rev"
	echo "suite $suite arch $arch"
	echo "source $(git -C "$repo" rev-parse HEAD)$([[ -z $(git -C "$repo" status --porcelain) ]] || echo ' with uncommitted changes')"
	for deb in "$radio_deb" "$dashboard_deb"; do
		echo "package $(dpkg-deb -f "$deb" Package) $(dpkg-deb -f "$deb" Version) $(sha256sum <"$deb" | cut -d' ' -f1) $(basename "$deb")"
	done
} >"$build/manifest"
cat "$build/manifest"
if (( prepare )); then
	echo "Prepared. To build: cd '$pigen' && ./build-docker.sh"
	exit 0
fi

(cd "$pigen" && ./build-docker.sh)
shopt -s nullglob
artifacts=("$pigen"/deploy/*.img."$IMAGE_COMPRESSION")
(( ${#artifacts[@]} == 1 )) || { echo "Expected exactly one compressed disk image" >&2; exit 1; }
artifact=${artifacts[0]}
cp "$build/manifest" "$pigen/deploy/$(basename "$artifact" ".img.$IMAGE_COMPRESSION").manifest"
(cd "$pigen/deploy" && sha256sum -- "$(basename "$artifact")" >"$(basename "$artifact").sha256")
echo "Built in $(( $(date +%s) - started )) s: $artifact"
"$image/verify-artifact.sh" "$artifact" "$radio_deb" "$dashboard_deb"
cat "$artifact.sha256"
ls -l "$artifact"
echo "Image, manifest and logs: $pigen/deploy/"
