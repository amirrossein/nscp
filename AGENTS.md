# NSCP — Agent Instructions

Read [PROJECT.md](PROJECT.md) for the full project reference before making any changes.
Read [TASK.md](TASK.md) for the ordered implementation task list.

## Quick orientation

- Go + Fyne desktop app for SSH/SCP file management.
- Module: `github.com/amirrossein/nscp`
- All implementation tasks are in TASK.md, numbered TASK-01 through TASK-24.
- Implement tasks in the dependency order shown in the summary table at the end of TASK.md.

## Non-negotiable rules

1. SCP only for transfers — no SFTP, no rsync.
2. Secrets go to the OS keychain (`internal/credential`) — never to JSON files.
3. Remote paths use `path` package; local paths use `filepath`.
4. All Fyne widget mutations must happen on the main goroutine.
5. `TransferJob` public methods must be goroutine-safe.
6. Never edit `cmd/nscp/bundled.go` by hand — regenerate with `fyne bundle`.

## How to verify your work

```bash
go build ./...          # must exit 0
go test -race ./...     # must pass
make build              # produces bin/nscp
```
