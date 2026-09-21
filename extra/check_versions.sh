#!/bin/bash

set -u

# Print the compiler version info of every configured language by running its
# `ver` command inside the language rootfs, without relying on Docker.
# The rootfs is reconstructed from the `[fs].base` overlay layer paths already
# recorded in each language config, mounted as an overlay, then chrooted.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LANGS_DIR="${1:-$SCRIPT_DIR/etc/langs}"
MNT_ROOT="${MNT_ROOT:-/tmp}/check-lang"

if [ "$(id -u)" -ne 0 ]; then
	echo "must run as root to mount overlay and chroot" >&2
	exit 1
fi

mkdir -p "$MNT_ROOT"

MNT_CUR=
WORK_CUR=
PROC_CUR=

cleanup() {
	if [ -n "$PROC_CUR" ]; then
		umount "$PROC_CUR" >/dev/null 2>&1 || true
	fi
	if [ -n "$MNT_CUR" ]; then
		umount "$MNT_CUR" >/dev/null 2>&1 || true
		rm -rf "$MNT_CUR"
	fi
	[ -n "$WORK_CUR" ] && rm -rf "$WORK_CUR"
}

check_one() {
	local f="$1"
	local base ver name id upper lower root output rc envraw envs e

	base=$(sed -n 's/^base[[:space:]]*=[[:space:]]*"\(.*\)"[[:space:]]*$/\1/p' "$f")
	ver=$(sed -n 's/^ver[[:space:]]*=[[:space:]]*"\(.*\)"[[:space:]]*$/\1/p' "$f")
	name=$(sed -n 's/^name[[:space:]]*=[[:space:]]*"\(.*\)"[[:space:]]*$/\1/p' "$f" | head -1)
	id=$(basename "$f" .lang.toml)
	[ -n "$base" ] || { echo "--- $id ($name): no base path in config, skipped"; return; }
	[ -n "$ver" ] || { echo "--- $id ($name): no ver command in config, skipped"; return; }

	upper=${base%%:*}
	lower=${base#*:}

	MNT_CUR="$MNT_ROOT/$id"
	mkdir -p "$MNT_CUR"

	if [ "$lower" = "$base" ]; then
		root="$upper"
	else
		WORK_CUR=$(mktemp -d "$(dirname "$upper")/.checkwork-XXXXXX")
		if ! mount -t overlay overlay \
			-o "lowerdir=$lower,upperdir=$upper,workdir=$WORK_CUR" "$MNT_CUR" 2>/dev/null; then
			echo "--- $id ($name): cannot mount overlay for $upper, skipped"
			return
		fi
		root="$MNT_CUR"
	fi

	envraw=$(sed -n 's/.*env[[:space:]]*=[[:space:]]*\[\(.*\)\][[:space:]]*$/\1/p' "$f")
	if [ -n "$envraw" ]; then
		IFS=, read -r -a envs <<< "$envraw"
		for e in "${envs[@]:-}"; do
			e=${e//\"/}
			[ -n "$e" ] && export -- "$e" 2>/dev/null || true
		done
	fi

	mkdir -p "$root/proc" "$root/dev"
	PROC_CUR="$root/proc"
	mount -t proc proc "$PROC_CUR" >/dev/null 2>&1 || true
	[ -e "$root/dev/null" ] || mknod -m 666 "$root/dev/null" c 1 3 2>/dev/null || true

	output=$(chroot "$root" /bin/sh -c "$ver" 2>&1)
	rc=$?

	echo "==> $id ($name):"
	echo "$output" | head -8
	[ "$rc" -eq 0 ] || echo "   (ver command exited with $rc)"
}

for f in $(LC_ALL=C printf '%s\n' "$LANGS_DIR"/*.lang.toml | sort -V); do
	[ -f "$f" ] || continue
	(
		trap cleanup EXIT
		check_one "$f"
	)
done