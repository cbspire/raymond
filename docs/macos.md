# macOS

Raymond installs a root launchd daemon (`dev.raymond`), dedicated `lo0` aliases and one marked `/etc/resolver/<zone>` file per zone. Existing resolver files are never overwritten. Only aliases absent during installation are recorded for later removal. At service startup, missing recorded aliases are recreated after reboot. The daemon runs as root to bind ports 53/80 and restore aliases; this is a documented v0.1 privilege tradeoff. Future privilege isolation is required before broader deployment.

Use the explicit [installation walkthrough](installation.md) and [README bootstrap commands](../README.md#install-and-enable-os-integration). macOS uses launchd rather than systemd. The executable and its ancestor directories must be root-owned and not group/world writable. The managed daemon is `/Library/PrivilegedHelperTools/dev.raymond`, outside Homebrew-managed `/usr/local`; the reviewed bootstrap copy is `/opt/raymond-bootstrap/raymond`. macOS's standard `/etc` and `/var` aliases to `/private` are recognized; other symlinks are rejected.

Installation does not start the launchd daemon immediately. `sudo /opt/raymond-bootstrap/raymond start` bootstraps it; RunAtLoad enables reboot startup. Stop boots it out and restart kickstarts it. Uninstall removes only resources whose hashes match the root-owned journal and aliases created by Raymond. The system log `/var/log/raymond.log` is retained.

No real macOS runner is available in the development environment. Alias lifecycle, OS split DNS, reboot recovery and coexistence with localhost:80 require the privileged CI smoke test before release. Cross-compilation is insufficient evidence.
