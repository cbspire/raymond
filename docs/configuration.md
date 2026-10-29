# Configuration

See `examples/raymond.yaml`. The only supported schema is `version: 1`. Unknown keys, duplicate YAML keys, multiple documents, invalid labels and duplicate normalized routes are errors. Defaults are DNS `127.0.0.43:53` and HTTP `127.0.0.42:80`. Foreground tests may use high ports. OS installation requires ports 53 and 80.

Zones and domains are lowercase ASCII DNS names after normalization; one final dot is ignored. Unicode is rejected: convert IDNA names to explicit punycode before configuration. `.local` zones are disabled. Hosts match an installation's apex or any descendant, with longest suffix winning at label boundaries. All installation domains must belong to a configured zone. Targets are IPv4 loopback literals with numeric ports. External targets and IPv6 are deliberately unsupported in v0.1.

Edit the file and validate before a controlled restart. There is no hot reload or domain CRUD API. Keep privileged service configuration at `/etc/raymond/config.yaml` root-owned and not writable by other users.
