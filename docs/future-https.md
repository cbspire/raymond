# Deferred to v0.2

v0.1 does not listen for TLS, create a CA, alter trust stores or enable redirects/HSTS. v0.2 can add explicit TLS opt-in, per-hostname SNI certificates, secure key storage, certificate lifecycle tests and reversible trust changes after a separate design review. Deep nested names cannot be covered by a single wildcard certificate.
