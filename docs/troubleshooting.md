# Troubleshooting

Run `raymond config validate --file path` first. Use `raymond serve --config path` with high ports and dedicated loopback IPs for foreground debugging. Example: DNS `127.0.0.43:1053`, proxy `127.0.0.42:8080`. Query DNS directly and request HTTP with the intended Host. Installed integration uses ports 53/80 only.

`raymond doctor --config path` probes TCP/UDP binds without killing services. Permission errors require an explicit administrator invocation for privileged ports. A wildcard :80 listener conflicts; localhost:80 can coexist. A running Raymond itself occupies these ports, so stop it before bind diagnostics. Bind success does not prove OS split DNS: check native OS resolution after start (`dscacheutil -q host -a name site.install01.web.test` on macOS, `getent ahostsv4 site.install01.web.test` on Linux).

A 404 means no matching installation; a 502 means the selected backend was unreachable. Confirm the app's loopback IP/port. Raymond preserves WordPress Host, cookies and redirects; incorrect upstream redirects must be fixed in the app's HTTP configuration. Do not enable HTTPS redirects for v0.1.

Linux integration requires already-active systemd-resolved and systemd-networkd; missing tools or an existing raymond0 link are errors, not reasons to replace resolv.conf. Inspect `resolvectl status raymond0` and `networkctl status raymond0`. macOS needs root-owned resolver files and aliases on lo0. Never delete non-Raymond resolver files.

If install was interrupted, the root-owned journal is retained. Uninstall verifies file hashes before cleanup. Restore operator-modified installed content from your reviewed configuration/binary before retrying; do not force-delete unrelated resources. Unsupported runners must fail visibly rather than skip required integration.

The binary download installer refuses directories or symbolic links named `raymond` at its destination. Choose an explicit different absolute `RAYMOND_INSTALL_DIR` or resolve the conflicting path yourself; do not rerun it with sudo to bypass the refusal. A failed staged `version` command preserves the old binary. If no release exists, build the current candidate from source instead of retrying a missing release URL. See the [installation walkthrough](installation.md).
