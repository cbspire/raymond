# Raymond — Codex Implementation Plan

> **Project:** Raymond  
> **Tagline:** Elegant. Resourceful. Always in control.  
> **Goal:** A self-contained, open-source local wildcard DNS and HTTP reverse-proxy manager for macOS and Linux.  
> **Implementation language:** Go.  
> **Release scope:** v0.1.0 = DNS + HTTP proxy only. **HTTPS is explicitly deferred to v0.2.0.**

## 1. Codex operating instructions

You are the implementation engineer for a new open-source Go repository named `raymond`. Build a working, tested, documented product, not a proof-of-concept or pseudocode. Apply the architecture, constraints, acceptance tests, Git and release requirements below. Proceed in small, reviewable changes; run tests before committing; keep the repository buildable after every milestone. Do not expand scope without a documented reason. Never claim cross-platform integration works until it has been exercised on real macOS and Linux runners.

**Non-negotiables**

1. All user-facing prose, code, comments, commit messages and documentation must be in English.
2. Single native binary, statically linked where feasible (`CGO_ENABLED=0`), no Ruby, Rust, Node.js, Caddy, nginx or dnsmasq runtime dependency.
3. Supports macOS `darwin/arm64`, `darwin/amd64`, Linux `linux/amd64`; add `linux/arm64` if trivial (recommended).
4. Do **not** occupy `127.0.0.1:80`, `127.0.0.1:443`, `0.0.0.0:80` or `0.0.0.0:443`.
5. All proxies bind to a **dedicated loopback address**, default `127.0.0.42:80`. DNS binds to a different loopback address, default `127.0.0.43:53`. Avoid `127.0.0.53`/`.54` on Linux: these may be used by `systemd-resolved`.
6. Supports multiple private DNS zones (`test`, `internal`, etc.) with arbitrarily nested hostnames. `.local` must be disabled or warned against by default because it collides with mDNS/Bonjour.
7. Routes hostnames by the longest matching suffix, with one configurable backend IP and port per route. Preserve `Host` and the original URL/path/query.
8. In v0.1, terminate **HTTP only**. No HTTPS, certificate authority, compression, caching, rewriting, custom rules, dashboard, Docker requirement, external DNS or remote-control HTTP API.
9. Fully offline at runtime. `curl` is required only by the remote installer; Raymond itself performs no outbound network requests.
10. Installer must be safe, reproducible, idempotent, and reversible. Do not silently overwrite other developers' resolver files or networking settings.

## 2. User experience / concrete behavior

Example user configuration (schema subject to versioning, not arbitrary variation):

```yaml
version: 1

dns:
  zones:
    - test
    - internal
  listen: 127.0.0.43:53

proxy:
  listen: 127.0.0.42:80

routes:
  - domain: app.test
    target: 127.0.0.1:9001

  - domain: api.test
    target: 127.0.0.1:9002

  - domain: demo.internal
    target: 127.0.0.2:8080
```

Expected routes:

| Requested URL | Target |
| --- | --- |
| `http://tenant.app.test/` | `127.0.0.1:9001` |
| `http://preview.app.test/admin/` | `127.0.0.1:9001` |
| `http://tenant.api.test/` | `127.0.0.1:9002` |
| `http://preview.eu.api.test/` | `127.0.0.1:9002` |
| `http://demo.internal/` | `127.0.0.2:8080` |
| `http://unknown.test/` | Raymond HTTP `404`, not an arbitrary backend |

The root host `app.test` also routes to its backend. A more-specific route suffix wins if both `test` and `app.test` are explicitly configured. Domain comparison must be normalized (case-insensitive, trailing dot ignored), and suffix matching must respect label boundaries: `notapp.test` must **not** match `app.test`.

Users configure a base domain once. New subdomains route to the same backend without further Raymond configuration.

Command-line design:

```sh
raymond version
raymond doctor                   # Preflight: ports, loopback, privileges, DNS conflicts
raymond config path
raymond config validate [--file path]
raymond install                  # Explicit privileged OS integration, idempotent
raymond start                    # Run using installed OS service
raymond stop
raymond restart
raymond status
raymond uninstall                # Only remove Raymond-owned changes
raymond serve --config path      # Foreground mode for debugging/testing
```

No `raymond add domain ...` CRUD API for v0.1; editing YAML is sufficient. A controlled restart on config changes is acceptable; no hot reload required for v0.1. The CLI must clearly indicate when elevation is required rather than silently prompting in a pipe.

## 3. Technical architecture

### 3.1 Internal modules

- `cmd/raymond`: CLI entrypoint; version fields injected by build.
- `internal/config`: YAML parsing (`gopkg.in/yaml.v3`), defaults, validation and safe loading.
- `internal/router`: deterministic hostname normalization, longest-suffix matching, backend selection.
- `internal/dns`: minimal authoritative DNS resolver using `github.com/miekg/dns`; support A and AAAA (return A for matching names; return NOERROR/NODATA for unsupported AAAA when IPv6 proxy is unavailable); no upstream forwarding in v0.1.
- `internal/proxy`: Go `net/http` + `net/http/httputil.ReverseProxy`; HTTP/1.1 and WebSocket upgrades; preserve Host, method, path, raw query, redirects, streaming, and application cookies.
- `internal/platform/darwin`: macOS loopback alias lifecycle, `/etc/resolver/<zone>` management, `launchd` lifecycle.
- `internal/platform/linux`: loopback support and `systemd-resolved` route-only DNS integration; `systemd` service installation.
- `internal/install`: executable/data installation and cleanup, conflict checks, rollback, backup handling.
- `internal/diagnostics`: preflight checks and actionable `doctor` messages.

Use interfaces where needed for testability, not a generic plugin system. Avoid CGo and external binaries at runtime except OS-native management tools invoked by `install`, `start`, `stop`, and `uninstall`.

### 3.2 DNS

- Raymond is authoritative only for its configured private zones; the OS resolver routes those zones to Raymond using split DNS. All other queries remain handled by the existing system resolver.
- For names belonging to a configured zone, answer A with `127.0.0.42` when the hostname matches a configured route; for unknown names, prefer NXDOMAIN. This prevents accidental interception of unrelated local hostnames while retaining wildcard matches for all nested hostnames.
- Decide whether the route's base hostname is a valid match (yes); handle deeply nested names. Reject invalid DNS labels, duplicate routes, ambiguous targets and domain-to-zone mismatches.
- DNS query handling must be bounded, race-safe and minimally allocated; no network access to upstream resolvers.
- Default localhost-only interfaces; refuse to bind public/LAN addresses unless a future explicit feature is added.
- Ensure resolver files target `127.0.0.43`, and use port `53` initially for maximum OS compatibility. Consider an unprivileged custom port **only if both OS integrations are proven end-to-end**; do not assume Linux and macOS support it identically.

### 3.3 HTTP proxy

- Bind exactly `127.0.0.42:80`; never bind `:80`/`0.0.0.0:80`.
- Create one ReverseProxy with a per-request selected backend, rather than a listener per hostname/site.
- Preserve the original `Host` in upstream requests, so applications can route by hostname.
- Only overwrite/set forwarding headers according to an explicit trust policy; discard untrusted client-supplied `Forwarded`, `X-Forwarded-For`, `X-Forwarded-Host`, and `X-Forwarded-Proto` before inserting known-good values. v0.1 sets `X-Forwarded-Proto: http`.
- Use conservative timeouts for dial, response-header wait and idle connection; **do not enforce a short total request timeout** because imports, administrative requests and background jobs can run longer.
- Support streaming, large POST bodies, WebSocket upgrade, `Location`, cookies and `Set-Cookie` without rewriting by default. Do not add gzip, cache, redirects or custom headers except essential forwarding headers.
- Return useful `404` for unmatched hostnames, `502` when a backend is unreachable, and logs without secrets.
- Restrict route targets to IP literals (IPv4 loopback by default). Explicitly validate any broader address support before allowing it: Raymond should not become a general SSRF-capable LAN proxy.

### 3.4 Platform integration and privilege model

**macOS**

- Ensure `127.0.0.42` and `127.0.0.43` are reachable on `lo0`; create loopback aliases only when absent, and record which changes Raymond actually made.
- Use one `/etc/resolver/<zone>` file per DNS zone; refuse to overwrite preexisting unrelated files without an explicit operator decision. Files must contain an unambiguous Raymond ownership marker.
- Install a dedicated `launchd` service for auto-start. Privileged ports and DNS files require elevation; handle authorization explicitly.
- Keep the privileged surface minimal: do not expose a remotely reachable control socket, do not run user-supplied commands as root, and use root-owned installation/service files. If root privilege is required to keep port 53/80 open, document the tradeoff and privilege-isolation roadmap; do not pretend the service is unprivileged.
- `uninstall` removes only owned files/aliases/service definitions and restores prior state when it can prove ownership. Never delete a non-Raymond resolver file.

**Linux**

- Target modern systemd distributions with `systemd-resolved` for v0.1 (Ubuntu/Debian/Fedora as supported by CI and docs); make unsupported resolvers fail clearly rather than rewiring `/etc/resolv.conf` blindly.
- Use a route-only DNS integration for each zone, with no changes to unrelated DNS configuration.
- Use a dedicated systemd service, loopback-only bind and the minimum required capability for privileged ports (`CAP_NET_BIND_SERVICE`); use hardening directives when compatible with system resolver integration.
- Do not conflict with the default `systemd-resolved` stub (`127.0.0.53`/`127.0.0.54`).
- Installer/uninstaller must preserve unrelated systemd drop-ins.

**Security acceptance:** Running a web server on `127.0.0.1:80` before launching Raymond must continue working afterward. An existing listener on `0.0.0.0:80` will conflict with Raymond: `doctor` must flag it, not terminate the other service.

## 4. Git repository structure

```text
raymond/
├── .github/
│   ├── workflows/
│   │   ├── ci.yml
│   │   ├── integration.yml
│   │   ├── security.yml
│   │   └── release.yml
│   ├── dependabot.yml
│   ├── CODEOWNERS
│   └── PULL_REQUEST_TEMPLATE.md
├── cmd/raymond/main.go
├── internal/
│   ├── config/
│   ├── dns/
│   ├── router/
│   ├── proxy/
│   ├── platform/{darwin,linux}/
│   ├── install/
│   └── diagnostics/
├── test/
│   ├── integration/
│   └── fixtures/
├── scripts/
│   ├── install.sh
│   └── smoke.sh
├── docs/
│   ├── architecture.md
│   ├── configuration.md
│   ├── macos.md
│   ├── linux.md
│   ├── troubleshooting.md
│   ├── security.md
│   └── future-https.md
├── examples/raymond.yaml
├── .goreleaser.yaml
├── .golangci.yml
├── .editorconfig
├── .gitignore
├── .gitattributes
├── Makefile
├── go.mod
├── go.sum
├── README.md
├── LICENSE
├── CONTRIBUTING.md
├── SECURITY.md
├── CHANGELOG.md
└── AGENTS.md
```

Do not create empty scaffolding files with TODO-only content. Add them together with functional implementation.

## 5. Implementation milestones — use separate branches/PRs

### M0 — Repository foundation

- Initialize module (`github.com/OWNER/raymond` is a **placeholder** until repository owner is known).
- Write README with scope, limitations, compatibility, architecture, example mapping, and v0.1/v0.2 roadmap.
- Define `AGENTS.md` with constraints, coding conventions, validation commands, no unreviewed privileged changes and the completion checklist.
- Add Makefile targets `fmt`, `lint`, `test`, `test-race`, `build`, `integration`, `snapshot`, `check`.
- Add minimal CLI `version` and `config validate`, plus config and routing tests.
- Acceptance: `make check` works on a clean checkout.

### M1 — Config and deterministic routing

- Parse schema with strict validation; reject unknown keys to catch typos.
- Normalization: case-insensitive, remove final DNS dot, IDNA/punycode decision documented, enforce DNS label boundaries.
- Longest-suffix matcher; explicit valid apex behavior; 10 routes/50 host fixture.
- Table-driven and property/fuzz tests including lookalike suffixes, invalid config, duplicate domains, overlapping routes.
- Acceptance: all routes work without running DNS or proxy.

### M2 — HTTP reverse proxy

- Dynamic backend selection per request; preserve original Host.
- Unit/integration tests using `httptest`, streaming, large POST, redirects, cookies, WebSockets, unknown hosts, down backends and connection reuse.
- A foreground test server must be able to listen on an unprivileged port for CI without root.
- Acceptance: `curl --resolve tenant.app.test:80:127.0.0.42 http://tenant.app.test/` reaches the correct backend when the installed proxy is running. Unit tests should use an alternate test listen port instead of privileged 80.

### M3 — Authoritative local DNS

- A/AAAA resolution with `miekg/dns`; zone boundary checks; no forwarding.
- Tests for `tenant.app.test`, deep names, multiple configured zones, unknown hosts, AAAA and non-A types.
- Integration tests use configurable high DNS port on loopback; platform installation later uses real port 53.
- Acceptance: test resolver queries return Raymond proxy address only for valid matched hostnames.

### M4 — OS integration, lifecycle and cleanup

- `doctor`, `install`, `start`, `stop`, `status`, `uninstall` on macOS and Linux.
- Fail closed when DNS route ownership, privileged port bind, loopback alias or systemd-resolved compatibility is ambiguous.
- Persist one root-owned system config at a documented path (e.g. `/etc/raymond/config.yaml`), with safe file permissions, atomic replacement and pre-validation. Do not let a root service execute shell snippets or arbitrary configuration paths supplied by untrusted users.
- Distinguish CLI user mode and privileged installation mode; explicitly document any admin permissions.
- Acceptance: install → start → resolve → proxy → stop → uninstall → verify restoration, on both OSes.

### M5 — Packaging, installer, CI and release

- GoReleaser matrix and SHA-256 manifest.
- GitHub Actions PR CI, privileged OS smoke, vulnerability analysis and release workflow.
- Remote installer with architecture detection and verified archive checksum.
- Documentation with reproducible first-run instructions and simple troubleshooting.
- Acceptance: a fresh macOS or Linux machine can download a released archive, install, configure and remove Raymond without Go installed.

### M6 — Release candidate and v0.1.0

- Run a 10-route/50-hostname fixture on macOS; ensure the backend receives the correct `Host`.
- Test concurrent agent HTTP requests and keep-alive; measure latency/RSS for diagnostic purposes only, no invented throughput SLA.
- Validate localhost:80 is unaffected, loopback-only exposure, OS restoration, error paths and no external runtime traffic.
- Publish only once all required CI and OS integration checks pass.

## 6. Git and collaboration policy

- Default branch `main`; short-lived feature branches `feat/...`, `fix/...`, `docs/...`, `ci/...`. Use PRs, squash merge, and a linear history. No direct push to protected `main`.
- Conventional Commits (`feat:`, `fix:`, `test:`, `docs:`, `ci:`, `chore:`). Semantic versioning (`v0.1.0` tag; preserve `v` in releases). Release notes generated from conventional commits and reviewed before publishing.
- PR description must state purpose, design choice, risk, tests executed, docs changes and screenshots/logs if OS integration changes. Avoid unrelated formatting/refactors.
- Required checks on `main`: format, `go vet`, lint, unit tests + race detector, integration tests, vulnerability scan, build matrix. Enable branch protection or a repository ruleset and required status checks through repository settings.
- Enable Dependabot for Go modules and GitHub Actions, weekly updates, grouping minor/patch dependencies. Enable secret scanning where available. Use `CODEOWNERS` for security-critical `internal/platform`, `internal/install`, and `.github/workflows`.
- Restrict workflows' `GITHUB_TOKEN` permissions to `contents: read`; only the release job gets `contents: write` and, if signing, `id-token: write`. Do not expose write tokens to untrusted PRs.
- Use current stable major-version tags for GitHub Actions (for example, `actions/checkout@v7`). These tags receive minor and patch updates automatically; Dependabot proposes new major versions for review.
- Signing and GitHub attestation of release artifacts are recommended before public distribution; checksum alone does not authenticate an artifact if the source of the checksum is compromised.

## 7. Complete CI/CD specification

### 7.1 `.github/workflows/ci.yml` (every PR and `main` push)

- Check out source; set up Go from `go.mod` with module caching.
- Format check: `test -z "$(gofmt -l .)"` (limit to Go sources; handle paths correctly).
- `go mod tidy` verification with a clean Git diff, `go vet ./...`.
- Run `golangci-lint` v2 with a version pinned in repo; use the official action.
- Run `go test ./...` on `ubuntu-latest` and `macos-latest`.
- Run `go test -race ./...` on at least Linux AMD64 and macOS ARM64 when runners permit.
- Cross-compile with `CGO_ENABLED=0` for `darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64`; verify all artifacts exist.
- Separate unit tests from privileged OS smoke tests so contributor PRs don't need to change host DNS settings during ordinary tests.

### 7.2 `.github/workflows/integration.yml` (PRs where safe + main)

- Run privileged smoke tests on clean macOS and Linux GitHub-hosted runners only after reviewing runner constraints.
- Start disposable localhost backend listeners on ports 9001 and 9002.
- Test a real Raymond `127.0.0.42:80` proxy while a separate preexisting `127.0.0.1:80` listener remains responsive.
- Validate private-zone DNS resolution from OS APIs, not just direct `dig` to the Raymond DNS socket.
- Test a route whose suffix is `app.test` and clients `tenant.app.test`, `deep.tenant.app.test`.
- Test `install`, `stop`, `uninstall` cleanup and make sure unrelated `/etc/resolver` or systemd config is unmodified.
- Use `trap` cleanup even when a test fails. If a hosted runner cannot support system-level tests, use dedicated ephemeral macOS/Linux runners rather than marking these tests as passing.

### 7.3 `.github/workflows/security.yml` (PR + weekly scheduled)

- `govulncheck ./...` for dependencies.
- CodeQL for Go (official `github/codeql-action@v4`) if enabled for the repo.
- Dependency review on PR where supported; `go vet` and static linting in ordinary CI.
- Secret scanning/Dependabot security alerts enabled in repository settings.
- Threat-model tests for DNS zone escaping, host spoofing, proxy target validation, privileged config files and unsafe uninstall.

### 7.4 `.github/workflows/release.yml` (protected tags `v*`)

- `checkout` with full git history, set up Go, run tests and GoReleaser v2 through `goreleaser/goreleaser-action@v7` (or the current stable major tag).
- Build native binaries, e.g. archives:
  - `raymond_0.1.0_darwin_arm64.tar.gz`
  - `raymond_0.1.0_darwin_amd64.tar.gz`
  - `raymond_0.1.0_linux_amd64.tar.gz`
  - `raymond_0.1.0_linux_arm64.tar.gz`
- Include `raymond` executable, LICENSE and README in archives; publish `checksums.txt`.
- Set `CGO_ENABLED=0`, `-trimpath` and version/commit injected via `-ldflags`.
- Sign/attest artifacts where configured, create GitHub Release with notes, flag pre-1.0 releases as prerelease if desired.
- Do not automatically release to Homebrew until the primary installer is stable.
- For a retag or replay, fail safely; never silently overwrite published release binaries.

## 8. curl | bash installation — requirements

**Planned public command (not a live URL until the repository exists):**

```sh
curl -fsSL https://raw.githubusercontent.com/OWNER/raymond/main/scripts/install.sh | bash
```

After download, a separate explicit administrator action performs OS setup:

```sh
sudo raymond install
raymond doctor
raymond start
```

The remote script must **only install the binary**; it must not silently run `sudo`, alter DNS settings, install system services or execute Raymond as root. For higher-assurance use, document a two-step download/inspection path as the preferred alternative to piping remote code into a shell.

Installer acceptance:

1. `#!/usr/bin/env bash`, `set -euo pipefail`; support Bash versions shipped with macOS. No `jq` or Go requirement.
2. Detect OS via `uname -s` and arch via `uname -m` (`arm64`/`aarch64`, `x86_64`/`amd64`); reject unsupported platforms.
3. Support pinned version via `RAYMOND_VERSION=v0.1.0`; default to stable latest GitHub Release only when online.
4. Download archive and `checksums.txt` from the **same immutable release tag** via HTTPS. Validate exact archive filename and SHA-256 using `shasum -a 256` on macOS or `sha256sum` on Linux. Fail if mismatch; never execute a downloaded archive before verification.
5. Use `mktemp -d`, `trap` cleanup, `tar` path validation and no arbitrary pathname extraction into privileged directories. Do not use `eval` or execute GitHub API payloads.
6. Install atomically to `~/.local/bin/raymond` by default, without sudo. Expose `RAYMOND_INSTALL_DIR` for overrides, validate the path and provide a PATH hint. The OS service installer must know the chosen trusted absolute executable path and enforce ownership/permission checks.
7. Print binary version and exact next steps. No network access is needed after installation and release retrieval.
8. Handle failed download, 404, checksum mismatch, unsupported OS/arch, missing permissions, interrupted installation and upgrading an existing binary.
9. Consider checksum signature / GitHub artifact attestation verification for public releases, since HTTPS + checksum provides integrity but not independent provenance.

Before release, update `OWNER` to the repository owner in README, script and CI; do not publish dead installation URLs.

## 9. Tests and definition of done

**Core test matrix**

| Scenario | Expected |
| --- | --- |
| `tenant.app.test` | Correct route target |
| `deep.tenant.app.test` | Same route target |
| `app.test` | Same route target |
| `site.other.web.test` | Other route only if configured |
| Hostname lookalikes | No false suffix match |
| 10 routes / 50 sites | Deterministic correct routes |
| `*.test` and `*.internal` | Scoped DNS works for both zones |
| `.local` | Warn/disable by default |
| Backend down | Clean 502 |
| Unknown hostname | NXDOMAIN at DNS or 404 at proxy |
| POST with multipart uploads | Unmodified request body and headers |
| Session cookies | No cookie rewrite |
| Application redirects | Unmodified Location unless a specific bug is proven |
| WebSocket / long-running upload | No early termination |
| localhost:80 already in use | Both servers coexist |
| `0.0.0.0:80` already in use | Clear conflict diagnostic |
| macOS install/uninstall | Only owned resources changed |
| Linux install/uninstall | Only owned resources changed |
| Unexpected crash/restart | Recoverable, owned resolver state consistent |
| Offline runtime | Operates with network disconnected |
| Release installer | Checksum verified, no implicit sudo |

**Completion criteria:** installation and cleanup tested on real supported OS versions; all required checks green; docs include actual copy/paste commands for an existing local HTTP app; release binary can be downloaded and run without Go; no functionality implemented outside v0.1 scope.

## 10. Deferred v0.2.0: HTTPS — do not implement in v0.1

- Enable TLS at `127.0.0.42:443`, leaving `127.0.0.1:443` untouched.
- Create a local root CA with secure private-key storage and explicit trust/untrust commands on macOS and Linux.
- Generate leaf certificates **per requested hostname using SNI** and cache securely. A `*.test` cert does not cover `tenant.app.test`.
- Correctly set `X-Forwarded-Proto: https` to backend, without enabling it during HTTP requests. Document application configuration for trusted reverse-proxy HTTPS detection separately.
- Add tests for SANs, cert renewal, permission boundaries, trust-store changes, invalid hostnames and cleanup.
- Make HTTPS opt-in first; avoid surprise browser redirects or automatic HSTS.
- Keep the same YAML route mapping and routing, adding TLS configuration without a breaking schema migration.

## 11. Codex execution protocol

Implement in the milestone order above. For each milestone:

1. Summarize files to change and intended behavior briefly.
2. Write failing tests for routing/DNS/proxy behavior before implementation when possible.
3. Implement the smallest correct change and run appropriate tests/linters.
4. Inspect diff for accidental scope creep, privileged operations, hidden external dependencies and Host-header regressions.
5. Create a focused Conventional Commit on a feature branch. Open a PR or provide a PR-ready diff and test summary.
6. Record known limitations honestly; never mark OS tests as passing when they have not run.

The v0.1 implementation is finished only when its actual executable, installer, CI workflows and operating-system integration work. Do not stop after writing a plan or generating empty files.

### Reference documentation

- [Go reverse proxy](https://pkg.go.dev/net/http/httputil#ReverseProxy)
- [miekg/dns](https://github.com/miekg/dns)
- [GoReleaser](https://goreleaser.com/)
- [GoReleaser GitHub Action](https://github.com/goreleaser/goreleaser-action)
- [golangci-lint action](https://github.com/golangci/golangci-lint-action)
- [CodeQL Action](https://github.com/github/codeql-action)
- [systemd-resolved](https://www.freedesktop.org/software/systemd/man/latest/systemd-resolved.service.html)
- [SideDNS (reference, not a dependency)](https://github.com/Wilfried-Tech/sidedns)
