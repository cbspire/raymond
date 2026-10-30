# Architecture

Strict versioned YAML is normalized before creating an immutable router. Routes are copied and sorted by suffix length, with apex and label-boundary matching. DNS answers only configured zones: matching A records point to the dedicated HTTP IPv4 address, unsupported types return NODATA, unknown names return NXDOMAIN and out-of-zone queries are refused. Both TCP and UDP sockets are acquired before serving.

One ReverseProxy selects the configured backend for each Host. It retains the original Host, raw path, query, method, cookies, redirects and streaming body. A shared transport reuses backend connections and supports upgrades without caching or compression. There is no total request timeout.

Foreground serving binds all three sockets before starting services and shuts down on SIGINT/SIGTERM. OS lifecycle installation is isolated from request handling, uses fixed native management commands and journals only Raymond-owned files. See platform docs for root permissions and validation limitations.
