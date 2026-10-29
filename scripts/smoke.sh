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
sudo env PATH=/usr/bin:/bin:/usr/sbin:/sbin \
  GODEBUG=netdns=cgo \
  RAYMOND_PRIVILEGED_SMOKE=1 RAYMOND_BINARY="$root/bin/raymond" \
  "$root/bin/raymond-acceptance" -test.v -test.timeout=8m
