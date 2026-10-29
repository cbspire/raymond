# Raymond

Elegant. Resourceful. Always in control.

Raymond is a single native Go binary for local wildcard DNS and HTTP reverse proxying on macOS and Linux. v0.1 targets HTTP only; HTTPS and certificate management are deferred to v0.2. There is no dashboard, remote control API, caching, compression or external DNS forwarding.

`site.install01.web.test` and arbitrarily deep subdomains route to one installation's backend while preserving the original Host, path and query. More-specific configured suffixes win. Unknown hosts are rejected. See [example configuration](examples/raymond.yaml).

The dedicated HTTP address defaults to `127.0.0.42:80`; DNS uses `127.0.0.43:53`. Raymond leaves `127.0.0.1:80` available. An existing wildcard listener on `0.0.0.0:80` conflicts and must be addressed by the operator. Targets are IPv4 loopback IP literals. ASCII labels and explicit punycode are supported; `.local` is rejected because of mDNS.

Build with Go 1.24 or newer: `make check`. Validate configuration with `bin/raymond config validate --file examples/raymond.yaml`. Native targets are macOS amd64/arm64 and Linux amd64/arm64; cross-compilation alone does not validate privileged OS integration.

Architecture: strict YAML configuration → immutable longest-suffix router → authoritative DNS and a single Go HTTP reverse proxy. Runtime requests require only local backends; no external services or language runtimes are required.

Development follows M0–M6 in [the implementation plan](RAYMOND_CODEX_PLAN.md). Each milestone must remain buildable. OS integration and release publication require real platform validation before v0.1 can be declared complete.
