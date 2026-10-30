# Architecture

Strict versioned YAML is normalized before creating an immutable router. Routes are copied and sorted by suffix length, with apex and label-boundary matching. DNS answers only configured zones: matching A records point to the dedicated HTTP IPv4 address, unsupported types return NODATA, unknown names return NXDOMAIN and out-of-zone queries are refused. Both TCP and UDP sockets are acquired before serving.

One ReverseProxy selects the configured backend for each Host. It retains the original Host, raw path, query, method, cookies, redirects and streaming body. A shared transport reuses backend connections and supports upgrades without caching or compression. There is no total request timeout.

Foreground serving binds all three sockets before starting services and shuts down on SIGINT/SIGTERM. OS lifecycle installation is isolated from request handling, uses fixed native management commands and journals only Raymond-owned files. See platform docs for root permissions and validation limitations.

The 0.1.2 management plane lives in `internal/control` and `internal/manager`. A root daemon on a private Unix socket serves fixed operations to the enrolled owner and authenticated browser sessions forwarded by the restricted proxy. The manager survives stopping the proxy; no additional public listener exists. User config, credential and CLI I/O runs in a clean-environment subprocess after dropping UID/GID. The React dashboard and CLI share the protocol.

Each serialized operation is durable before a separate worker inherits its flock. The worker survives manager restart, keeps the protected executable available during replacement, records before-images under a separate rollback directory, verifies health/version before commit, and preserves evidence across failure. Atomic journal snapshots remain readable while the worker holds its lock. Recovery never blindly replays running requests. Saved config and confirmed active config remain distinct until activation commits. Service/manager shutdown cancels active log readers while leaving durable workers independent.

Native acceptance on disposable supported runners is required before merging/releasing this branch. See [the handoff](dashboard-0.1.2-handoff.md) and [validation](validation.md).
