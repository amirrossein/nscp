# NSCP — Project Reference

> Single source of truth for all AI coding agents and human contributors.
> Agent entry-point files (CLAUDE.md, AGENTS.md, .cursorrules, etc.) all point here.

---

## What This App Does

**NSCP** ("Neo SCP") is a cross-platform desktop application (macOS / Windows / Linux) built
with Go and the Fyne UI toolkit. It is a two-pane, commander-style file manager for SCP:

- The window has a **left and a right pane**. Each one shows a directory **tree** and has a
  **host picker** at the top.
- **Both panes start on the local machine** (the user's home directory).
- From the host picker, a pane can switch to:
  - **Local**
  - a **recent server** (the last 8 used)
  - a host from **`~/.ssh/config`**
  - a **saved server**
  - a **new connection**: host, port, user, then ssh-agent / key file / password, plus an
    optional ProxyJump
- **Drag a file or folder from one pane onto the other** to copy it. You can also drop files
  from Finder/Explorer onto a pane. Every copy direction works:
  - local → local
  - local → remote
  - remote → local
  - remote → remote, relayed through this machine
- If the destination already exists, NSCP asks: Overwrite / Skip / Overwrite all / Cancel.
- Transfers queue in a panel at the bottom, which shows progress and lets you cancel.
- File operations work in both panes: new folder, rename, delete.

---

## Hard Constraints — Never Violate These

| Rule | Reason |
|------|--------|
| Use **SCP only** for file transfers (no SFTP, no rsync) | locked product decision |
| Use **SSH remote shell commands** for listing and file ops (no SFTP) | SCP cannot list dirs |
| Remote → remote copies are **relayed through the client** (SCP download piped into SCP upload) | servers need no access or credentials for each other |
| Store secrets in the **OS keychain only** — never in JSON files | security |
| Remote paths use `path` (POSIX) not `filepath` | remote is always Linux |
| Local paths use `filepath` | host OS may be Windows |
| Every path in a remote shell command goes through `vfs.ShellQuote` | shell injection |
| `profile.Store` must never write passwords or passphrases to disk | security |
| All public methods on `transfer.Job` must be goroutine-safe | concurrent UI + engine |
| UI callbacks that touch widgets must run on the main (Fyne) goroutine — use `runOnMain` | Fyne requirement |
| A host key mismatch is a hard error and is never offered as a prompt | MITM protection |

---

## Technology Stack

| Concern | Package |
|---------|---------|
| UI toolkit | `fyne.io/fyne/v2` |
| SSH protocol | `golang.org/x/crypto/ssh` + `knownhosts` + `agent` |
| SCP transfers | `github.com/povsister/scp` |
| OS keychain | `github.com/zalando/go-keyring` |
| Profile IDs | `github.com/google/uuid` |

Go module: `github.com/amirrossein/nscp`

Remote servers need `scp`, GNU `find` (for `-printf`), and GNU `stat` (for `-L --printf`).
That is standard on Linux distributions; on Alpine, install `findutils` and `coreutils`.

---

## Directory Structure

```
nscp/
├── cmd/nscp/main.go          ← entry point: builds stores, dialer and manager, then opens the window
├── internal/
│   ├── profile/              ← Profile model, JSON Store (atomic writes), Recent()
│   ├── credential/           ← Store interface, KeyringStore (OS keychain), MemStore (tests)
│   ├── sshconfig/            ← own ~/.ssh/config parser: Aliases(), Resolve(), Include, ProxyJump
│   ├── sshclient/
│   │   ├── client.go         ← Dialer.Connect, Client (Run, keepalive), hop resolution
│   │   ├── auth.go           ← Prompter interface, auth chain (agent, keys, password, kbd-interactive)
│   │   ├── knownhosts.go     ← KnownHosts, ErrHostKeyMismatch, HostKeyAlgorithms()
│   │   └── manager.go        ← Manager: ref-counted connections shared between panes and jobs
│   ├── sshtest/              ← in-process SSH server for tests (runs commands with /bin/sh)
│   ├── vfs/
│   │   ├── fs.go             ← FS interface, Entry, WalkItem
│   │   ├── local.go          ← LocalFS (os + filepath)
│   │   ├── remote.go         ← RemoteFS (shell commands over SSH + path)
│   │   └── shell.go          ← ShellQuote
│   ├── transfer/
│   │   ├── job.go            ← Job state machine + progress counters
│   │   ├── engine.go         ← Run(job): plan, conflict handling, per-file SCP in all four directions
│   │   └── queue.go          ← Queue: one worker goroutine, jobs run in order
│   └── ui/
│       ├── window.go         ← MainWindow, startTransfers, OS drop, job completion
│       ├── pane.go           ← Pane: host button, toolbar, path entry, lazy tree state
│       ├── tree.go           ← fileRow widget (tap, double tap, right click, drag)
│       ├── dnd.go            ← dragCoordinator, hit-testing of panes and rows
│       ├── hostpicker.go     ← host menu, connect()
│       ├── connectdialog.go  ← new/edit connection form
│       ├── prompts.go        ← Prompter implementation (password, passphrase, host key dialogs)
│       ├── conflict.go       ← Overwrite / Skip / Overwrite all / Cancel
│       ├── transfers.go      ← transfer queue panel
│       ├── fileops.go        ← context menu, new folder, rename, delete
│       ├── theme.go          ← nscpTheme: always-dark macOS-style palette, sizes, chevron icons
│       ├── widgets.go        ← card, pill, thinBar, hostChip, fillFirst/fixedHeight layouts
│       ├── chrome.go         ← pane header: host chip, tools, breadcrumbs, column header, footer
│       ├── icons.go          ← file-type icons, short timestamps
│       └── format.go         ← humanBytes, runOnMain
├── tests/integration/        ← Docker-backed tests (build tag: integration) + sshd Dockerfile
├── FyneApp.toml
├── Makefile
└── PROJECT.md                ← this file
```

---

## Package Responsibilities & Boundaries

### `internal/profile`
- `Store` persists `profiles.json` under `os.UserConfigDir()/nscp/`. It writes a temp file and
  then calls `os.Rename`, with mode 0600.
- A profile with `SSHConfigAlias` set takes host, user, port, identity files and ProxyJump from
  `~/.ssh/config` at connect time. The profile itself only stores auth overrides and usage data.
- `Recent(profiles, n)` returns the profiles with a non-zero `LastUsedAt`, newest first.
- **Never** calls the keyring or SSH.

### `internal/credential`
- Keychain service is `nscp:<profileID>`; the account is `password` or `passphrase`.
- A missing entry returns `("", nil)`.

### `internal/sshconfig`
- A small parser of our own. It is used instead of a library so that `Include ~/...` works and
  hosts from included files can be listed.
- Like ssh, the first value obtained wins. `IdentityFile` values accumulate.
- `Match` blocks are skipped, except `Match all`.

### `internal/sshclient`
- `Dialer.Connect(ctx, profile)` resolves the target and each ProxyJump hop. It dials the hops in
  order; every hop gets its own auth and its own known-hosts check.
- **Auth order:**
  1. ssh-agent (if enabled)
  2. key files (the passphrase comes from the keychain, otherwise the user is prompted)
  3. password and keyboard-interactive (the keychain password is tried first, then a prompt)
- A secret is saved to the keychain only if the user ticked *Remember* **and** authentication
  succeeded.
- **Known hosts:**
  - The checks read `~/.ssh/known_hosts` and NSCP's own `os.UserConfigDir()/nscp/known_hosts`.
  - Keys the user accepts are written only to NSCP's file.
  - When a key is already known for a host, `HostKeyAlgorithms()` limits the handshake to that
    key's algorithm.
- **Timeouts:** the TCP dial times out after 15 s. The handshake has no timer, because it may be
  waiting on a user prompt; only cancelling the context aborts it.
- **Connection sharing:** `Manager` reference-counts clients by profile ID, so two panes on the
  same server share one connection. Queued jobs `Retain` the clients they use.

### `internal/vfs`
- **Remote listing:**
  `find DIR -mindepth 1 -maxdepth 1 -printf '%f\0%y\0%Y\0%s\0%m\0%T@\0'`.
  Output is NUL-separated and parsed in groups of 6 fields, so any file name works. `%Y` reports
  the type of a symlink's target.
- **Remote walk (for transfers):** `find ROOT/ -mindepth 1 -printf '%P\0%y\0%Y\0%s\0%m\0'`.
  - Symlinked files are copied as content, with their size from `stat -L`.
  - Symlinked directories are skipped and reported as warnings. Local walks follow the same rule.
- `Stat` exits with status 44 when the path does not exist. That is mapped to `fs.ErrNotExist`.
- Removing a filesystem root (`/`, `C:\`) is refused.

### `internal/transfer`
- **States:** Queued → Running → Done | Failed | Cancelled. Invalid transitions do nothing.
- **Engine steps:**
  - Check the conflict, and honour `Job.Overwrite`. A file↔folder type change deletes the
    existing item first.
  - Walk the source.
  - Create the directories; on a remote destination they are batched into `mkdir -p` calls.
  - Copy file by file, with a fresh `scp.Client` per file.
- **SCP path quoting:** povsister/scp puts paths inside `'…'` without escaping them. So
  `ClientOption.RemoteBinary` is set to `cd <ShellQuote(dir)> && scp`, and the library is only
  ever given a bare file name.
- **remote → remote:** an `io.Pipe` connects `CopyFromRemote` to `CopyToRemote`.
- A partially copied file is deleted when a copy fails or is cancelled.
- Copying a folder into itself, or onto itself, is rejected.

### `internal/ui`
- **Visual design** (modelled on modern macOS dark apps):
  - The app always uses `nscpTheme`, which is dark only: a near-black window, raised rounded
    cards (`newCard`), and one blue accent (#0A84FF) for selection, folders and progress.
  - Secondary text uses `newCaption`.
  - Toolbar buttons are flat (`newFlatButton`). Only primary actions use a filled button.
  - Colours come from the palette vars in `theme.go`; don't hard-code colours elsewhere.
  - Theme icons must not be resolved in package-level vars, because the Fyne app doesn't exist
    yet at init time.
- Custom rows and composites must be real widgets (`BaseWidget` plus a renderer) or
  `*fyne.Container`s; Fyne's painter does not walk into other wrapper types.
- All widget mutations happen on the main goroutine. Background work calls `runOnMain(func(){…})`,
  which is `fyne.Do` in the app and a test-controlled queue in tests.
- The tree keeps `Root == ""` (Fyne draws a non-empty root as a row) and maps `""` to
  `Pane.root`. Children load lazily: a `Loading…` placeholder shows until the listing arrives.
- Each `Pane.gen` counter discards async results from a previous location or connection.
- **Drag and drop:** Fyne has no API for dragging between widgets.
  - `fileRow` implements `Draggable` and forwards events to `dragCoordinator`.
  - The coordinator hit-tests panes and rows with `AbsolutePositionForObject`.
  - **Drop location:** a folder row means that folder; a file row means its parent folder;
    anywhere else in the pane means the pane's root.
- A drop from the OS (`Window.SetOnDropped`) uses the same hit test.
- **v1 limitation:** you drag one item at a time, because `widget.Tree` only supports single
  selection. Dropping onto the same pane does nothing.

### `cmd/nscp`
- Minimal: create the app, the stores, the dialer and the manager, then
  `ui.NewMainWindow(...).ShowAndRun()`.
- If `profile.NewStore()` fails, show an error dialog and exit 1.

---

## Key Data Flows

### Connect a pane
```
host picker → Pane.connect(profile)
  → Manager.Acquire(ctx, profile)          [reuses a live shared connection]
      → Dialer.Connect
          → per hop: TCP / jump-host channel → ssh handshake
                → known_hosts check → prompter.ConfirmHostKey (unknown hosts only)
                → auth chain → prompter.Password / Passphrase if needed
  → RemoteFS.Home()
  → main goroutine: Profiles.Touch(lastUsedAt), Pane.setRemote(...)
```

### Drag a file to the other pane
```
fileRow.Dragged → dragCoordinator: ghost label follows the cursor, the target pane/row is highlighted
fileRow.DragEnd → MainWindow.startTransfers(src, entries, dstPane, dropDir)
  → goroutine: transfer.Conflict() for each entry
  → main: resolveConflicts dialogs → transfer.NewJob(...) → Queue.Submit
Queue worker → transfer.Run(job) → onFinished → main: reload the destination dir, refresh the panel
```

### Remote → remote relay
```
CopyFromRemote(src) ──► io.Pipe ──► CopyToRemote(dst, Size)   (bytes counted on the upload side)
```

---

## Coding Conventions

- No comments unless the **why** is non-obvious (hidden constraint, workaround, subtle invariant).
- No docstring blocks. One-line comments only when necessary.
- Use `errors.As` / `errors.Is` for typed error checks (e.g., `ErrHostKeyMismatch`).
- Tests use `t.TempDir()` — never write to real config directories.
- SSH behaviour is unit-tested against `internal/sshtest`. Anything that needs GNU find/stat on
  the server goes in the Docker integration tests.
- UI tests use `test.NewTempApp` and drain `runOnMain` callbacks with `pump()`.
- Build tag for Docker integration tests: `//go:build integration`.
- Use `var _ Interface = (*Concrete)(nil)` compile-time guards.

---

## Build & Test Commands

```bash
make run               # run in development
make build             # compile to bin/nscp
make test              # go test -race ./...
make vet               # go vet, including the integration package
make test-integration  # Docker: builds an Alpine sshd image, starts two servers
make lint              # requires golangci-lint
make package-macos     # requires the fyne CLI and an app icon (see FyneApp.toml)
```

To render a screenshot of the main window headlessly:

```bash
NSCP_SCREENSHOT=/tmp/nscp.png go test -run TestScreenshot ./internal/ui
```
