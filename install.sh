#!/usr/bin/env bash
set -euo pipefail
# Resolve once and use the installer from the same immutable release as its assets.
repo=cbspire/raymond
version=${RAYMOND_VERSION:-}
if [ -z "$version" ]; then
  url=$(curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest")
  version=${url##*/}
fi
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]] || { echo 'Could not resolve a Raymond release version.' >&2; exit 1; }
script=$(mktemp)
trap 'rm -f "$script"' EXIT
curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL "https://raw.githubusercontent.com/$repo/$version/scripts/install.sh" -o "$script"
RAYMOND_VERSION="$version" bash "$script" "$@"
