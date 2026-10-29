# v0.1 acceptance evidence

Raymond is a tested release candidate. Stable `v0.1.0` publication remains a separate review/tagging action; no stable release has been published by this task. All product artifacts contain one native executable plus README/LICENSE. The DNS server and HTTP proxy are built into that executable.

## Native M4–M6 acceptance

[Passing native workflow run 40972916814](https://github.com/cbspire/raymond/actions/runs/40972916814) establishes the following on actual disposable OS runners:

| Capability | macOS 15 / arm64 | Ubuntu 24.04 / amd64 |
| --- | --- | --- |
| Real GoReleaser archive installed as an ordinary user | Passed | Passed |
| Go absent from runtime PATH; product built CGO_ENABLED=0 | Passed | Passed |
| No non-system runtime libraries / static Linux ELF | Passed | Passed |
| Native OS DNS and 10 installations / 50 concurrent hostnames | Passed | Passed |
| Exact backend, original Host, path, query, cookie and redirect behavior | Passed | Passed |
| localhost:80 coexistence and wildcard-listener diagnostic | Passed | Passed |
| Kernel inspection of exactly the dedicated loopback listeners | Passed | Passed |
| Actual daemon SIGKILL followed by OS service-manager recovery | Passed | Passed |
| Controlled restart and start after deleting volatile alias/link state | Passed | Passed |
| Existing-file conflicts, modified-file refusal and unrelated-file preservation | Passed | Passed |
| Owned files, aliases/interface and journal removed on uninstall | Passed | Passed |
| External networking kernel-restricted during DNS/HTTP runtime | Seatbelt policy passed | Isolated network namespace passed |

The native helper uses the platform's libc resolver, not a direct DNS query substituted for OS integration. If a disposable Ubuntu image begins with a foreign `resolv.conf`, the test first verifies that Raymond refuses to change it, then prepares the supported stub symlink as an explicit test fixture and restores the original file/symlink, mode and ownership at cleanup. The production installer never rewires `resolv.conf`. It is a compiled CI-only test binary; it is not shipped or needed by Raymond. The runtime PATH is an explicit whitelist of installer-native tools with no Go, Python, Node or external DNS/proxy program. Linux additionally verifies the daemon is non-root with only CAP_NET_BIND_SERVICE.

The cold-start test reconstructs volatile state after stopping the service and removing owned aliases/interface. It does not claim an actual machine power-cycle. WordPress-compatible protocol behavior is exercised through representative HTTP backends; WordPress and its database are not bundled with Raymond.

## Diagnostic measurements

The referenced run measured 50 native-OS-resolved concurrent requests at about 23.7 ms on macOS and 39.8 ms on Linux. Daemon RSS afterward was 26,480 KiB and 11,844 KiB respectively. These are one-run diagnostics, not a benchmark contract or throughput SLA.

## Other verification

- `make check`: format, vet, golangci-lint v2.12.0, unit tests, race detector and static binary build.
- `make integration`: ordinary high-port DNS over TCP/UDP and HTTP tests. macOS fixtures use existing localhost sockets; privileged acceptance separately validates the real dedicated aliases.
- HTTP tests cover large bodies, raw URL preservation, forwarding-header spoofing, streaming, real WebSocket frames, connection reuse and 404/502 behavior.
- Short fuzz runs completed 210,211 normalization and 134,325 suffix-boundary executions with no failures.
- Installer fixtures cover OS/architecture selection, pinned/latest versions, failed downloads, missing/ambiguous/corrupted checksums, non-regular archive members, failed executable startup, directory/symlink destinations, and offline archive mode.
- Ownership tests cover atomic publication, no-overwrite behavior, safe permissions, symlink refusal, changed-file refusal and lifecycle lock exclusion.
- `govulncheck` passes using the patched Go 1.26.10 minimum. CodeQL is conditional on repository support: private repositories opt in using `RAYMOND_CODEQL_ENABLED=true` after enabling code scanning. Otherwise its job is explicitly skipped and the summary explains the missing coverage.
- Pinned actionlint and shell syntax checks pass. Four GoReleaser archives and SHA-256 manifests are built for darwin/linux × amd64/arm64. Native acceptance covers darwin/arm64 and linux/amd64; the other two targets are cross-built.

## Distribution and remaining release actions

The **Privileged OS integration** workflow uploads candidate archives, checksums, metadata, installer and configuration/docs. Its immutable artifact names include the tested Git SHA; evidence logs are uploaded per OS. Artifacts expire after 14 days. Candidate versions are `0.1.0-rc.g<commit>`, while final tagged releases use their actual version.

The owner still needs to review/merge the PR and authorize the release tag/publication. Repository branch/tag protections, environment approval rules, dependency/secret scanning and optional artifact signing/attestation are settings or release-policy actions, not automatically enabled by source files. The release workflow requires CI, security and native OS acceptance before its publication job. GitHub's `release` environment should be configured with the owner's approval policy before publishing.

Linux integration currently requires already-active systemd-networkd/systemd-resolved and the resolved stub symlink. Other Linux resolver arrangements fail closed. macOS's installed daemon runs as root for privileged ports and alias reconstruction; privilege isolation remains future hardening. No HTTPS, certificates or trust-store changes are included in v0.1.
