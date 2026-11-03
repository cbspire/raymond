# Configuration

See `examples/raymond.yaml`. The `routes` list maps each base domain and its subdomains to a local backend. Previously generated user files remain readable without editing; avoid mixing mapping keys. The only supported schema is `version: 1`. Unknown keys, duplicate YAML keys, multiple documents, invalid labels and duplicate normalized routes are errors. Defaults are DNS `127.0.0.43:53` and HTTP `127.0.0.42:80`. Foreground tests may use high ports. OS installation requires ports 53 and 80.

`raymond install` creates `~/.raymond/raymond.yaml` automatically with directory mode 0700 and file mode 0600. The starter has the `raymond.test` DNS zone and an empty route list, with a commented mapping example. Add your local services there. Existing files are validated without being overwritten, including on repeat installs. Symlink config files/directories are refused. Under sudo, the invoking account determines the home directory and owns newly created config files.

`raymond config path` prints this user path. `config validate`, `doctor` and `serve` use it by default. Explicit `--file`/`--config` overrides still work. `install --config path` uses that existing file without creating a default configuration.

`main_domain` sets the full dashboard hostname. For an `.internal`-only namespace, use:

```yaml
version: 1
main_domain: raymond.internal
dns:
  zones: [internal]
  listen: 127.0.0.43:53
proxy:
  listen: 127.0.0.42:80
routes: []
```

Use `internal` for a DNS zone and `raymond.internal` for the dashboard hostname; do not include a leading dot, scheme or port. An explicit main domain must belong to a configured zone. Omitting it preserves the legacy `raymond.test` hostname, which only resolves when covered by a configured zone. DNS and HTTP serve only the exact dashboard hostname; unknown subdomains return NXDOMAIN/404. Application routes retain precedence. The same configured hostname controls browser Host/Origin checks and `raymond dashboard` launches. After changing it, run `raymond dashboard` again to open an authorized session at the new hostname.

Foreground testing can use alternate ports, for example DNS `127.0.0.43:1053` and HTTP `127.0.0.42:8080`. On macOS a reviewed `/etc/resolver/internal` file can select `nameserver 127.0.0.43` and `port 1053`; the loopback aliases must already exist. Browse `http://raymond.internal:8080`. This requires no changes to an existing `.test` resolver or CoreDNS. Native service installation still requires its supported default addresses and ports. Foreground serving alone does not enroll the management broker; dashboard controls remain unavailable until enrollment.

The embedded dashboard assets work offline. The authenticated management API remains local and requires enrollment, session/CSRF checks and the configured Host/Origin.

Zones and domains are lowercase ASCII DNS names after normalization; one final dot is ignored. Unicode is rejected: convert IDNA names to explicit punycode before configuration. `.local` zones are disabled. Hosts match a route's base domain or any descendant, with longest suffix winning at label boundaries. Explicit wildcard routes such as `"*.apps.test"` match any subdomain (including nested subdomains), while leaving the `apps.test` apex separate. Wildcards are allowed only as the initial `*.`. Quote them in YAML to avoid YAML alias syntax. A more specific base domain wins over a broader wildcard; when `apps.test` and `*.apps.test` are both configured, the former handles the apex and the latter handles descendants. All route domains must belong to a configured zone. Targets are IPv4 loopback literals with numeric ports. External targets and IPv6 are deliberately unsupported in v0.1.

Edit the user file and validate with `raymond config validate`. Privileged installation validates and copies it to `/etc/raymond/config.yaml`, which remains root-owned and not writable by other users. The service reads that protected snapshot, so run `raymond setup` to apply user edits. There is no hot reload. Enrolled dashboard CRUD stays in a browser draft until Save & Apply validates the complete config and expected revision. Uninstall preserves your user configuration.

## macOS HTTP socket coexistence

`proxy.allow_wildcard_coexistence` is an optional boolean, false by default. Set it to `true` on macOS to permit a dedicated HTTP socket to coexist with an existing wildcard listener, such as Docker Desktop's host port forwarder. Other platforms reject this option when enabled. The proxy address must still be a dedicated IPv4 loopback address, never `127.0.0.1` or a wildcard. Actual TCP/UDP bind checks remain mandatory, and the DNS wildcard guard is unchanged. An occupied exact address still fails.

```yaml
proxy:
  listen: 127.0.0.42:80
  allow_wildcard_coexistence: true
```

This changes the preflight policy only; it does not change routing, add listeners, alter Docker or terminate another process. After editing an already-installed configuration, run `raymond setup` to update the protected service snapshot.
