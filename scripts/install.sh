#!/usr/bin/env bash
set -euo pipefail
repo=cbspire/raymond
case "$(uname -s)" in Darwin) os=darwin;; Linux) os=linux;; *) echo 'Unsupported operating system' >&2; exit 1;; esac
case "$(uname -m)" in arm64|aarch64) arch=arm64;; x86_64|amd64) arch=amd64;; *) echo 'Unsupported architecture' >&2; exit 1;; esac
version=${RAYMOND_VERSION:-}
if [ -z "$version" ]; then
  url=$(curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest")
  version=${url##*/}
fi
if ! [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then echo 'Expected an immutable stable version such as v0.1.0' >&2; exit 1; fi
archive="raymond_${version#v}_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$version"
tmp=$(mktemp -d)
stage=''
trap 'rm -rf "$tmp"; if [ -n "$stage" ]; then rm -f "$stage"; fi' EXIT
curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL "$base/$archive" -o "$tmp/$archive"
curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"
expected=$(awk -v name="$archive" '$2 == name {print $1}' "$tmp/checksums.txt")
if ! [[ "$expected" =~ ^[a-fA-F0-9]{64}$ ]]; then echo 'Missing or ambiguous archive checksum' >&2; exit 1; fi
if [ "$os" = darwin ]; then actual=$(shasum -a 256 "$tmp/$archive"); else actual=$(sha256sum "$tmp/$archive"); fi
actual=${actual%% *}
if [ "$actual" != "$expected" ]; then echo 'Archive checksum mismatch' >&2; exit 1; fi
# Never extract a pathname from the archive into the installation directory.
# Only regular files with exact release names are accepted, then raymond is streamed.
tar -tzf "$tmp/$archive" > "$tmp/names"
if ! awk 'BEGIN {binary=0} $0=="raymond" {binary++;next} $0=="LICENSE"||$0=="README.md" {next} {exit 1} END {if(binary!=1)exit 1}' "$tmp/names"; then echo 'Unsafe archive paths' >&2; exit 1; fi
tar -tvzf "$tmp/$archive" > "$tmp/types"
if ! awk 'substr($0,1,1)!="-" {exit 1}' "$tmp/types"; then echo 'Archive contains a non-regular file' >&2; exit 1; fi
dir=${RAYMOND_INSTALL_DIR:-${HOME:?HOME required}/.local/bin}
case "$dir" in /*) ;; *) echo 'Install directory must be absolute' >&2; exit 1;; esac
case "$dir" in /|*$'\n'*) echo 'Unsafe install directory' >&2; exit 1;; esac
mkdir -p "$dir"
stage=$(mktemp "$dir/.raymond.XXXXXX")
tar -xOzf "$tmp/$archive" raymond > "$stage"
chmod 755 "$stage"
mv -f "$stage" "$dir/raymond"
stage=''
"$dir/raymond" version
printf 'Installed %s/raymond. Add %s to PATH if needed.\n' "$dir" "$dir"
printf '%s\n' 'No DNS or service changes were made. Review docs/linux.md or docs/macos.md before explicit administrator installation.'
