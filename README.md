# Neo SCP

A cross-platform desktop app for copying files over SCP, built with Go and [Fyne](https://fyne.io).

NSCP shows two panes side by side. Each pane is a directory tree on your computer or on an SSH
server. Drag a file or folder from one pane to the other to copy it.

## Features

- **Two panes, any combination:**
  - local ↔ local
  - local ↔ server
  - server ↔ server (relayed through your machine, so the servers never need to reach each other)
- **Host picker on each pane:**
  - Local
  - recent servers
  - hosts from `~/.ssh/config` (`Include`, `ProxyJump` and `IdentityFile` are supported)
  - saved servers
  - a new connection
- **Authentication:** ssh-agent, key file (passphrase-protected keys too), or password.
  Secrets can be saved in your OS keychain, never in plain files.
- **ProxyJump / bastion hosts.**
- **Host key verification:** uses your `known_hosts`, asks before trusting a new host, and
  refuses keys that have changed.
- **Drag and drop** between panes, or from Finder / Explorer onto a pane.
- **Asks before overwriting:** Overwrite, Skip, Overwrite all or Cancel.
- **Transfer queue** with progress and cancel.
- **New folder, rename and delete,** on both local and remote panes.

SCP is the only transfer protocol. Remote directories are listed over the SSH shell, so a server
needs `scp` plus GNU `find` and `stat`. Linux distributions have these as standard.

## Building

Requirements:
- Go 1.26+
- A C compiler and the [Fyne prerequisites](https://docs.fyne.io/started/) for your OS

```bash
make run     # start the app
make build   # build bin/nscp
make test    # unit tests (race detector on)
make test-integration   # end-to-end tests against real OpenSSH servers in Docker
```

## License

MIT
