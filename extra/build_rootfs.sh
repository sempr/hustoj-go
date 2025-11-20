#!/bin/bash

name=${1:-"0"}
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

#cd $name
docker build -t hustoj:lang-${name} $name
bash replace.sh hustoj:lang-${name} etc/langs/${name}.lang.toml

