# Raymond

Local wildcard DNS and HTTP reverse proxying in one native Go binary.

Map a local app once and use as many nested site hostnames as your local app needs. Raymond preserves the original HTTP Host, path, query, cookies, redirects and streaming traffic, including WebSocket upgrades.

**Release 0.1.1:** native macOS 15/arm64 and Ubuntu 24.04/amd64 acceptance covers packaged setup, native DNS, crash/restart recovery, cleanup and offline runtime. Download published binaries from [GitHub Releases](https://github.com/cbspire/raymond/releases), or build from source below. See [validation evidence](docs/validation.md).

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/cbspire/raymond/main/install.sh | bash
```

Run as your normal user. The installer verifies the download, creates your user configuration, asks for administrator access to configure local DNS and the background service, and starts Raymond. It adds the command to your shell profile when needed; open a new terminal afterward. Visit [http://raymond.test](http://raymond.test).

```sh
raymond upgrade                  # Latest stable release
raymond upgrade --version 0.1.1  # A specific release
```

`raymond update` is an alias. Existing configuration is preserved. Setup briefly stops the service while updating it and restores the previous service if the update fails. To apply edited routes without downloading a release, run `raymond setup`.

## What it does

- Authoritative local DNS for configured private zones such as `.test` and `.internal`, with no upstream forwarding.
- Longest hostname-suffix routing, including each route's base hostname and arbitrarily nested subdomains.
- HTTP at `127.0.0.42:80` and DNS at `127.0.0.43:53`, leaving `127.0.0.1:80` available.
- One loopback IP/port target per route; nested hostnames work without individual registration.
- Explicit service installation and cleanup for macOS and supported systemd Linux systems.

**One native executable, no application runtime dependencies.** Raymond includes its DNS server and HTTP proxy. It needs no Go installation, Docker, Python, Node, nginx or dnsmasq to run. Linux binaries are statically linked; macOS uses only system libraries. YAML is configuration data. Installed service/split-DNS integration uses the OS's native management tools and services, described below.

**HTTP only.** HTTPS, certificates and trust-store changes are deferred to v0.2. There is no caching, compression or rewriting. This 0.1.2 feature branch adds an authenticated local dashboard and native management broker; its native acceptance checks must pass before release. `.local` is rejected because it conflicts with mDNS/Bonjour. Local processes can reach the proxy; loopback is not an authentication boundary.

## Build from source

Development needs Git, Make and **Go 1.26.10 or a later compatible patched toolchain**. Frontend changes also need Node 22.12+ (`npm ci --prefix web && npm run build --prefix web`); ordinary Go builds use the checked-in embedded assets. The compiled binary does not require Go or another language runtime.

```sh
git clone https://github.com/cbspire/raymond.git
cd raymond

make build
bin/raymond version
```

The result is `bin/raymond`. macOS and Linux builds target amd64 and arm64. To evaluate without installing Go, download a `raymond-candidate-*` artifact from the PR's **Privileged OS integration** workflow, unzip it, and use the included `scripts/install.sh` with its `dist/` archives as described in [candidate installation](docs/installation.md#install-a-candidate-without-go). The artifact includes the example YAML and installation guide. Native acceptance covers macOS arm64 and Linux amd64; the other two targets are cross-built.

## Configure a local app

Start your app on `127.0.0.1:9001`. For a disposable example, you can run `python3 -m http.server 9001 --bind 127.0.0.1` in another terminal; Python is only an example backend, not a Raymond dependency.

Initialize your user configuration automatically:

```sh
bin/raymond install
bin/raymond config path
```

This creates `~/.raymond/raymond.yaml` with private permissions, a dedicated `raymond.test` zone and an empty route list. Existing settings are validated and preserved. Without sudo, `install` prepares only this user configuration.

After privileged installation and service startup, [http://raymond.test](http://raymond.test) serves the embedded React management dashboard, including an empty service directory when no mappings exist. It is embedded in the executable, works offline and needs no extra web server. An explicit route matching `raymond.test` takes precedence over the dashboard landing page.

Edit the generated file to add your local app mappings, for example:

```yaml
version: 1
dns:
  zones: [raymond.test]
  listen: 127.0.0.43:53
proxy:
  listen: 127.0.0.42:80
routes:
  - domain: app.raymond.test
    target: 127.0.0.1:9001
```

Save your changes to `~/.raymond/raymond.yaml`, then validate it:

```sh
bin/raymond config validate
```

| Requested hostname | Backend |
| --- | --- |
| `app.raymond.test` | `127.0.0.1:9001` |
| `api.app.raymond.test` | `127.0.0.1:9001` |
| `preview.eu.app.raymond.test` | `127.0.0.1:9001` |
| `notapp.raymond.test` | No match |
| `unknown.raymond.test` | DNS NXDOMAIN / HTTP 404 |

A more-specific configured route wins over a broader suffix. Comparison is case-insensitive, ignores one final DNS dot and respects label boundaries. Use ASCII labels or explicit punycode, and IPv4 loopback literal targets. See the [configuration reference](docs/configuration.md).

## Install and enable OS integration

Review the platform requirements before running administrator commands:

| Platform | Requirements and privilege model |
| --- | --- |
| [macOS](docs/macos.md) | launchd, dedicated `lo0` aliases and owned `/etc/resolver/<zone>` files. The daemon runs as root in v0.1. |
| [Linux](docs/linux.md) | Both systemd-resolved and systemd-networkd already active; `/etc/resolv.conf` resolves to `/run/systemd/resolve/stub-resolv.conf`. The service uses DynamicUser and CAP_NET_BIND_SERVICE. |

NetworkManager-only, non-systemd and container setups are unsupported for automatic Linux integration. Raymond refuses conflicts and does not rewrite `resolv.conf` or overwrite unrelated resolver files. An existing `0.0.0.0:80` listener is refused by default; macOS users can explicitly enable the coexistence option below when their exact loopback bind is available. OS integration requires the default addresses and ports shown above.

On macOS, Docker Desktop can hold a wildcard socket even for a localhost-only published port. To permit separate-address HTTP coexistence without changing Docker, explicitly add this to your user configuration:

```yaml
proxy:
  listen: 127.0.0.42:80
  allow_wildcard_coexistence: true
```

This option defaults to false and is macOS-only. Raymond still binds exactly its dedicated loopback address, checks actual TCP/UDP availability, and rejects wildcard DNS conflicts. It does not stop or reconfigure other services. See [the Docker Desktop workaround](docs/troubleshooting.md#docker-desktop-on-macos).

The following explicit commands install a protected copy of your reviewed binary, install Raymond-owned resources, check binds, and start the service. They change OS DNS/service configuration; **review the changes before running them on your workstation**.

```sh
./bin/raymond setup       # One administrator authorization for enrollment
./bin/raymond doctor
./bin/raymond dashboard
./bin/raymond status
```

`raymond setup` enrolls the invoking ordinary account, validates its private configuration and installs independent native proxy/manager services using one protected executable. The default config is `~/.raymond/raymond.yaml`; use `--config path` for an invoking-user-owned custom file. Enrollment records that single owner, config path and CLI path outside the editable YAML and does not silently transfer them. The CLI executable must live in an owner-writable directory, such as `~/.local/bin`.

After enrollment, dashboard Save & Apply and CLI `setup`, `doctor`, `start`, `stop`, `restart`, `status`, `logs` and `upgrade` use the same private operation API without further sudo. Setup and successful CLI upgrades automatically open an authorized dashboard; `raymond dashboard` is an optional shortcut to reopen it. A short-lived private launch file delivers authorization; the token is absent from terminal output and process arguments. The dashboard shares the proxy listener and disappears when Raymond stops; the Unix-socket manager remains available for `raymond start`.

Low-level root-only `install`/`start` remains supported for compatibility, but it does not enroll dashboard management. Run normal-user `setup` once to migrate a 0.1.1 installation. Service binaries never run from your home directory. Only fixed native operations are exposed, and config/credential/CLI file I/O runs after dropping to the recorded owner's UID. See [installation details](docs/installation.md).

Verify through the normal OS resolver:

```sh
curl --noproxy '*' http://api.app.raymond.test/
curl --noproxy '*' http://preview.eu.app.raymond.test/
```

You should receive your app's HTTP response. `curl --resolve` can diagnose the proxy directly but does not validate OS DNS integration. See [installation details](docs/installation.md) for foreground debugging and platform-specific DNS checks.

## Live request logs

```sh
./bin/raymond logs                     # Last 50 lines, then follow new requests
./bin/raymond logs --lines 100         # Start with more history
./bin/raymond logs --no-follow         # Print recent lines and exit
```

Press Ctrl+C to stop following. Each completed HTTP request records its method, hostname, escaped path, response status, response bytes and duration. Query strings, authorization headers, cookies and request/response bodies are excluded. Streaming requests and WebSockets are logged when they finish. macOS follows `/var/log/raymond.log` across file rotation; Linux follows only `raymond.service` in the journal. Enrolled owners receive a redacted stream through the manager without journal-group membership or sudo. Foreground `serve` writes the same request events to stderr.

Software updates use verified native archives from the fixed Raymond release source. `restart` runs the installed version; it does not install an arbitrary local rebuild.

## Stop, update and uninstall

```sh
./bin/raymond stop
./bin/raymond start
./bin/raymond restart
sudo ./bin/raymond uninstall
```

These are separate lifecycle examples. Uninstall also stops the service if needed. It removes only recorded owned resources and refuses operator-modified installed files. Your `~/.raymond/raymond.yaml`, downloaded binary remain; empty directories/logs may also remain.

For config changes, use dashboard Save & Apply or edit your user YAML and run `raymond setup`. Stale revisions are rejected, failed activation keeps saved changes pending, and Retry/Discard resolve them. Use `raymond upgrade` for a newer management-compatible stable release (0.1.2 or later); downloaded shell installers are not executed by the manager. Manual lifecycle commands remain available; changing protected installed files directly prevents safe cleanup. No hot reload is provided.

## Download options

The one-command installer resolves the latest stable release once, then fetches its installer and checksum-verified binary from that immutable tag. Release tags have no `v` prefix. You can also download and inspect the script before running it:

```sh
curl -fsSL https://raw.githubusercontent.com/cbspire/raymond/0.1.1/scripts/install.sh -o install-raymond.sh
less install-raymond.sh
RAYMOND_VERSION=0.1.1 bash install-raymond.sh
```

Set `RAYMOND_BINARY_ONLY=1` to download only the executable, without configuration, administrator access or shell-profile edits. For offline archives, also set `RAYMOND_ARCHIVE_DIR=/absolute/path/to/dist` and pin `RAYMOND_VERSION`. `RAYMOND_INSTALL_DIR` overrides the default `~/.local/bin`. Checksums provide integrity from the release source; they are not independent signing/provenance.

## Develop and validate

```sh
make check
make integration
bash scripts/test-installer.sh
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
```

`make check` runs formatting checks, vet, pinned static analysis, unit/race tests and a static build. First-time tooling/dependency installation needs Internet access. Normal serving contacts only configured local backends; explicit update checks/upgrades contact the fixed release source. `make snapshot` needs GoReleaser v2.16.0 and builds all four native archives without publishing.

Privileged acceptance tests run separately on disposable macOS/Linux PR runners. For a manual run, prepare GoReleaser snapshot archives in `dist/`, then run `RAYMOND_PRIVILEGED_SMOKE=1 scripts/smoke.sh` only on a reviewed disposable supported runner. The compiled test helpers are CI tools, not part of the distributed product. Release publication is gated on those tests, security checks and configured approval rules.

The vulnerability scan runs on every PR. CodeQL runs automatically for public repositories; for a private repository, enable code scanning in Settings → Code security and set the repository Actions variable `RAYMOND_CODEQL_ENABLED=true`. Otherwise CodeQL is explicitly reported as skipped. See [security configuration](docs/security.md).

Further reading: [installation](docs/installation.md), [troubleshooting](docs/troubleshooting.md), [architecture](docs/architecture.md), [security](docs/security.md), [contributing](CONTRIBUTING.md), and [license](LICENSE).
