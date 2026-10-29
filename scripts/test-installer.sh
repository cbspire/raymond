#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/fake" "$tmp/release" "$tmp/files" "$tmp/bin"
printf '#!/bin/sh\necho raymond v0.1.0\n' > "$tmp/files/raymond"
chmod +x "$tmp/files/raymond"
archive=raymond_0.1.0_linux_amd64.tar.gz
tar -czf "$tmp/release/$archive" -C "$tmp/files" raymond
(cd "$tmp/release" && shasum -a 256 "$archive" > checksums.txt)
cat > "$tmp/fake/uname" <<'SH'
#!/bin/sh
case "$1" in -s) echo Linux;; -m) echo x86_64;; esac
SH
cat > "$tmp/fake/curl" <<'SH'
#!/bin/sh
out=''; url=''
while [ "$#" -gt 0 ]; do case "$1" in -o) out=$2;shift 2;; https://*) url=$1;shift;; --proto|--tlsv1.2) if [ "$1" = --proto ]; then shift 2;else shift;fi;; *) shift;; esac;done
cp "$FIXTURE/${url##*/}" "$out"
SH
chmod +x "$tmp/fake/"*
export PATH="$tmp/fake:$PATH" FIXTURE="$tmp/release" RAYMOND_VERSION=v0.1.0 RAYMOND_INSTALL_DIR="$tmp/bin"
bash "$root/scripts/install.sh" > /dev/null
[ "$("$tmp/bin/raymond" version)" = 'raymond v0.1.0' ]
bash "$root/scripts/install.sh" > /dev/null
printf 'corrupted' >> "$tmp/release/$archive"
if bash "$root/scripts/install.sh" > /dev/null 2>&1; then echo 'Accepted bad checksum' >&2; exit 1; fi
[ "$("$tmp/bin/raymond" version)" = 'raymond v0.1.0' ]
# Even a checksummed archive must not contain traversal or links.
rm "$tmp/release/$archive"
ln -s /etc/passwd "$tmp/files/link"
tar -czf "$tmp/release/$archive" -C "$tmp/files" raymond link
(cd "$tmp/release" && shasum -a 256 "$archive" > checksums.txt)
if bash "$root/scripts/install.sh" > /dev/null 2>&1; then echo 'Accepted unsafe archive' >&2; exit 1; fi
echo 'Installer success, repeatability, checksum and archive safety tests passed'
