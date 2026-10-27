---
name: macjobs
description: Use when inspecting, creating, starting/stopping, tagging, or troubleshooting macOS background jobs with the macjobs CLI, or when changing macjobs itself.
metadata:
  internal: true
---

# macjobs

Portable operator guidance for assistants lives in `skills/macjobs-bot/SKILL.md` (`macjobs-bot`); install that skill when the human wants their bot to run macjobs on a Mac.

## Ground rules

- Install: `brew install --cask MBFrosty/tap/macjobs` (signed and notarized).
- `doctor` exits 1 when it has findings (any `fail` item), 0 otherwise. `logs --json` is NDJSON: one object per line, not one document. `list --kind` only accepts launchd/system/brew/tailscale/process/cron and exits 1 otherwise. A foreign-job `--dry-run` of stop/restart/disable/edit/remove includes a `note` that the real run needs `--yes` plus typed TTY confirmation.
- Read-only first: `list`, `show`, `logs`, `ports`, and `doctor` are safe inspection commands. `doctor` reports and never fixes.
- Before `create`, `edit`, `enable`, `disable`, `start`, `stop`, `restart`, `remove`, `adopt`, `tag`, or `untag`, run with `--dry-run`, show the plan, and obtain approval for the real operation. Never remove without explicit confirmation. Existing explicit approval for the same operation remains valid.
- Foreign jobs require `--yes` and a typed TTY label confirmation for stop, restart, disable, edit, and remove. Never bypass this; ask the user to perform the confirmation. Start and enable do not have that tool gate, but still need the preview/approval workflow.
- System jobs, Apple jobs, Tailscale routes, selected processes, and cron entries are protected from lifecycle changes. Tags are metadata only for all six kinds; Apple jobs cannot be tagged. Tags never grant management or creation provenance.
- Prefer `--json` (schemaVersion 1) for parsing; use tables for people. TTY `list`/`doctor` color and the `show` header apply only when stdout is a TTY, `NO_COLOR` is unset, and `--no-color` is not passed. Piped tables and `--json` stay unchanged. Logs are redacted, but never paste secrets into chat.
- Times ending in `Z` are UTC; convert to the user's zone. LAST RUN is inferred from log mtime; an old value can mean nothing was logged.
- `registry init` and `watchdog check` have no dry-run. Registry initialization writes metadata; get approval before bootstrap or replacement. Watchdog checks can reopen apps and write log/state files; do not treat them as read-only inspection.

## Common commands

```sh
macjobs list
macjobs list --all
macjobs list --kind cron --json
macjobs list --kind launchd --owner team --unhealthy
macjobs list --tag homelab
macjobs list --tag a,b
macjobs list --tag a --tag b --json
macjobs list --port 8080
macjobs list --port 8080,443 --kind launchd
macjobs ports
macjobs ports --unattributed --json
macjobs show com.example.worker
macjobs logs com.example.worker -n 100
macjobs logs com.example.worker -f
macjobs doctor --json
macjobs tag com.example.worker homelab --dry-run --json
macjobs untag com.example.worker homelab --dry-run --json
macjobs restart com.example.worker --dry-run --json
macjobs adopt com.example.worker --owner team --purpose 'Example worker' --tag homelab --dry-run
macjobs registry init
macjobs create interval --name example --owner team --purpose 'Example worker' --tag homelab --every 10m --dry-run -- /bin/echo example
macjobs create calendar --name example-weekly --at 'Sun 03:00' --dry-run -- /bin/echo example
macjobs create app-watchdog --name example-watchdog --app 'Example App' --every 10m --dry-run
```

Remove `--dry-run` only after approval. Use exact IDs from `list`: labels for launchd, `system:<label>` for system, `homebrew.mxcl.<service>` for Homebrew, route IDs beginning `tailscale:` for routes, registered IDs for process matches, and `cron:user:<hash8>` / `cron:system:<hash8>`. Homebrew services may appear as launchd items when deduplicated.

Tags normalize to lowercase, match `^[a-z0-9][a-z0-9._-]{0,31}$`, and are sorted and deduplicated. Tag/untag take separate positional tags. The `--tag` flag on list/create/adopt accepts repeated flags and comma lists. List matches ANY requested tag, ANDed with kind, owner, port, and unhealthy. `--port` accepts repeated flags or comma lists and matches ANY observed or expected port, including Tailscale front and backend ports. Untagging a missing tag is an unchanged success. Tag creates an unmanaged metadata stub if needed; untag does not create an absent entry. Create refuses existing IDs; adopt merges tags and preserves existing metadata/provenance unless explicitly changed. Empty tags are omitted from YAML/JSON and shown as `-` in the table.

`ports` lists visible TCP listeners and unconnected UDP sockets with job attribution (direct PID or process-tree ancestor). `--unattributed` keeps non-Apple listeners with no known job, including when combined with `--all`. The PORTS column follows PID: observed ports (`8080`, `5353/udp`), missing expected ports prefixed `!`, at most four values then `+N`, or `-`. Unattributed listeners are not new list items.

For a watchdog, select actual fresh app-level evidence before configuring `--probe-log`, `--probe-http`, regexes, or `--tcp-peer`. TCP alone cannot establish a fresh connection. `--restart-on-disconnect` is opt-in and only acts after confirmed `connected=no`; leave it off unless requested. `macjobs watchdog check --app 'Example App' --log ~/Library/Logs/example-watchdog.log` runs one check, including possible reopen and state/log writes.

## Cron (read-only)

`list --kind cron`, `show <cron id>`, and `doctor` include user `crontab -l` and readable `/etc/crontab`. IDs hash normalized schedule + NUL + original command; comments/reordering preserve IDs, duplicates gain `-2`, `-3`, and command edits change IDs and can leave absent registry entries. Use exact IDs from list. State `scheduled` only means listed, not proof of execution. System entries include `runAs`.

Lifecycle commands refuse cron with "cron items are read-only in macjobs", including `--dry-run` and `--yes`; `create cron` and adoption are refused. Never edit, remove, or install a crontab through another tool. Tag/untag remain local registry metadata only and follow the normal preview/approval workflow.

Show adds `entry: {schedule, command, env}` with env keys/redacted values and `plist: null`. Additive item fields `command`, `source`, `line`, `runAs` retain schemaVersion 1. Env assignments (including inline prefixes and short values) and recognized command credentials stay private. Malformed entries never show raw text. Missing literal absolute executables warn (fail if managed); malformed lines and `%` semantics warn. Relative programs are not resolved through PATH. Doctor still exits 1 only for fail.

Check warnings for unavailable crontabs, including possible Full Disk Access restrictions under launchd. Missing/empty crontabs are normal. Stderr is suppressed: exit 1 with empty stdout is treated as no crontab, so identical permission-denial output is ambiguous. Never use sudo or read the spool. Login items, next-run calculation, and cron run history are outside scope.

## Reading results

- `fail` on an `application.*` registry orphan can be a stale app record after a restart. Confirm current inventory before removing metadata; the finding alone does not establish a broken service.
- `system:org.cups.cupsd` not running is normal when no printing is requested.
- A scheduled job with no log yet can have `unknown` health before its first run.
- Watchdog `connected=unknown` means no fresh app-level evidence; it does not establish a disconnect.
- `doctor` exits nonzero for failing health after printing findings. Check warnings before interpreting missing inventory as absence.
- A missing declared port is `fail` only for a KeepAlive or running job when listeners are visible. System jobs, socket-activated plists, and Tailscale front ports never fail for an invisible listener; report `unknown` or omit the finding.
- Overlapping expected ports, a port held by another job or an unattributed PID, and a missing inferred port are `warn`. A Tailscale route to a dead local backend remains `fail` and names the job that expected that backend when known.
- Doctor may warn that N listeners have no known job; inspect them with `macjobs ports`. A non-root `lsof` cannot see root-owned or launchd-held sockets. Never use sudo.
- A launchd job whose `ProgramArguments[0]` or `Program` is an absolute path that does not exist is `fail` when the job is managed or adopted, and `warn` when it is foreign. The path is redacted in output. System jobs and Apple jobs skip this finding.

## Command and flag reference

Checked the installed binary help and refreshed from the candidate binary's root and every subcommand's `--help`, built in a temporary directory. The candidate adds `--no-color`. Refresh from the installed binary after upgrading for operator use; use a temporary candidate binary for uninstalled CLI changes and explicitly record that distinction. Never install as a side effect of refreshing documentation.

Global flags: `--all`, `--json`, `--no-color`, `--registry <file>`, `-h/--help`. Root also supports `-v/--version`. Optional `mj` is a shell alias (`alias mj=macjobs`) or a symlink to the same executable, not a CLI alias. Root `--help` does not list an `mj` alias.

- `list`: `--kind`, `--owner`, `--tag`, `--port`, `--unhealthy`.
- `ports`: `--unattributed`. Table columns are PROTO PORT HOST PID COMMAND JOB VIA FINDINGS.
- `show <id>`: `--show-env` is deprecated and never reveals environment values.
- `logs <id>`: `-n/--lines` (default 50), `-f/--follow`.
- `doctor`: no additional flags; report only.
- `tag <id> <tag>...`, `untag <id> <tag>...`: `--dry-run`.
- `start|stop|restart|enable|disable|edit|remove <id>`: `--dry-run`, `--yes`. Remove also has `--purge-log`, restricted to unshared logs of tool-created jobs. Edit uses `$EDITOR` without shell expansion.
- `registry init`: `--force` replaces all metadata, including tags; never use without approval for that loss.
- `create <template> --name <name> [-- <command...>]`: templates are `interval`, `calendar`, `keepalive`, `app-watchdog`. Flags: `--name`, `--owner` (default unknown), `--purpose`, `--tag`, `--log`, `--dry-run`, `--every`, `--at` (repeatable), `--app`, and the probe flags below. Commands require an absolute executable. Log defaults to `~/Library/Logs/macjobs/<name>.log`.
- `adopt <label>`: `--owner`, `--purpose`, `--tag`, `--dry-run`, `--as-created`. Only user launchd agents are eligible. `--as-created` asserts tool provenance and bypasses later foreign-job confirmation; do not use unless accurate and approved.
- `watchdog check`: `--app`, `--log`, `--state` (default `~/.config/macjobs/state/<app>.json`), `--interval` (default 10m), `--relaunch-interval` (default 10m), and the probe flags below.
- Probe flags on create/watchdog check: `--probe-log`, `--connected-regex`, `--disconnected-regex`, `--probe-http` (loopback HTTP only), `--tcp-peer` (repeatable alternatives), `--max-age` (defaults to check interval), `--restart-on-disconnect` (default false).
- `completion bash|fish|powershell|zsh`: `--no-descriptions`; generates a script. Follow subcommand help for shell integration.
- `help [command]`: command-specific help. `registry`, `watchdog`, and `completion` are parent commands; select their subcommand.

## Developing macjobs

Read AGENTS.md, README.md, and docs/design.md before changing behavior. Keep host inspection read-only and route every external command in the application through the appropriate Runner allowlist. Tests use sanitized replay fixtures or fake collectors/Runners and never query host service managers. Keep machine configuration outside the repo, redact errors and output, and never invoke sudo.

Build and checks:

```sh
go build ./...
go vet ./...
go test ./...
```

CI also runs `go test -race ./...` on Ubuntu and macOS. For help refresh, build with `go build -o <temporary-directory>/macjobs ./cmd/macjobs`, read root and subcommand help, then delete the binary and directory. Run the same checks before publishing; retain an open PR's worktree until merge or abandonment, then remove it and its branch. Delete temporary output and build artifacts.

Only when installation is explicitly requested:

```sh
mkdir -p "$HOME/.local/bin"
go build -o "$HOME/.local/bin/macjobs" ./cmd/macjobs
ln -s macjobs "$HOME/.local/bin/mj"
```

The README's optional `mj` symlink (or a shell alias) points to the installed macjobs binary; for a `go install` installation it uses the Go bin directory. Do not overwrite an existing binary or symlink without authorization. Watchdogs embed the running binary's absolute path; Homebrew Caskroom/Cellar installs prefer a stable `bin/macjobs` link that refers to the same file. Otherwise keep that location permanent.

### Registry schema

Default path: `~/.config/macjobs/registry.yaml`, overridable by `--registry`. `schemaVersion: 1`; `label_prefix` defaults to `local.macjobs.` and must match `^[A-Za-z0-9][A-Za-z0-9.-]*\.$`. Entries have:

- `id`: nonempty, unique. `kind`: omitted/default launchd, or launchd/system/brew/tailscale/process/cron.
- `owner`: free-form; empty/omitted displays as unknown. `purpose`: optional description.
- `managed`, `created_by_tool`: booleans; tags never change them. Optional `created_by`, `template`, `adopted_at` record provenance/adoption.
- `tags`: optional list of lowercase tokens matching `^[a-z0-9][a-z0-9._-]{0,31}$`. CLI input lowercases; registry YAML must already use lowercase valid tokens. Load/save deduplicate and sort without changing the caller's slices.
- `port`: optional 1–65535 (zero means omitted); `ports`: optional list of 1–65535, sorted and deduplicated on load/save; `log`: optional path, supporting `~/`.
- `match`: optional `name`, `regex`, `port`; at least one is required. Regex must compile, port is 1–65535 when specified, and all supplied conditions must match. Process arguments are never collected.

```yaml
schemaVersion: 1
label_prefix: local.macjobs.
entries:
  - id: com.example.worker
    kind: launchd
    owner: team
    purpose: Example worker
    tags: [homelab]
    managed: false
    created_by_tool: false
```

Unknown fields and unsupported versions are rejected via strict YAML decoding. Older binaries reject registries containing tags or `ports`. `registry init --force` replaces all metadata, tags included. Writers lock metadata transactions and publish atomically; dry-runs skip locks to avoid creating files. Lock contention fails safely and should be retried after the other writer finishes.

### JSON output contract

All JSON uses `schemaVersion: 1`. Additive optional fields such as `tags` retain version 1; consumers tolerate new fields and states. Removing/renaming fields, changing types or existing meanings, or incompatible shape changes require a version bump and migration documentation/tests. Registry YAML has its own version and strict fields; adding an optional YAML field still affects compatibility with old binaries.

- list/doctor: `{schemaVersion, items, warnings}`; ports: `{schemaVersion, listeners, warnings}`; show: `{schemaVersion, item, plist, warnings}`. Empty item tags and empty item `ports` are omitted. Additive item `ports` entries are `{proto, port, host?, pid?, source, listening, via?}` with source `declared|socket|inferred|tailscale|observed`. Listeners add `command`, `jobId`, and `expectedBy`. `port` is unchanged.
- registry init: registry document. logs: one JSON record per line, with schemaVersion/id/path/line.
- Actions: schemaVersion/action/id/label/kind/dryRun/ops/result/error; create adds redacted plist XML. Tag/create/adopt previews include nonempty resulting `tags`. Ops describe plans, not proof of completion. `registry-tag`/`registry-untag` are Go metadata operations, never shell commands.
- Results: planned/completed/unchanged/refused/failed. Error objects have stable code and safe message; failed/refused operations exit nonzero. JSON remains one document except log streaming; confirmation prompts go to stderr.

## Apple listener classification

`ports` hides Apple-owned listeners by default; `--all` includes them marked `Apple`. JSON uses the same filters and adds optional `apple: true` without changing schemaVersion 1 (absent means false). Apple listeners never contribute to the doctor's no-known-job warning, including with `--all`, but still participate in expected-port conflict checks.

Ownership uses the first executable text mapping from a read-only `lsof` query for visible listening PIDs. Cleaned paths under `/System/` (including Cryptexes), `/usr/libexec/`, `/usr/sbin/`, and `/Library/Apple/` count as Apple; an attributed `com.apple.*` job also counts. This location heuristic does not verify signatures. Missing mappings and process names alone do not establish ownership. Unowned apps under `/Applications/` remain unattributed. No additional flags were introduced; help was checked against the installed binary and a temporary candidate build without installing it.
