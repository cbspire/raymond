# Raymond

Elegant. Resourceful. Always in control.

Raymond is a single native Go binary for local wildcard DNS and HTTP reverse proxying on macOS and Linux. v0.1 targets HTTP only; HTTPS and certificate management are deferred to v0.2. There is no dashboard, remote control API, caching, compression or external DNS forwarding.

`site.install01.web.test` and arbitrarily deep subdomains route to one installation's backend while preserving the original Host, path and query. More-specific configured suffixes win. Unknown hosts are rejected. See [example configuration](examples/raymond.yaml).

The dedicated HTTP address defaults to `127.0.0.42:80`; DNS uses `127.0.0.43:53`. Raymond leaves `127.0.0.1:80` available. An existing wildcard listener on `0.0.0.0:80` conflicts and must be addressed by the operator. Targets are IPv4 loopback IP literals. ASCII labels and explicit punycode are supported; `.local` is rejected because of mDNS.

Build with Go 1.26 or newer: `make check`. Validate configuration with `bin/raymond config validate --file examples/raymond.yaml`. Native targets are macOS amd64/arm64 and Linux amd64/arm64; cross-compilation alone does not validate privileged OS integration.

Architecture: strict YAML configuration → immutable longest-suffix router → authoritative DNS and a single Go HTTP reverse proxy. Runtime requests require only local backends; no external services or language runtimes are required.

Development follows M0–M6 in [the implementation plan](RAYMOND_CODEX_PLAN.md). Each milestone must remain buildable. OS integration and release publication require real platform validation before v0.1 can be declared complete.

## Run and validate

```sh
make build
bin/raymond serve --config examples/raymond.yaml
```

Default ports need explicit administrator privileges; use high ports in a copied YAML file for development. On macOS the dedicated loopback aliases must first be established through reviewed OS installation. See [macOS](docs/macos.md), [Linux](docs/linux.md), [configuration](docs/configuration.md), [security](docs/security.md), and [troubleshooting](docs/troubleshooting.md).

The installer at `scripts/install.sh` installs only a verified release binary to `~/.local/bin`; it never elevates privileges or changes resolver settings. No v0.1.0 release has been published by this implementation task. After a release exists, prefer downloading and inspecting the installer before running it with `RAYMOND_VERSION=v0.1.0 bash install.sh`. Checksums protect integrity but are not independent artifact provenance.

Required development checks are `make check`, `make integration`, `bash scripts/test-installer.sh`, vulnerability analysis and the four-target build matrix. Privileged OS smoke tests require a reviewed disposable runner (`RAYMOND_PRIVILEGED_SMOKE=1 scripts/smoke.sh`). A release must wait for both real OS jobs, security checks and human approval. Repository protection and environment approval settings must be enabled by a maintainer.

See [validation evidence and remaining release gates](docs/validation.md). The implementation candidate is buildable and locally tested, but real macOS/systemd Linux installation and cleanup are still required before v0.1.0 release readiness.
