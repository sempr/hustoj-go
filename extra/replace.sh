#!/bin/bash

set -e

img="$1"
conf="$2"

echo "$img $conf"

V=$(docker inspect "$img" -f "base = \"{{.GraphDriver.Data.UpperDir}}:{{.GraphDriver.Data.LowerDir}}\"")
echo "$V"

env_json=$(docker inspect "$img" --format '{{json .Config.Env}}')

declare -A envmap=()
linear=()

while IFS= read -r tok; do
	key=${tok%%=*}
	val=${tok#*=}
	if [ -n "$key" ] && [ -z "${envmap[$key]+x}" ]; then
		linear+=("$key")
	fi
	envmap[$key]=$val
done < <(printf '%s' "$env_json" | grep -o '"[^"]*"' | tr -d '"')

manual=$(sed -n 's/.*env[[:space:]]*=[[:space:]]*\[\(.*\)\][[:space:]]*$/\1/p' "$conf")
	if [ -n "$manual" ]; then
		while IFS= read -r t; do
			t=${t//\"/}
			t=$(printf '%s' "$t" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')
			[ -n "$t" ] || continue
			key=${t%%=*}
			val=${t#*=}
			if [ -n "$key" ]; then
				if [ -z "${envmap[$key]+x}" ]; then
					linear+=("$key")
				fi
				envmap[$key]=$val
			fi
		done < <(printf '%s\n' "$manual" | tr ',' '\n')
	fi

env_items=()
for key in "${linear[@]}"; do
	env_items+=("\"$key=${envmap[$key]}\"")
done

env_line="env = [$(IFS=,; echo "${env_items[*]}")]"
esc=$(printf '%s' "$env_line" | sed 's/&/\\&/g; s/\\/\\\\/g')

sed -i.bak \
	-e "s|^base = .*|${V}|g" \
	-e "s|^env = .*|${esc}|" \
	"$conf"