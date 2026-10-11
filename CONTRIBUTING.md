# Contributing

Use the Go version declared in `go.mod`. macOS is required for live inspection; fixture tests also run on Linux.

## Checks

Run before opening a pull request:

```bash
go vet ./...
go test ./...
go build ./...
```

External commands belong behind the Runner interface. Add sanitized replay fixtures with neutral names only (no personal paths, hostnames, or job labels). Never commit credentials or machine-specific configuration.

Any CLI change must also update the repository skills described in `AGENTS.md`. Do not reference issue trackers or personal details in commits or pull requests.

## Security

Report security issues privately as described in `SECURITY.md`.

## Releases

Maintainers cut releases as described in `RELEASING.md`.

## Cleanup

Delete scratch files, temporary builds, `dist/`, and unused worktrees after the work is merged or abandoned.
