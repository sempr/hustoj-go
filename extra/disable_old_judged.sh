#!/bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_FALSE="$(command -v false)"

if [ "$(id -u)" -ne 0 ]; then
	echo "must run as root" >&2
	exit 1
fi

JUDGED_BINS=(
	/usr/bin/judged
	/usr/bin/judge_client
	/usr/bin/judge_client_c
	/usr/local/bin/judged
	/usr/local/bin/judge_client
)

UNDO=0
[ "${1:-}" = "--undo" ] && UNDO=1

kill_old() {
	echo "==> killing legacy judged/judge_client processes"
	pkill -9 -x judged 2>/dev/null || true
	pkill -9 -x judge_client 2>/dev/null || true
	pkill -9 -x judge_client_c 2>/dev/null || true
}

neuter_units() {
	echo "==> removing SysV rc links for init.d/hustoj (boot autostart)"
	update-rc.d -f hustoj remove >/dev/null 2>&1 || true

	echo "==> disabling leftover legacy systemd units"
	local unit
	for unit in $(systemctl list-unit-files --type=service 2>/dev/null | awk '{print $1}' | grep -i 'judged' || true); do
		case "$unit" in
			judged-go*|judge-go*) continue ;;
		esac
		echo "   - $unit"
		systemctl stop "$unit" >/dev/null 2>&1 || true
		systemctl disable "$unit" >/dev/null 2>&1 || true
	done
}

clean_cron() {
	echo "==> commenting cron entries referencing judged"
	local f
	for f in /etc/crontab /etc/cron.d/* /var/spool/cron/crontabs/*; do
		[ -f "$f" ] || continue
		grep -q 'judged' "$f" 2>/dev/null || continue
		cp -a "$f" "$f.orig.judged-backup"
		sed -i 's/^\([^#].*judged\)/#\1/' "$f"
		echo "   - $f"
	done
}

install_placeholder() {
	local path="$1"
	# lock the path even if it does not exist yet, so a reinstall / revive
	# (make install, cp, mv, package manager) cannot recreate it.
	if [ -e "$path" ] || [ -L "$path" ]; then
		chattr -i "$path" 2>/dev/null || true
		rm -f "$path"
	fi
	{
		printf '#!/bin/sh\n'
		printf 'exec %s\n' "$BIN_FALSE"
	} > "$path"
	chmod 0755 "$path"
	chattr +i "$path"
	echo "   - $path -> wrapper script exec ${BIN_FALSE} (immutable)"
}

undo_placeholder() {
	local path="$1"
	[ -e "$path" ] || [ -L "$path" ] || return 0
	chattr -i "$path" 2>/dev/null || true
	rm -f "$path"
	echo "   - $path -> removed"
}

if [ "$UNDO" -eq 1 ]; then
	echo "==> undoing: removing false placeholders"
	for b in "${JUDGED_BINS[@]}"; do
		undo_placeholder "$b"
	done
	echo "==> restoring SysV rc links"
	update-rc.d hustoj defaults >/dev/null 2>&1 || true
	echo "==> restoring cron backups"
	for f in /etc/crontab /etc/cron.d/* /var/spool/cron/crontabs/*; do
		[ -f "$f.orig.judged-backup" ] || continue
		mv -f "$f.orig.judged-backup" "$f"
		echo "   - $f"
	done
	echo "done. reinstall the original judged binaries if needed."
	exit 0
fi

kill_old
neuter_units
clean_cron

echo "==> locking legacy judged paths (create or replace with false wrapper)"
for b in "${JUDGED_BINS[@]}"; do
	install_placeholder "$b"
done

echo "==> verifying"
for b in "${JUDGED_BINS[@]}"; do
	if [ -L "$b" ]; then
		echo "   - $b: WARNING still a symlink!"
	elif lsattr -d "$b" 2>/dev/null | grep -q -- 'i' && ! "$b"; then
		echo "   - $b immutably neutered"
	else
		echo "   - $b: WARNING not fully neutered!"
	fi
done
echo "done. legacy judged cannot restart; judged-go.service is unaffected."