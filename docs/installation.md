# Installation and removal

For automatic setup, run `curl -fsSL https://raw.githubusercontent.com/cbspire/raymond/main/install.sh | bash` as your normal user. The installer verifies the binary, creates user config, requests administrator access, configures DNS, starts the service and updates your shell profile. Use `raymond upgrade` later. The manual steps below remain available.

Raymond has two installation steps: obtaining a binary as an ordinary user, then explicitly installing OS DNS/service integration as an administrator. Downloading the binary alone never changes DNS. Use the [released binary walkthrough](../README.md#install) for `0.1.1`, download candidate archives for testing, or follow the [source-build walkthrough](../README.md#build-from-source) for development.

## 0.1.2 local management (feature branch)

Run `raymond setup` as the intended ordinary owner. One administrator authorization installs the protected executable, independent proxy/manager services, root-only ownership metadata, and the owner's private `~/.raymond/control.json`. A 0.1.1 installation is migrated through the same enrollment flow. Native macOS/Ubuntu acceptance for this implementation is tracked separately from the historical 0.1.1 results; do not treat this branch as release-ready until those checks pass.

After enrollment, config activation, service controls, diagnostics, log access and verified stable updates use the private broker without further sudo. Repeated `setup` with no arguments uses the enrolled config, including a custom path. Config, credential and CLI file access runs under the owner's UID; the broker never accepts commands, filesystem paths or download URLs from API callers. The CLI must remain at its recorded user-owned, owner-writable path. A different account or custom config path cannot silently take over an enrollment.

`raymond dashboard` opens an authorized browser without accounts/password setup. The single-use launch token is kept in a private local redirect file, not a command-line argument or HTTP query. Browser sessions persist through reloads and service/manager updates in the authorized tab. Use `raymond dashboard` to authorize a new tab; authentication uses an explicit session header, not cookies. Stopping the proxy closes the dashboard; `raymond start` uses the surviving Unix-socket manager. An app route shadowing `raymond.test` is diagnosed by the CLI and retains precedence.

Management state, sessions, serialized operations and rollback snapshots live under `/var/lib/raymond/management` (root-only). The socket is `/run/raymond-control/control.sock` on Linux and `/Library/PrivilegedHelperTools/raymond-control/control.sock` on macOS. Unix owner requests additionally verify the peer UID; Linux delivers the distinct browser-proxy credential through systemd `LoadCredential` to the restricted proxy. There is no public administration port.

Workers retain their operation lock across manager replacement, keep a protected executable available, and persist rollback evidence before modifying resources. Interrupted running work is reconciled rather than replayed. A failed activation restores the working service and leaves the owner's saved edits pending for Retry/Discard. Changed/unproven files block recovery rather than being overwritten. Do not delete operation/rollback records to bypass a conflict.

Explicit `raymond upgrade [--version 0.1.2]` resolves a stable immutable archive/checksum at the fixed repository, verifies/extracts it natively, checks the executable version, replaces protected and owner binaries, restarts management and confirms health/version. Serving otherwise remains offline. CLI updates never download or execute a shell installer. An uncompleted first enrollment may need `setup` again; an already completed enrollment does not request recurring passwords.

`sudo raymond uninstall` remains an explicit administrator removal action. It stops both native services and removes only proven owned resources, plus the owner's matching private management credential through a dropped-UID helper. The user's config and CLI are preserved. Root-only legacy install/start remains available without enrolling an ordinary owner.

The following low-level installation guide also documents the published 0.1.1 workflow and compatibility commands.

## Install a candidate without Go

1. Open PR #1 → Checks → **Privileged OS integration** → workflow run. Choose a run with passing macOS and Linux acceptance jobs.
2. Download its `raymond-candidate-*` artifact and unzip it into a directory you control. Artifacts are retained for 14 days and private repositories require GitHub authentication to download them.
3. The bundle contains `dist/` with four archives, `checksums.txt` and `metadata.json`, plus `scripts/install.sh`, this guide, README and an example YAML. Each native archive has one executable (`raymond`) plus README/LICENSE.
4. Read `dist/metadata.json` to obtain the exact candidate version, such as `0.1.1-rc.gabcdef0`. Inspect the included installer before running it.
5. From the unzipped bundle, install the matching native binary as your ordinary user:

```sh
# Replace this value with the exact "version" from dist/metadata.json.
RAYMOND_VERSION=0.1.1-rc.gabcdef0 \
  RAYMOND_BINARY_ONLY=1 \
  RAYMOND_ARCHIVE_DIR="$PWD/dist" \
  bash scripts/install.sh
export PATH="$HOME/.local/bin:$PATH"
raymond version
```

This uses already-downloaded archives; it does not require Go, curl or network access. The installer chooses your OS/architecture, verifies SHA-256, validates archive members and executes the staged binary's `version` command before replacing a previous binary. For privileged setup below, substitute `"$HOME/.local/bin/raymond"` for `./bin/raymond` in the service setup commands. Run `raymond install` to initialize your editable user configuration.

The kernel and native OS services are the only runtime platform requirements. The shell installer uses standard archive/file/checksum tools to obtain the executable; those tools are not used to process DNS or proxy requests.

## Before OS setup

1. Review the [native acceptance evidence](validation.md). The macOS 15/arm64 and Ubuntu 24.04/amd64 lifecycle jobs pass; other OS versions/architectures may need separate validation. Use a disposable system for first-time evaluation.
2. Run `bin/raymond install` as your ordinary user. This creates `~/.raymond/raymond.yaml` automatically, with a dedicated `raymond.test` zone and no active mappings. Edit that file to add your local backends. Existing config files are preserved, and sudo installation also initializes the invoking user's file if missing. An explicit `install --config path` uses that file instead.
3. Run `bin/raymond config validate` and make sure the configured local backend is running. Review the generated YAML before privileged installation; the installed root-owned copy is not the file to edit.
4. Read [macOS requirements](macos.md) or [Linux requirements](linux.md). Linux requires both systemd-resolved and systemd-networkd, plus the existing resolved stub symlink. Raymond does not convert unsupported resolver setups.
5. Ensure no unrelated file already occupies a Raymond resource path. The installer refuses conflicts rather than overwriting them. A wildcard `:80` listener is rejected by default; a listener on `127.0.0.1:80` can coexist. macOS users can explicitly enable `proxy.allow_wildcard_coexistence` for the HTTP proxy, subject to the exact loopback bind succeeding; see [Docker Desktop troubleshooting](troubleshooting.md#docker-desktop-on-macos).

## Binary locations and permissions

Run the reviewed executable directly with `sudo ./bin/raymond install`, or use `sudo "$HOME/.local/bin/raymond" install` for a downloaded binary. The installer takes a bounded snapshot of that executable and atomically publishes a separate root-owned service copy. Source files and ancestors may belong to root or the invoking sudo user, but other owners, symlinks and group/world-writable paths are rejected. No manual copy or ownership change of your checkout is required. Root service files never point into your home directory.

| Resource | macOS | Linux |
| --- | --- | --- |
| Editable user config (0600; parent 0700) | `~/.raymond/raymond.yaml` | `~/.raymond/raymond.yaml` |
| Managed service binary (0755) | `/Library/PrivilegedHelperTools/dev.raymond` | `/usr/libexec/raymond/raymond` |
| Service config (0644) | `/etc/raymond/config.yaml` | `/etc/raymond/config.yaml` |
| Ownership journal (0600) | `/var/lib/raymond/state.json` | `/var/lib/raymond/state.json` |
| Service definition | `/Library/LaunchDaemons/dev.raymond.plist` | `/etc/systemd/system/raymond.service` |
| DNS integration | `/etc/resolver/<zone>` | `/etc/systemd/network/80-raymond.{link,netdev,network}` |

Fixed paths, root ownership and non-writable ancestors protect privileged startup. macOS's standard `/etc` and `/var` aliases into `/private` are recognized; arbitrary symlinks are refused. macOS's service helper avoids Homebrew-owned `/usr/local`. If a required path is untrusted, use a suitable supported system rather than changing unrelated directory ownership.

## Service setup and verification

Follow the exact install, doctor and start commands in the [README](../README.md#install-and-enable-os-integration). With sudo, `install` uses the invoking user's configuration and installs a validated root-owned snapshot plus resolver/service resources, but does not start the daemon immediately. Without sudo, `install` only prepares the user config. The configured macOS wildcard policy is checked before creating resolver/service resources or loopback aliases; actual bind checks still run after alias setup, including with HTTP coexistence enabled. Run full bind diagnostics after privileged install and before start; an already-running Raymond occupies its own ports.

After start, test OS DNS rather than only querying the Raymond socket directly:

```sh
# macOS
dscacheutil -q host -a name api.app.raymond.test

# Linux
getent ahostsv4 api.app.raymond.test
```

Expect `127.0.0.42`, then make an HTTP request using the hostname:

```sh
curl --noproxy '*' http://api.app.raymond.test/
```

For a proxy-only diagnostic that bypasses OS DNS:

```sh
curl --noproxy '*' --resolve api.app.raymond.test:80:127.0.0.42 \
  http://api.app.raymond.test/
```

A successful direct proxy request does not establish that OS split DNS works. Use `raymond status` to inspect service state and `raymond logs` to follow live requests. `logs --lines N` changes the recent history and `logs --no-follow` exits after printing it. macOS uses `/var/log/raymond.log`; Linux uses the journal for `raymond.service` only. Request logs require the managed service binary to include the current logging implementation; run `raymond setup` after a local rebuild to update that copy.

## Foreground development without Linux OS setup

On Linux, the kernel normally makes the whole IPv4 loopback range available without adding aliases. For unprivileged foreground debugging, copy the example YAML to `raymond-dev.yaml` and change its listener settings:

```yaml
dns:
  zones: [test]
  listen: 127.0.0.43:1053
proxy:
  listen: 127.0.0.42:8080
```

Keep `version: 1` and at least the route mapping you want to test. Validate the complete file, then run:

```sh
bin/raymond config validate --file raymond-dev.yaml
bin/raymond serve --config raymond-dev.yaml
```

In another terminal:

```sh
curl --noproxy '*' --resolve tenant.app.test:8080:127.0.0.42 \
  http://tenant.app.test:8080/
```

SIGINT or SIGTERM stops foreground serving. This does not install OS resolver routes, so use an explicit direct DNS query on port 1053 if diagnosing DNS. High ports are for foreground testing; installed integrations require 53 and 80.

On macOS high ports still require the dedicated loopback aliases. Do not assume changing port numbers makes those addresses available. Use reviewed OS installation on a disposable macOS machine; ordinary Go tests inject portable socket fixtures instead.

## Upgrades and config changes

For enrolled 0.1.2 management, use `raymond upgrade` (or `raymond update`) for the latest newer compatible stable release, or `raymond upgrade --version 0.1.2` to select one. Existing user configuration is preserved. The installer restores the previous user binary if setup fails. Native setup verifies owned resources before stopping the service, saves its protected binary/config and restores them if replacement fails. Recovery failures retain the backup directory named in the error for explicit repair.

After editing your YAML or rebuilding locally, run `raymond setup` to apply it. An enrolled setup briefly restarts the service without administrator prompts; initial enrollment requests authorization once. There is no hot reload. Modified protected files still cause a refusal; resolve the ownership diagnostic before retrying. Manual uninstall/install/start remains supported.

## Uninstall and interrupted setup

```sh
sudo ./bin/raymond uninstall
```

Only resources that match the recorded ownership hashes are removed. Preexisting resolver files and unrelated systemd drop-ins are preserved. Only recorded Raymond-created macOS aliases are removed. Your `~/.raymond/raymond.yaml` (or explicit source config), user-downloaded binary remain, as can empty directories, a lock file and logs. Remove those user-created copies separately if no longer needed; they are not service resources.

A failed or interrupted install can leave a journal for explicit rollback. Retry uninstall with the same reviewed local binary. If a file was changed, restore its reviewed original installed content before retrying; do not delete the journal or force-remove unknown resources. An unsupported or ambiguous resolver state is an error, not an instruction to replace `resolv.conf`.

## Release binary installer

After a release exists, `scripts/install.sh` supports:

| Variable | Behavior |
| --- | --- |
| `RAYMOND_VERSION=0.1.1` | Select an immutable stable or prerelease tag; online mode otherwise resolves latest stable once. |
| `RAYMOND_ARCHIVE_DIR=/absolute/path/to/dist` | Install already-downloaded archives and checksums without network access; requires a pinned version. |
| `RAYMOND_INSTALL_DIR=/absolute/path` | Override `~/.local/bin`; only the binary is installed there. |

The script requires Bash (including macOS Bash 3.2), tar, gzip, awk and standard file tools, plus `shasum` on macOS or `sha256sum` on Linux. Online downloads additionally require curl. It accepts no positional arguments. Downloads and redirects use HTTPS. Checksums, exact archive names and regular-file entries are checked before executing the staged binary's `version` command. Failed downloads/checksums/validation leave an existing binary unchanged. A directory or symlink at the destination is refused. Staging files are removed on failure and handled signals.

The SHA-256 manifest comes from the same GitHub release as the archive; this provides integrity but not independent provenance. Review signing/attestation before broader distribution. Default setup requests administrator access for service/DNS changes. Set `RAYMOND_BINARY_ONLY=1` to skip those steps and shell-profile edits.
