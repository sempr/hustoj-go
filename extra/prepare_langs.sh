#!/bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

for dir in */; do
    id="${dir%/}"
    [ "$id" = "base" ] && continue
    [ -f "$id/Dockerfile" ] || continue
    [ -f "etc/langs/${id}.lang.toml" ] || continue

    echo "===> building image hustoj:lang-${id}"
    docker build -t "hustoj:lang-${id}" "$id"
    bash replace.sh "hustoj:lang-${id}" "etc/langs/${id}.lang.toml"
done