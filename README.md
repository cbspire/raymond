# Raymond

Local wildcard DNS and HTTP reverse proxying in one native Go binary.

Map an installation once and use as many nested site hostnames as your local app needs. Raymond preserves the original HTTP Host, path, query, cookies, redirects and streaming traffic, including WebSocket upgrades.

**Current status:** v0.1 is a tested release candidate. Real macOS 15/arm64 and Ubuntu 24.04/amd64 jobs pass packaged installation, native DNS, crash/restart recovery, cleanup and offline-runtime acceptance. Candidate archives are downloadable from the PR's **Privileged OS integration** workflow artifacts. No stable GitHub release is published yet. See [validation evidence](docs/validation.md).

## What it does

- Authoritative local DNS for configured private zones such as `.test` and `.internal`, with no upstream forwarding.
- Longest hostname-suffix routing, including the installation's own hostname and arbitrarily nested subdomains.
- HTTP at `127.0.0.42:80` and DNS at `127.0.0.43:53`, leaving `127.0.0.1:80` available.
- One loopback IP/port target per installation; no registration of individual WordPress multisite blogs.
- Explicit service installation and cleanup for macOS and supported systemd Linux systems.

**One native executable, no application runtime dependencies.** Raymond includes its DNS server and HTTP proxy. It needs no Go installation, Docker, Python, Node, nginx or dnsmasq to run. Linux binaries are statically linked; macOS uses only system libraries. YAML is configuration data. Installed service/split-DNS integration uses the OS's native management tools and services, described below.

**HTTP only.** HTTPS, certificates and trust-store changes are deferred to v0.2. There is no caching, compression, rewriting, dashboard or remote-control API. `.local` is rejected because it conflicts with mDNS/Bonjour. Local processes can reach the proxy; loopback is not an authentication boundary.

## Build the current candidate

Development needs Git, Make and **Go 1.26.10 or a later compatible patched toolchain**. The compiled binary does not require Go or another language runtime.

```sh
git clone https://github.com/cbspire/raymond.git
cd raymond

# Until PR #1 is merged, check out the candidate for review:
git fetch origin pull/1/head
git switch --detach FETCH_HEAD

make build
bin/raymond version
```

The result is `bin/raymond`. macOS and Linux builds target amd64 and arm64. To evaluate without installing Go, download a `raymond-candidate-*` artifact from the PR's **Privileged OS integration** workflow, unzip it, and use the included `scripts/install.sh` with its `dist/` archives as described in [candidate installation](docs/installation.md#install-a-candidate-without-go). The artifact includes the example YAML and installation guide. Native acceptance covers macOS arm64 and Linux amd64; the other two targets are cross-built.

## Configure a local app

Start your app on `127.0.0.1:9001`. For a disposable example, you can run `python3 -m http.server 9001 --bind 127.0.0.1` in another terminal; Python is only an example backend, not a Raymond dependency.

Copy [examples/raymond.yaml](examples/raymond.yaml) to your own config and edit the targets. Keep this source config for later reinstallations. For example:

```yaml
version: 1
dns:
  zones: [test, internal]
  listen: 127.0.0.43:53
proxy:
  listen: 127.0.0.42:80
installations:
  - domain: install01.web.test
    target: 127.0.0.1:9001
```

Save this as `raymond.yaml`, then validate it:

```sh
bin/raymond config validate --file raymond.yaml
```

| Requested hostname | Backend |
| --- | --- |
| `install01.web.test` | `127.0.0.1:9001` |
| `site.install01.web.test` | `127.0.0.1:9001` |
| `dealer.eu.site.install01.web.test` | `127.0.0.1:9001` |
| `evilinstall01.web.test` | No match |
| `unknown.test` | DNS NXDOMAIN / HTTP 404 |

A more-specific configured installation wins over a broader suffix. Comparison is case-insensitive, ignores one final DNS dot and respects label boundaries. Use ASCII labels or explicit punycode, and IPv4 loopback literal targets. See the [configuration reference](docs/configuration.md).

## Install and enable OS integration

Review the platform requirements before running administrator commands:

| Platform | Requirements and privilege model |
| --- | --- |
| [macOS](docs/macos.md) | launchd, dedicated `lo0` aliases and owned `/etc/resolver/<zone>` files. The daemon runs as root in v0.1. |
| [Linux](docs/linux.md) | Both systemd-resolved and systemd-networkd already active; `/etc/resolv.conf` resolves to `/run/systemd/resolve/stub-resolv.conf`. The service uses DynamicUser and CAP_NET_BIND_SERVICE. |

NetworkManager-only, non-systemd and container setups are unsupported for automatic Linux integration. Raymond refuses conflicts and does not rewrite `resolv.conf` or overwrite unrelated resolver files. An existing `0.0.0.0:80` listener conflicts; stop or reconfigure it yourself. OS integration requires the default addresses and ports shown above.

The following explicit commands copy your reviewed binary into a root-owned bootstrap directory, install Raymond-owned resources, check binds, and start the service. They change OS DNS/service configuration; **review the changes before running them on your workstation**.

```sh
sudo install -d -o root -m 755 /var/lib/raymond-bootstrap
sudo install -o root -m 755 bin/raymond /var/lib/raymond-bootstrap/raymond
sudo /var/lib/raymond-bootstrap/raymond install --config "$PWD/raymond.yaml"
sudo /var/lib/raymond-bootstrap/raymond doctor
sudo /var/lib/raymond-bootstrap/raymond start
/var/lib/raymond-bootstrap/raymond status
```

`install` and `start` are separate actions. The service uses a root-owned copy of the binary and `/etc/raymond/config.yaml`; it does not execute from your checkout or `~/.local/bin`. macOS stores that service binary under `/Library/PrivilegedHelperTools`, outside Homebrew-managed directories. The bootstrap path and its ancestors must also be root-owned and not group/world writable; do not change ownership of unrelated directories to bypass a refusal.

Verify through the normal OS resolver:

```sh
curl --noproxy '*' http://site.install01.web.test/
curl --noproxy '*' http://dealer.eu.site.install01.web.test/
```

You should receive your app's HTTP response. `curl --resolve` can diagnose the proxy directly but does not validate OS DNS integration. See [installation details](docs/installation.md) for foreground debugging and platform-specific DNS checks.

## Stop, update and uninstall

```sh
sudo /var/lib/raymond-bootstrap/raymond stop
sudo /var/lib/raymond-bootstrap/raymond start
sudo /var/lib/raymond-bootstrap/raymond restart
sudo /var/lib/raymond-bootstrap/raymond uninstall
```

These are separate lifecycle examples. Uninstall also stops the service if needed. It removes only recorded owned resources and refuses operator-modified installed files. Your source config, downloaded binary and bootstrap copy remain; empty directories/logs may also remain.

For a config change or binary upgrade, **uninstall with the existing binary first**, then edit your source YAML or replace the bootstrap binary, validate and install again. An identical reinstall is a no-op; changing installed files directly prevents safe cleanup. No hot reload is provided.

## Install a released binary

**Future release path:** these commands require an existing `v0.1.0` release and will not work before it is published. Check [GitHub Releases](https://github.com/cbspire/raymond/releases) first. Source builds above are the current evaluation path.

Prefer downloading and inspecting the installer before running it:

```sh
curl --proto '=https' --proto-redir '=https' -fsSL \
  https://raw.githubusercontent.com/cbspire/raymond/v0.1.0/scripts/install.sh \
  -o install-raymond.sh
less install-raymond.sh
RAYMOND_VERSION=v0.1.0 bash install-raymond.sh
export PATH="$HOME/.local/bin:$PATH"
raymond version
```

The script detects macOS/Linux and amd64/arm64, downloads the archive and SHA-256 manifest from the same immutable release tag, verifies the checksum and archive contents, checks that the binary runs, then atomically installs it to `~/.local/bin/raymond`. It never invokes sudo or changes DNS/services. Checksums provide integrity, not independent signing/provenance.

Pinned candidate versions such as `v0.1.0-rc.g<commit>` are also accepted. Use `RAYMOND_ARCHIVE_DIR=/absolute/path/to/dist` to install already-downloaded candidate archives without curl or network access; a pinned version is required in that mode. Omit `RAYMOND_VERSION` in online mode to select the latest stable release. Override the user binary directory with an absolute `RAYMOND_INSTALL_DIR`, for example `RAYMOND_INSTALL_DIR="$HOME/bin"`. Directory and symlink destinations are refused. To enable OS integration, review the downloaded binary, substitute `"$HOME/.local/bin/raymond"` for `bin/raymond` in the bootstrap-copy command above, and follow the same explicit setup steps.

## Develop and validate

```sh
make check
make integration
bash scripts/test-installer.sh
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
```

`make check` runs formatting checks, vet, pinned static analysis, unit/race tests and a static build. First-time tooling/dependency installation needs Internet access. Raymond itself only contacts configured local backends at runtime. `make snapshot` needs GoReleaser v2.16.0 and builds all four native archives without publishing.

Privileged acceptance tests run separately on disposable macOS/Linux PR runners. For a manual run, prepare GoReleaser snapshot archives in `dist/`, then run `RAYMOND_PRIVILEGED_SMOKE=1 scripts/smoke.sh` only on a reviewed disposable supported runner. The compiled test helpers are CI tools, not part of the distributed product. Release publication is gated on those tests, security checks and configured approval rules.

The vulnerability scan runs on every PR. CodeQL runs automatically for public repositories; for a private repository, enable code scanning in Settings → Code security and set the repository Actions variable `RAYMOND_CODEQL_ENABLED=true`. Otherwise CodeQL is explicitly reported as skipped. See [security configuration](docs/security.md).

Further reading: [installation](docs/installation.md), [troubleshooting](docs/troubleshooting.md), [architecture](docs/architecture.md), [security](docs/security.md), [contributing](CONTRIBUTING.md), and [license](LICENSE).
