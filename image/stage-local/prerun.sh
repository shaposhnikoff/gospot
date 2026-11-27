#!/bin/bash -e
# The local stage starts from the Lite root filesystem of stage2.
if [[ ${PREV_ROOTFS_DIR} != "${WORK_DIR}/stage2/rootfs" ]]; then
	echo "stage-local must immediately follow stage2" >&2
	exit 1
fi
if [ ! -d "${ROOTFS_DIR}" ]; then
	copy_previous
fi
