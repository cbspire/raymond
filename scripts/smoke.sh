#!/usr/bin/env bash
# Only run on a disposable macOS/Linux runner after reviewing privileged changes.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
if [ "${RAYMOND_PRIVILEGED_SMOKE:-}" != 1 ]; then echo 'Set RAYMOND_PRIVILEGED_SMOKE=1 only on a reviewed disposable runner' >&2; exit 1; fi
sudo -n true
sudo mkdir -p /opt/raymond-smoke
sudo install -o root -m 755 "$root/bin/raymond" /opt/raymond-smoke/raymond
cleanup() {
  sudo /opt/raymond-smoke/raymond uninstall || true
  if [ -n "${backend:-}" ]; then sudo kill "$backend" 2>/dev/null || true; fi
  if [ -n "${tmp:-}" ]; then rm -rf "$tmp"; fi
  sudo rm -f /opt/raymond-smoke/raymond
  sudo rmdir /opt/raymond-smoke 2>/dev/null || true
}
trap cleanup EXIT
# The helper binds localhost:80 before Raymond and runs two local app backends.
sudo python3 "$root/scripts/smoke_backend.py" &
backend=$!
tmp=$(mktemp -d)
sudo python3 - "$tmp/before.json" <<'PYHASH'
import os,sys,json,hashlib
paths={}
for root in ['/etc/resolver','/etc/systemd/network','/etc/systemd/resolved.conf.d','/etc/systemd/system/raymond.service.d']:
 for parent,dirs,files in os.walk(root):
  for name in files:
   p=os.path.join(parent,name)
   if os.path.isfile(p):paths[p]=hashlib.sha256(open(p,'rb').read()).hexdigest()
json.dump(paths,open(sys.argv[1],'w'))
PYHASH
python3 - "$tmp/config.yaml" <<'PY'
import sys
with open(sys.argv[1],'w') as f:
 f.write('version: 1\ndns:\n  zones: [test, internal]\ninstallations:\n')
 for i in range(10):
  zone='test' if i<5 else 'internal'
  f.write('  - domain: install%d.web.%s\n    target: 127.0.0.1:%d\n'%(i,zone,9001+i%2))
PY
sleep 1
sudo /opt/raymond-smoke/raymond install --config "$tmp/config.yaml"
sudo /opt/raymond-smoke/raymond install --config "$tmp/config.yaml"
sudo /opt/raymond-smoke/raymond start
python3 - <<'PY'
import socket,urllib.request,time,concurrent.futures
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
hosts=['deep.site%d.install%d.web.%s'%(j,i,'test' if i<5 else 'internal') for i in range(10) for j in range(5)]
def check(host):
 assert socket.gethostbyname(host)=='127.0.0.42',host
 with opener.open('http://'+host+'/wp-admin/?original=1',timeout=5) as r:
  assert r.read().decode()==host+' /wp-admin/?original=1',host
end=time.monotonic()+20
while True:
 try:check(hosts[0]);break
 except Exception:
  if time.monotonic()>end:raise
  time.sleep(.2)
start=time.monotonic()
with concurrent.futures.ThreadPoolExecutor(max_workers=10) as p:list(p.map(check,hosts))
print('50 OS-resolved concurrent HTTP requests passed in %.3fs'%(time.monotonic()-start))
with opener.open('http://127.0.0.1/',timeout=2) as r:assert r.read()==b'localhost unchanged'
PY
sudo /opt/raymond-smoke/raymond stop
sudo /opt/raymond-smoke/raymond uninstall
# Ensure cleanup really removed owned split DNS/service resources, not just a PID.
case "$(uname -s)" in
 Darwin) test ! -e /etc/resolver/test; test ! -e /etc/resolver/internal; test ! -e /Library/LaunchDaemons/dev.raymond.plist;;
 Linux) test ! -e /etc/systemd/network/80-raymond.network; test ! -e /etc/systemd/network/80-raymond.netdev; test ! -e /etc/systemd/system/raymond.service; ! ip link show raymond0 >/dev/null 2>&1;;
esac
test ! -e /var/lib/raymond/state.json
sudo python3 - "$tmp/before.json" <<'PYHASH'
import sys,json,hashlib
for p,digest in json.load(open(sys.argv[1])).items():
 assert hashlib.sha256(open(p,'rb').read()).hexdigest()==digest, 'Unrelated resource changed: '+p
PYHASH
echo 'Privileged lifecycle and cleanup checks passed'
