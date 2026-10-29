#!/usr/bin/env bash
# Offline fixtures; simulated OS/architecture selection does not certify native execution.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/fake" "$tmp/release" "$tmp/files" "$tmp/install space" "$tmp/downloads"
printf '#!/bin/sh\necho raymond v0.1.0\n' > "$tmp/files/raymond"
chmod +x "$tmp/files/raymond"
cat > "$tmp/fake/uname" <<'SH'
#!/bin/sh
case "$1" in -s) echo "$FIXTURE_OS";; -m) echo "$FIXTURE_ARCH";; esac
SH
cat > "$tmp/fake/curl" <<'SH'
#!/bin/sh
out=''; url=''; head=false
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out=$2; shift 2;;
    --proto|--proto-redir|-w) shift 2;;
    -fsSLI) head=true; shift;;
    https://*) url=$1; shift;;
    *) shift;;
  esac
done
printf '%s\n' "$url" >> "$CURL_LOG"
if [ "${FAIL_DOWNLOAD:-}" = 1 ]; then exit 22; fi
if [ "$head" = true ]; then
  printf 'https://github.com/cbspire/raymond/releases/tag/v0.1.0'
else
  cp "$FIXTURE/${url##*/}" "$out"
fi
SH
chmod +x "$tmp/fake/"*
export PATH="$tmp/fake:$PATH" FIXTURE="$tmp/release" CURL_LOG="$tmp/curl.log"
export RAYMOND_VERSION=v0.1.0 RAYMOND_INSTALL_DIR="$tmp/install space" TMPDIR="$tmp/downloads"
export FIXTURE_OS=Linux FIXTURE_ARCH=x86_64
archive=raymond_0.1.0_linux_amd64.tar.gz
package_fixture() {
  tar -czf "$FIXTURE/$archive" -C "$tmp/files" raymond
  (cd "$FIXTURE" && shasum -a 256 "$archive" > checksums.txt)
}
installed() { [ "$("$RAYMOND_INSTALL_DIR/raymond" version)" = 'raymond v0.1.0' ]; }
reject() {
  if bash "$root/scripts/install.sh" > "$tmp/error.log" 2>&1; then
    echo "Installer incorrectly accepted: $1" >&2; exit 1
  fi
}
# Each release filename and checksum-tool branch, including common arch aliases.
for platform in 'Linux x86_64 linux amd64' 'Linux aarch64 linux arm64' 'Darwin x86_64 darwin amd64' 'Darwin arm64 darwin arm64'; do
  read -r FIXTURE_OS FIXTURE_ARCH os arch <<< "$platform"
  export FIXTURE_OS FIXTURE_ARCH
  archive="raymond_0.1.0_${os}_${arch}.tar.gz"
  package_fixture
  bash "$root/scripts/install.sh" > /dev/null
  installed
done
export FIXTURE_OS=Linux FIXTURE_ARCH=x86_64
archive=raymond_0.1.0_linux_amd64.tar.gz
package_fixture
bash "$root/scripts/install.sh" > /dev/null
installed
# Resolve latest once, then fetch both assets from that exact tag.
unset RAYMOND_VERSION
: > "$CURL_LOG"
bash "$root/scripts/install.sh" > /dev/null
[ "$(wc -l < "$CURL_LOG" | tr -d ' ')" = 3 ]
grep -F 'https://github.com/cbspire/raymond/releases/download/v0.1.0/raymond_0.1.0_linux_amd64.tar.gz' "$CURL_LOG" > /dev/null
grep -F 'https://github.com/cbspire/raymond/releases/download/v0.1.0/checksums.txt' "$CURL_LOG" > /dev/null
export RAYMOND_VERSION=v0.1.0
printf 'corrupted' >> "$FIXTURE/$archive"
reject 'corrupted checksum'; installed
package_fixture
: > "$FIXTURE/checksums.txt"
reject 'missing checksum'; installed
package_fixture
cat "$FIXTURE/checksums.txt" >> "$tmp/duplicate"
cat "$tmp/duplicate" >> "$FIXTURE/checksums.txt"
reject 'ambiguous checksum'; installed
package_fixture
export FAIL_DOWNLOAD=1
reject 'failed download'; installed
unset FAIL_DOWNLOAD
# Check a symlink with an otherwise valid permitted name, exercising type checks.
rm "$tmp/files/raymond"
ln -s /etc/passwd "$tmp/files/raymond"
package_fixture
reject 'symlink binary'; installed
rm "$tmp/files/raymond"
printf '#!/bin/sh\nexit 1\n' > "$tmp/files/raymond"
package_fixture
reject 'binary fails version check'; installed
printf '#!/bin/sh\necho raymond v0.1.0\n' > "$tmp/files/raymond"
package_fixture
# A directory or symlink destination must not receive or redirect the staged file.
mkdir -p "$tmp/conflict/raymond"
export RAYMOND_INSTALL_DIR="$tmp/conflict"
reject 'directory destination'
[ -z "$(ls -A "$tmp/conflict/raymond")" ]
rmdir "$tmp/conflict/raymond"
ln -s "$tmp/install space/raymond" "$tmp/conflict/raymond"
reject 'symlink destination'
export RAYMOND_INSTALL_DIR="$tmp/install space"
installed
export RAYMOND_VERSION='v0.1.0;bad'
reject 'invalid tag'; installed
export RAYMOND_VERSION=v0.1.0 FIXTURE_OS=Unsupported
reject 'unsupported OS'; installed
export FIXTURE_OS=Linux FIXTURE_ARCH=unsupported
reject 'unsupported architecture'; installed
export FIXTURE_ARCH=x86_64 RAYMOND_INSTALL_DIR="$tmp/install space"
# Offline archive mode must not contact curl, even when it would fail.
export RAYMOND_ARCHIVE_DIR="$FIXTURE" FAIL_DOWNLOAD=1
package_fixture
bash "$root/scripts/install.sh" > /dev/null
installed
unset RAYMOND_VERSION
reject 'offline archive install without a pinned version'
export RAYMOND_VERSION=v0.1.0
unset RAYMOND_ARCHIVE_DIR FAIL_DOWNLOAD
export RAYMOND_INSTALL_DIR=relative/path
reject 'relative install directory'
[ -z "$(ls -A "$tmp/downloads")" ]
echo 'Installer platform selection, latest/pinned download, upgrade preservation, archive, path and failure fixtures passed'
