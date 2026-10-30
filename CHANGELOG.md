# Changelog

## Unreleased

- Establish versioned configuration, deterministic routing and a native Go CLI.
- Add strict normalized routing, authoritative local DNS and HTTP proxying with WebSocket/streaming support.
- Add journaled ownership checks, platform service templates and explicit lifecycle commands.
- Add checksum-verifying binary installer, four-target packaging and gated CI/release workflows.
- Complete native macOS/Ubuntu lifecycle, crash recovery, offline runtime and real-package acceptance without Go on runtime PATH.
- Fix BSD wildcard-listener detection, protected executable paths, Linux scoped DNS activation and interface ownership.
- Publish downloadable candidate workflow artifacts and support pinned offline archive installation.
- Initialize and preserve `~/.raymond/raymond.yaml` automatically, including correct user ownership under sudo.
- Install reviewed user-owned executables directly into the protected service path without a manual bootstrap copy.
- Add the embedded dashboard landing page at `raymond.test`, with user-defined routes taking precedence.
- Add live service logs and HTTP access records that exclude query strings, credentials and payloads.
- Silence expected launchd service probes and acknowledge successful lifecycle commands.
- Add explicit macOS HTTP wildcard coexistence for Docker Desktop while retaining exact socket and DNS checks.
