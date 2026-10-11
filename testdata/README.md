# Replay fixtures

The launchctl, Homebrew, Tailscale, plist, and listening-port fixtures retain structures captured from a macOS machine. Labels, users, paths, addresses, route targets, environment values, and command names were replaced with neutral placeholders before writing these files. Only the selected launchctl fields are retained. Process names use neutral examples in the observed column format. Extra port fixtures cover IPv4/IPv6 TCP duplicates, unconnected versus connected UDP, and a process tree with spaces in `comm` plus a parent cycle.

The golden inventory combines these fixtures with synthetic registry entries to exercise an orphan and a dead route target. Additional inline fixtures test parsing drift and failure states without invoking host commands. Tests use a strict replay Runner and temporary files, never launchd.

To update the golden file after reviewing an intentional schema change, run `MACJOBS_UPDATE_GOLDEN=1 go test ./internal/inventory -run TestGoldenInventory` and inspect the diff.

Action golden files cover lifecycle dry-run plans and structured refusal errors. Home directories are normalized to `<home>`. Action tests use a fake Runner for all service, editor, validation, and parent-PID commands.
