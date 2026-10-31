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
CGO_ENABLED=0 go build -trimpath -o "$root/bin/raymond-manager-candidate" ./cmd/raymond
CGO_ENABLED=0 go build -trimpath -ldflags='-X main.version=0.1.2' -o "$root/bin/raymond-manager-update" ./cmd/raymond
CGO_ENABLED=0 go build -trimpath -ldflags='-X main.version=0.1.3' -o "$root/bin/raymond-manager-next" ./cmd/raymond
CGO_ENABLED=1 go test -c -tags osintegration -o "$root/bin/raymond-manager-acceptance" ./internal/manager
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
  "$root/bin/raymond-acceptance" -test.v -test.timeout=8m 2>&1 | tee "$root/acceptance.log"

sudo env PATH="$native_bin" \
  RAYMOND_PRIVILEGED_SMOKE=1 RAYMOND_TEST_USER="$(id -un)" \
  RAYMOND_MANAGER_BINARY="$root/bin/raymond-manager-candidate" \
  RAYMOND_UPDATE_BINARY="$root/bin/raymond-manager-update" \
  RAYMOND_NEXT_BINARY="$root/bin/raymond-manager-next" \
  "$root/bin/raymond-manager-acceptance" -test.v -test.run '^TestNativeManagedLifecycle$' -test.timeout=10m 2>&1 | tee -a "$root/acceptance.log"
