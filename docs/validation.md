# v0.1 implementation validation

This is an implementation candidate, **not a published or certified v0.1.0 release**. M0–M5 code and M6 local acceptance tests are present as focused commits on stacked feature branches. Required real-OS acceptance remains open.

## Verified locally (Linux amd64, Go 1.26.10)

- Exact `make check`: format, go vet, pinned golangci-lint v2.12.0 (zero issues), unit tests, race tests and CGO-disabled binary build.
- `make integration`: foreground DNS over TCP/UDP and HTTP, plus 10 installations / 50 concurrent hostnames across two zones with backend identity and WordPress Host/path/query assertions.
- `go test -tags port80 -race -v ./test/integration`: localhost:80 remains responsive while Raymond serves dedicated loopback:80; wildcard :80 conflict detected without terminating the other listener.
- HTTP tests: large POST preservation, raw paths/query strings, original Host, cookies, redirects, forwarding-header spoofing, streaming, real WebSocket handshake/framed messages, raw upgrades, 404/502 and backend connection reuse.
- Configuration/router fuzzing: 210,211 normalization executions and 134,325 suffix-boundary executions in short fuzz runs; no failures. Regression tests cover Unicode case folding, canonical ports, duplicate routes, YAML typos and unsafe targets.
- Installer offline fixtures: successful/repeated install, checksum corruption and unsafe archives rejected without replacing the existing binary. Bash syntax checks pass.
- Ownership tests: preexisting resource refusal, atomic no-overwrite publication, modified-file cleanup refusal, journal retention, symlink refusal and lifecycle lock exclusion.
- govulncheck v1.9.0: no vulnerabilities found with current dependencies.
- actionlint v1.7.7: workflows validated (shellcheck/pyflakes integrations not installed).
- GoReleaser v2.16.0: configuration validates and local snapshot archives/checksums build for darwin/amd64, darwin/arm64, linux/amd64 and linux/arm64. No tag, remote push or release publication was performed.
- Standalone native executable: 50 concurrent local HTTP requests preserve Host and URL, followed by successful SIGTERM shutdown. A diagnostic run took 1.154 seconds with 11,024 KiB RSS after the requests. These measurements include the Python load driver/backend and do not define a performance SLA. The Go integration run under the race detector took about 70 ms for 50 DNS+HTTP requests; the workloads differ and are not comparable benchmarks.

## Required external acceptance (not run)

- Full install → native OS resolution → HTTP → stop → uninstall on real macOS and supported systemd Linux runners. This container has neither a systemd PID 1 nor resolvectl; no macOS host is attached. No resolver or service changes were made here.
- macOS alias creation/removal, launchd reboot recovery and root-daemon behavior.
- Linux networkd dummy-interface creation, exact resolved per-link scope and cleanup, resolved stub compatibility, and DynamicUser service hardening on real distributions.
- Crash/reboot lifecycle recovery and network-disconnected packet-level verification.
- Privileged smoke workload and unrelated-resource restoration: scripts and CI gates exist but have not run on real OS runners. Unsupported runner prerequisites must fail; they must not be converted into skips.
- CodeQL, protected branch checks, environment approvals, dependency/secret scanning settings, artifact provenance and download of a publicly released archive. Repository settings require maintainer configuration; files do not enable them automatically. Dependency-review Action availability was not confirmed, so no unverified workflow dependency was added.

Release is blocked until these required platform checks pass. Trigger reviewed disposable-runner integration through GitHub after configuring the `platform-validation` environment. Configure the `release` environment approval and protected tags before publishing. A local build or cross-compilation does not satisfy real-OS acceptance.
