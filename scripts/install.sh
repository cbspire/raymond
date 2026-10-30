#!/usr/bin/env bash
# Install only the user-owned binary. OS integration is a separate explicit step.
set -euo pipefail
repo=cbspire/raymond
fail() { printf 'Raymond installer: %s\n' "$*" >&2; exit 1; }
[ "$#" -eq 0 ] || fail 'Use RAYMOND_VERSION and RAYMOND_INSTALL_DIR environment variables; no arguments are accepted.'
for tool in uname tar gzip awk mktemp mkdir chmod mv rm cp; do
  command -v "$tool" >/dev/null 2>&1 || fail "Required command not found: $tool"
done
case "$(uname -s)" in
  Darwin) os=darwin; checksum_tool=shasum;;
  Linux) os=linux; checksum_tool=sha256sum;;
  *) fail 'Unsupported operating system; expected macOS or Linux.';;
esac
case "$(uname -m)" in
  arm64|aarch64) arch=arm64;;
  x86_64|amd64) arch=amd64;;
  *) fail 'Unsupported architecture; expected amd64 or arm64.';;
esac
command -v "$checksum_tool" >/dev/null 2>&1 || fail "Required command not found: $checksum_tool"
dir=${RAYMOND_INSTALL_DIR:-${HOME:?HOME required}/.local/bin}
case "$dir" in /*) ;; *) fail 'Install directory must be absolute.';; esac
case "$dir" in /|*$'\n'*) fail 'Unsafe install directory.';; esac
# Canonicalize the directory, then refuse directory/symlink destinations instead
# of letting mv put a downloaded executable inside an unrelated directory.
mkdir -p "$dir"
dir=$(cd "$dir" && pwd -P)
[ "$dir" != / ] || fail 'Refusing to install into the filesystem root.'
[ ! -d "$dir/raymond" ] && [ ! -L "$dir/raymond" ] || fail 'Destination raymond is a directory or symbolic link; resolve it explicitly first.'
archive_dir=${RAYMOND_ARCHIVE_DIR:-}
if [ -n "$archive_dir" ]; then
  case "$archive_dir" in /*) ;; *) fail 'RAYMOND_ARCHIVE_DIR must be absolute.';; esac
  [ -d "$archive_dir" ] || fail 'Offline archive directory does not exist.'
else
  command -v curl >/dev/null 2>&1 || fail 'Required command not found: curl'
fi
version=${RAYMOND_VERSION:-}
if [ -n "$archive_dir" ] && [ -z "$version" ]; then fail 'RAYMOND_VERSION is required for offline archive installation.'; fi
if [ -z "$version" ]; then
  if ! url=$(curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest"); then
    fail 'Could not resolve the latest stable release. Check connectivity and GitHub Releases, or build from source.'
  fi
  version=${url##*/}
fi
[[ "$version" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]] || fail 'Expected an immutable version such as 0.1.0 or 0.1.0-rc.1.'
archive="raymond_${version#v}_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$version"
tmp=$(mktemp -d)
stage=''
cleanup() {
  rm -rf "$tmp"
  if [ -n "$stage" ]; then rm -f "$stage"; fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
if [ -n "$archive_dir" ]; then
  cp "$archive_dir/$archive" "$tmp/$archive"
  cp "$archive_dir/checksums.txt" "$tmp/checksums.txt"
else
  curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL "$base/$archive" -o "$tmp/$archive"
  curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"
fi
expected=$(awk -v name="$archive" '$2 == name {print $1}' "$tmp/checksums.txt")
[[ "$expected" =~ ^[a-fA-F0-9]{64}$ ]] || fail 'Missing or ambiguous archive checksum.'
if [ "$os" = darwin ]; then actual=$(shasum -a 256 "$tmp/$archive"); else actual=$(sha256sum "$tmp/$archive"); fi
actual=${actual%% *}
[ "$actual" = "$expected" ] || fail 'Archive checksum mismatch.'
# Do not extract archive paths into the installation directory. Accept exact
# release filenames and regular files only, then stream the binary to staging.
tar -tzf "$tmp/$archive" > "$tmp/names"
if ! awk 'BEGIN {binary=0} $0=="raymond" {binary++;next} $0=="LICENSE"||$0=="README.md" {next} {exit 1} END {if(binary!=1)exit 1}' "$tmp/names"; then
  fail 'Unsafe archive paths.'
fi
tar -tvzf "$tmp/$archive" > "$tmp/types"
if ! awk 'substr($0,1,1)!="-" {exit 1}' "$tmp/types"; then fail 'Archive contains a non-regular file.'; fi
stage=$(mktemp "$dir/.raymond.XXXXXX")
tar -xOzf "$tmp/$archive" raymond > "$stage"
chmod 755 "$stage"
if ! binary_version=$("$stage" version); then
  fail 'Downloaded binary could not run; the existing binary was left unchanged.'
fi
# Check again after downloading so newly created directories/links are refused.
[ ! -d "$dir/raymond" ] && [ ! -L "$dir/raymond" ] || fail 'Destination changed to a directory or symbolic link.'
mv -f "$stage" "$dir/raymond"
stage=''
printf '%s\nInstalled %s/raymond. Add %s to PATH if needed.\n' "$binary_version" "$dir" "$dir"
printf '%s\n' 'No DNS or service changes were made. Follow the OS setup steps in https://github.com/cbspire/raymond#install-and-enable-os-integration.'
