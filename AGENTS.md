# NSCP — Agent Instructions

Read [PROJECT.md](PROJECT.md) for the full project reference before making any changes.

## Quick orientation

- Go + Fyne desktop app: two panes, each showing a local or SSH directory tree. Drag between
  them to copy files over SCP.
- Module: `github.com/amirrossein/nscp`

## Non-negotiable rules

1. SCP only for transfers — no SFTP, no rsync. Remote → remote is relayed through the client.
2. Secrets go to the OS keychain (`internal/credential`) — never to JSON files.
3. Remote paths use `path` package; local paths use `filepath`. Quote remote paths with `vfs.ShellQuote`.
4. All Fyne widget mutations must happen on the main goroutine (`runOnMain` in `internal/ui`).
5. `transfer.Job` public methods must be goroutine-safe.

## How to verify your work

```bash
go build ./...          # must exit 0
make test               # go test -race ./...
make test-integration   # needs Docker
make build              # produces bin/nscp
```
