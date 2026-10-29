#!/usr/bin/env bash
# Disposable runner only: compiled CI helpers are not Raymond runtime dependencies.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
if [ "${RAYMOND_PRIVILEGED_SMOKE:-}" != 1 ]; then
  echo 'Set RAYMOND_PRIVILEGED_SMOKE=1 only on a reviewed disposable runner' >&2
  exit 1
fi
sudo -n true
# The helper needs cgo solely to exercise each OS's native resolver API. The
# distributed product is always built CGO_ENABLED=0 and checked by the helper.
CGO_ENABLED=1 go test -c -tags osintegration -o "$root/bin/raymond-acceptance" "$root/test/acceptance"
# Ubuntu may also have Go in /usr/bin. Use a whitelist containing only the
# native installer tools, proving that the product never invokes a Go runtime.
native_bin=$(mktemp -d)
trap 'rm -rf "$native_bin"' EXIT
for tool in bash sh uname tar gzip awk mktemp mkdir chmod mv rm cp shasum sha256sum perl; do
  if tool_path=$(command -v "$tool"); then ln -s "$tool_path" "$native_bin/$tool"; fi
done
sudo env PATH="$native_bin" \
  GODEBUG=netdns=cgo \
  RAYMOND_PRIVILEGED_SMOKE=1 RAYMOND_ARCHIVE_DIR="$root/dist" \
  RAYMOND_INSTALL_SCRIPT="$root/scripts/install.sh" RAYMOND_TEST_USER="$(id -un)" \
  "$root/bin/raymond-acceptance" -test.v -test.timeout=8m
