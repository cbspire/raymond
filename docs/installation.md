# Installation and removal

Raymond has two installation steps: obtaining a binary as an ordinary user, then explicitly installing OS DNS/service integration as an administrator. Downloading the binary alone never changes DNS. The v0.1 candidate has no stable GitHub release yet. You can evaluate the downloadable candidate archives without Go, or use the [source-build walkthrough](../README.md#build-the-current-candidate) for development.

## Install a candidate without Go

1. Open PR #1 → Checks → **Privileged OS integration** → workflow run. Choose a run with passing macOS and Linux acceptance jobs.
2. Download its `raymond-candidate-*` artifact and unzip it into a directory you control. Artifacts are retained for 14 days and private repositories require GitHub authentication to download them.
3. The bundle contains `dist/` with four archives, `checksums.txt` and `metadata.json`, plus `scripts/install.sh`, this guide, README and an example YAML. Each native archive has one executable (`raymond`) plus README/LICENSE.
4. Read `dist/metadata.json` to obtain the exact candidate version, such as `0.1.0-rc.gabcdef0`. Inspect the included installer before running it.
5. From the unzipped bundle, install the matching native binary as your ordinary user:

```sh
# Replace this value with the exact "version" from dist/metadata.json.
RAYMOND_VERSION=v0.1.0-rc.gabcdef0 \
  RAYMOND_ARCHIVE_DIR="$PWD/dist" \
  bash scripts/install.sh
export PATH="$HOME/.local/bin:$PATH"
raymond version
```

This uses already-downloaded archives; it does not require Go, curl or network access. The installer chooses your OS/architecture, verifies SHA-256, validates archive members and executes the staged binary's `version` command before replacing a previous binary. For privileged setup below, substitute `"$HOME/.local/bin/raymond"` for `bin/raymond` in the bootstrap-copy command. Retain the example YAML as your own editable config.

The kernel and native OS services are the only runtime platform requirements. The shell installer uses standard archive/file/checksum tools to obtain the executable; those tools are not used to process DNS or proxy requests.

## Before OS setup

1. Review the [native acceptance evidence](validation.md). The macOS 15/arm64 and Ubuntu 24.04/amd64 lifecycle jobs pass; other OS versions/architectures may need separate validation. Use a disposable system for first-time evaluation.
2. Review your executable and YAML. Keep a separate editable source config; the installed root-owned copy is not the file to edit.
3. Run `bin/raymond config validate --file raymond.yaml` and make sure the configured local backend is running.
4. Read [macOS requirements](macos.md) or [Linux requirements](linux.md). Linux requires both systemd-resolved and systemd-networkd, plus the existing resolved stub symlink. Raymond does not convert unsupported resolver setups.
5. Ensure no unrelated file already occupies a Raymond resource path. The installer refuses conflicts rather than overwriting them. A wildcard `:80` listener conflicts with the dedicated address; a listener on `127.0.0.1:80` can coexist.

## Binary locations and permissions

The README uses `/var/lib/raymond-bootstrap/raymond` as the reviewed root-owned entry point for administrator commands. A user-owned downloaded binary must first be copied there with explicit `sudo install`; running the OS installer directly from a writable checkout or `~/.local/bin` is rejected. Root service files never point into your home directory.

| Resource | macOS | Linux |
| --- | --- | --- |
| Managed service binary (0755) | `/Library/PrivilegedHelperTools/dev.raymond` | `/usr/libexec/raymond/raymond` |
| Service config (0644) | `/etc/raymond/config.yaml` | `/etc/raymond/config.yaml` |
| Ownership journal (0600) | `/var/lib/raymond/state.json` | `/var/lib/raymond/state.json` |
| Service definition | `/Library/LaunchDaemons/dev.raymond.plist` | `/etc/systemd/system/raymond.service` |
| DNS integration | `/etc/resolver/<zone>` | `/etc/systemd/network/80-raymond.{link,netdev,network}` |

Fixed paths, root ownership and non-writable ancestors protect privileged startup. macOS's standard `/etc` and `/var` aliases into `/private` are recognized; arbitrary symlinks are refused. macOS's service helper avoids Homebrew-owned `/usr/local`. If a required path is untrusted, use a suitable supported system rather than changing unrelated directory ownership.

## Service setup and verification

Follow the exact bootstrap, install, doctor and start commands in the [README](../README.md#install-and-enable-os-integration). `install` creates resolver/service resources but does not start the daemon immediately. Run bind diagnostics after install and before start; an already-running Raymond occupies its own ports.

After start, test OS DNS rather than only querying the Raymond socket directly:

```sh
# macOS
dscacheutil -q host -a name site.install01.web.test

# Linux
getent ahostsv4 site.install01.web.test
```

Expect `127.0.0.42`, then make an HTTP request using the hostname:

```sh
curl --noproxy '*' http://site.install01.web.test/
```

For a proxy-only diagnostic that bypasses OS DNS:

```sh
curl --noproxy '*' --resolve site.install01.web.test:80:127.0.0.42 \
  http://site.install01.web.test/
```

A successful direct proxy request does not establish that OS split DNS works. Use `status` and native manager logs when startup fails. macOS logs go to `/var/log/raymond.log`; Linux logs are available with `journalctl -u raymond.service`.

## Foreground development without Linux OS setup

On Linux, the kernel normally makes the whole IPv4 loopback range available without adding aliases. For unprivileged foreground debugging, copy your YAML and change only:

```yaml
dns:
  zones: [test]
  listen: 127.0.0.43:1053
proxy:
  listen: 127.0.0.42:8080
```

Keep `version: 1` and at least the installation mapping you want to test. Validate the complete file, then run:

```sh
bin/raymond config validate --file raymond-dev.yaml
bin/raymond serve --config raymond-dev.yaml
```

In another terminal:

```sh
curl --noproxy '*' --resolve site.install01.web.test:8080:127.0.0.42 \
  http://site.install01.web.test:8080/
```

SIGINT or SIGTERM stops foreground serving. This does not install OS resolver routes, so use an explicit direct DNS query on port 1053 if diagnosing DNS. High ports are for foreground testing; installed integrations require 53 and 80.

On macOS high ports still require the dedicated loopback aliases. Do not assume changing port numbers makes those addresses available. Use reviewed OS installation on a disposable macOS machine; ordinary Go tests inject portable socket fixtures instead.

## Upgrades and config changes

An identical install is idempotent. Changing installed config or replacing its managed binary directly invalidates the ownership hashes and causes uninstall to refuse cleanup.

1. Keep the existing bootstrap binary and installed resources intact.
2. Run `sudo /var/lib/raymond-bootstrap/raymond uninstall`. It stops the service if needed.
3. Edit and validate your separate source YAML, or explicitly replace the root-owned bootstrap copy with the reviewed new binary.
4. Run install, doctor and start again using the source config.

This requires downtime; there is no hot reload. On macOS use `start` after `stop`; `restart` kickstarts an already-loaded launchd service.

## Uninstall and interrupted setup

```sh
sudo /var/lib/raymond-bootstrap/raymond uninstall
```

Only resources that match the recorded ownership hashes are removed. Preexisting resolver files and unrelated systemd drop-ins are preserved. Only recorded Raymond-created macOS aliases are removed. Your source config, user-downloaded binary and bootstrap executable remain, as can empty directories, a lock file and logs. Remove those user-created copies separately if no longer needed; they are not service resources.

A failed or interrupted install can leave a journal for explicit rollback. Retry uninstall with the same bootstrap binary. If a file was changed, restore its reviewed original installed content before retrying; do not delete the journal or force-remove unknown resources. An unsupported or ambiguous resolver state is an error, not an instruction to replace `resolv.conf`.

## Release binary installer

After a release exists, `scripts/install.sh` supports:

| Variable | Behavior |
| --- | --- |
| `RAYMOND_VERSION=v0.1.0` | Select an immutable stable or prerelease tag; online mode otherwise resolves latest stable once. |
| `RAYMOND_ARCHIVE_DIR=/absolute/path/to/dist` | Install already-downloaded archives and checksums without network access; requires a pinned version. |
| `RAYMOND_INSTALL_DIR=/absolute/path` | Override `~/.local/bin`; only the binary is installed there. |

The script requires Bash (including macOS Bash 3.2), tar, gzip, awk and standard file tools, plus `shasum` on macOS or `sha256sum` on Linux. Online downloads additionally require curl. It accepts no positional arguments. Downloads and redirects use HTTPS. Checksums, exact archive names and regular-file entries are checked before executing the staged binary's `version` command. Failed downloads/checksums/validation leave an existing binary unchanged. A directory or symlink at the destination is refused. Staging files are removed on failure and handled signals.

The SHA-256 manifest comes from the same GitHub release as the archive; this provides integrity but not independent provenance. Review signing/attestation before broader distribution. The installer never invokes sudo or changes service/DNS settings.
