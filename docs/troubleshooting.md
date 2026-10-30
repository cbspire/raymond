# Troubleshooting

Run `raymond config validate --file path` first. Use `raymond serve --config path` with high ports and dedicated loopback IPs for foreground debugging. Example: DNS `127.0.0.43:1053`, proxy `127.0.0.42:8080`. Query DNS directly and request HTTP with the intended Host. Installed integration uses ports 53/80 only.

`raymond doctor --config path` probes TCP/UDP binds without killing services. Permission errors require an explicit administrator invocation for privileged ports. A wildcard :80 listener conflicts; localhost:80 can coexist. A running Raymond itself occupies these ports, so stop it before bind diagnostics. Bind success does not prove OS split DNS: check native OS resolution after start (`dscacheutil -q host -a name tenant.app.test` on macOS, `getent ahostsv4 tenant.app.test` on Linux).

A 404 means no matching route; a 502 means the selected backend was unreachable. Confirm the app's loopback IP/port. Raymond preserves the original Host, cookies and redirects; incorrect upstream redirects must be fixed in the app's HTTP configuration. Do not enable HTTPS redirects for v0.1.

Linux integration requires already-active systemd-resolved and systemd-networkd; missing tools or an existing raymond0 link are errors, not reasons to replace resolv.conf. Inspect `resolvectl status raymond0` and `networkctl status raymond0`. macOS needs root-owned resolver files and aliases on lo0. Never delete non-Raymond resolver files.

If install was interrupted, the root-owned journal is retained. Uninstall verifies file hashes before cleanup. Restore operator-modified installed content from your reviewed configuration/binary before retrying; do not force-delete unrelated resources. Unsupported runners must fail visibly rather than skip required integration.

The binary download installer refuses directories or symbolic links named `raymond` at its destination. Choose an explicit different absolute `RAYMOND_INSTALL_DIR` or resolve the conflicting path yourself; do not rerun it with sudo to bypass the refusal. A failed staged `version` command preserves the old binary. If no release exists, build the current candidate from source instead of retrying a missing release URL. See the [installation walkthrough](installation.md).

## Docker Desktop on macOS

On the tested Mac with Docker Desktop 4.97.0, publishing `127.0.0.1:81:18080` produced a host `*:81` socket and accepted requests on a second loopback address. Publishing the same container endpoint as `127.0.0.1:18081:18080` respected the exact host IP. Thus changing Compose from `80:80` to `127.0.0.1:80:80` alone may not remove the low-port wildcard listener.

A separate high-port test verified that Darwin can route an exact-address native listener and a Docker wildcard socket independently: requests to the dedicated loopback reached the native server while localhost continued to reach Docker with the same HTTP response. Both startup orders passed, and all diagnostic containers were removed. These checks establish socket coexistence on this Mac, not privileged OS lifecycle acceptance.

To opt into that behavior without stopping Docker, edit `~/.raymond/raymond.yaml`:

```yaml
proxy:
  listen: 127.0.0.42:80
  allow_wildcard_coexistence: true
```

Then run `raymond config validate`, followed by the documented explicit `sudo raymond install` and `sudo raymond start` from the reviewed local binary. If Raymond is already installed, uninstall first using the existing installed resource journal. The workaround remains subject to the kernel accepting Raymond's exact socket; real bind errors are never ignored. DNS conflicts remain errors. No root-owned service or resolver changes are made merely by editing the user config.

Docker documents the changed privileged-port handling from version 4.97 in its [macOS permission requirements](https://docs.docker.com/desktop/setup/install/mac-permission-requirements/#binding-privileged-ports).

## Startup messages and live requests

A missing-service check before the first launchd bootstrap is expected; it is now silent. Successful `start`, `stop` and `restart` commands print a clear acknowledgement. `status` still shows the native service state, and real bootstrap failures remain visible.

Use `raymond logs` for the last 50 service log lines followed by new requests. Use `raymond logs --lines 100` for more history or `raymond logs --no-follow` to return immediately. Ctrl+C stops log following. macOS follows the fixed service log file across rotation, and Linux uses a journal filter for Raymond's unit. Foreground `serve` writes the same access records directly to stderr.

An empty file with the older installed daemon does not mean requests are failing: the old implementation emitted no access events. After rebuilding, uninstall/install/start to deploy the new daemon; a restart alone does not replace the protected executable. HTTP request records contain method, host, escaped path, final status, bytes and duration. Query strings, authentication headers, cookies and payloads are excluded. Long-lived streams and WebSocket requests appear when they complete.
