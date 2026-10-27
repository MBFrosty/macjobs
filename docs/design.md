# macjobs design

Architecture, contracts, safety model, and known limitations for the macjobs Go CLI on macOS. Updated October 12, 2026.

## Research

- **launchctl.** `launchctl print gui/<uid>/<label>` gives `state`, `pid`, `runs`, `last exit code` (or `last terminating signal`), `path`, and `program`/`arguments`. `print gui/<uid>` lists services in the domain, and `print-disabled gui/<uid>` lists enable/disable overrides. The manual says print output is "NOT API" and can change between releases, so parse only top-level `key = value` lines (split on the first `=`) and tolerate unknown or missing keys. `launchctl list` (PID, status, label; a negative status is the terminating signal) is the stable fallback. Use modern verbs `bootstrap`/`bootout`/`enable`/`disable`/`kickstart -k`; `load`/`unload` are legacy.
- **No last-run timestamp from launchd.** Infer it from the mtime of the job's StandardOut/ErrorPath log, from registry run stamps for jobs the tool creates, or optionally from `log show --predicate 'subsystem == "com.apple.xpc.launchd"'` (slow, so `--deep` only). Label inferred timestamps clearly.
- **plist schema.** `Label`, `Program`/`ProgramArguments`, `RunAtLoad`, `StartInterval` (seconds), `StartCalendarInterval` (dict or array of dicts of Minute/Hour/Day/Weekday/Month; missed runs coalesce on wake), `KeepAlive` (bool or dict: SuccessfulExit/NetworkState/PathState…), `StandardOutPath`/`StandardErrorPath`, `EnvironmentVariables`, `WorkingDirectory`. Plists can be XML or binary.
- **Go libraries.** `howett.net/plist` (pure Go, XML/binary, struct tags `plist:"Key"`, `[]map` for multiple calendar intervals), `spf13/cobra` for subcommands, `gopkg.in/yaml.v3` for the registry, stdlib `encoding/json` and `text/tabwriter` for piped tables, and `charmbracelet/lipgloss` for TTY tables (`lipgloss.Width` for ANSI-safe columns). Color is gated on a TTY stdout, an unset `NO_COLOR`, and no `--no-color`. No cgo.
- **Homebrew.** `brew services list --json` returns `[{name,status,user,file,exit_code}]`, with status started/scheduled/stopped/none/error/unknown/other. Homebrew uses JSON output itself. Dedupe `homebrew.mxcl.*` plists in the user's Library/LaunchAgents directory against the launchd scan.
- **Tailscale.** `tailscale serve status --json` returns ServeConfig `{TCP, Web{"host:port":{Handlers{"/path":{Proxy}}}}, AllowFunnel{"host:port":bool}, Services}`. Upstream notes that status JSON can omit Services, so prefer `tailscale serve get-config --all` when available. On macOS the CLI may be `/Applications/Tailscale.app/Contents/MacOS/Tailscale`.
- **Example jobs to seed.** Discover actual jobs on each machine; these are neutral examples:
  - A local dev server agent on :3773, with a desktop app's own server on :3774.
  - A webhook listener with an executable under the user's local share directory.
  - A weekly cleanup job with an executable under the user's local bin directory.
  - An app watchdog for a desktop app: every 10 minutes, reopens `/Applications/Example.app`, and logs to the user's Library/Logs directory.
  - Tailscale Serve: 443 maps to `127.0.0.1:3773`, with Funnel :8443 at `/webhook`.

## Analysis

- **Components.** (1) Collectors: launchd, brew, tailscale, processes, ports, and read-only cron. (2) A merge/model layer joining live state with the registry by label. (3) Health rules. (4) Lifecycle actions. (5) Templates/plist writer. (6) Table and JSON output. This is integration work with no networked service or database.
- **Health.** Each item rolls up to ok / warn / fail / unknown:
  - KeepAlive jobs that are not running, or high `runs` with a nonzero exit (possible crash loop).
  - Scheduled jobs with nonzero last exit or a log older than twice the interval.
  - A registry `port` not listening, checked with `lsof -nP -iTCP -sTCP:LISTEN`.
  - Tailscale Serve targeting a port with no listener, including dev servers that fail after an update.
  - A missing plist path or registry entry with no live job (orphan).
  - Overlapping expected ports, ports held by another job or an unattributed process, and missing inferred ports.
  - Launchd jobs whose `Program` or `ProgramArguments[0]` is a missing absolute path (managed/adopted: fail; foreign: warn).
- **Ownership.** launchd has no owner field. Ownership lives in the registry: a free-form `owner` (examples: `alice`, `ops`, `homebrew`, `unknown`), `purpose`, `tags`, `created_by_tool`, and `managed: true|false`. Discovered but unregistered items show `owner: unknown` and are read-only by default.
- **Key long-running processes.** Discover them from registry `match` (process name, regex, or port) instead of dumping all of `ps`. Show non-Apple jobs in `gui/<uid>` by default, hiding `com.apple.*`. `--all` includes Apple jobs for inspection.
- **Defaults chosen:**
  - Name: `macjobs`, optional shell alias `mj`.
  - Module: `github.com/MBFrosty/macjobs`; documentation and fixtures must be suitable for a public repository.
  - Registry: `~/.config/macjobs/registry.yaml`, resolving the home directory at runtime.
  - Managed plists: configurable label prefix, with generic default `local.macjobs.<name>`, installed in the user's Library/LaunchAgents directory.
  - Logs: `~/Library/Logs/macjobs/<name>.log`.

## Package layout

Go 1.23+, single module:

```text
cmd/macjobs/main.go         # cobra root; global flags --json --dry-run --yes --all
internal/launchd/           # print/list parser, domains, bootstrap/bootout/kickstart/enable/disable
internal/cron/              # pure crontab parser: schedules, IDs, env, shell-word heuristic
internal/plist/             # launchd structs (howett.net/plist), schedule humanizer
internal/brew/ internal/tailscale/ internal/procs/   # collectors (exec + JSON), lsof port probe
internal/registry/          # YAML load/save, atomic write, schema version
internal/model/             # Item{ID,Kind,Label,Owner,Purpose,Schedule,LastRun,LastExit,PID,State,Health,Notes}
internal/health/            # rule engine used by list/show/doctor
internal/actions/           # lifecycle safety gate: managed vs foreign, dry-run, confirm
internal/templates/         # Go structures: interval, calendar, keepalive, app-watchdog
internal/watchdog/          # bundle checks, specific probes, persisted recovery limit, one-line logs
testdata/                   # sanitized launchctl/brew/tailscale captures, golden JSON
```

- **Exec seam.** All shell-outs go through a `Runner` interface. Tests replay sanitized fixtures and never touch launchd or other host service managers.

## Cron collector

The inspection Runner gains exactly `crontab -l` (one argument, matched individually); `-e`, `-r`, `-u`, filenames, and every other argument combination are refused. The lifecycle Runner gains no commands. `Collector.SystemCrontab` defaults to `/etc/crontab` in `New`; fixture collectors inject a temporary path or leave it empty to skip system-file I/O. No sudo or spool access is used.

The pure parser skips blank/comment lines, keeps physical line numbers (including CRLF), records environment assignments, validates five-field schedules with ranges/steps/lists/month/day names, and accepts `@reboot`, `@yearly`, `@annually`, `@monthly`, `@weekly`, `@daily`, `@midnight`, and `@hourly`. System entries consume a run-as column before the command. Command spacing is preserved. Environment scope follows line order, with inline assignment prefixes included. SHELL/PATH remain private parser context; relative executable resolution and shell evaluation are deliberately omitted.

IDs are `cron:<user|system>:<hash8>` where hash8 is the first eight hex digits of SHA-256 over normalized schedule + NUL + original command. Identical entries get ordered `-2`, `-3` suffixes. Line numbers are separate observations; comments/reordering preserve IDs, command/schedule changes orphan prior registry metadata. Run-as is an additive observation rather than part of the hash; identical system schedules/commands with different users are distinguished by the duplicate suffix. Malformed entries use `cron:<source>:invalid-L<n>` and retain no raw command/schedule in output.

Cron items are observed/live when their source was read and have state `scheduled`, owner `unknown` until registry merge, and `readOnly: true`. Source availability is tracked independently for user/system registry orphans. A successful empty read or missing system file means no jobs. Exit 1 with empty stdout is also treated as an empty user crontab because stderr is suppressed; permission denial with the same output/exit is indistinguishable. Other user failures warn about Full Disk Access, and system read failures warn that the file was skipped. Unavailable sources yield unknown health for registry entries, rather than false orphan failures.

All cron env values are hidden in `show`; values of at least `redact.MinSecretLength` are also registered for output/log redaction regardless of key sensitivity. Inline assignments and credential options are masked by shell-word boundaries, including short values and quoted/escaped words, without execution. URL userinfo and authorization patterns reuse the shared Redactor. Labels are redacted before truncation. Doctor checks the first literal shell word after assignment prefixes with the shared absolute-program existence helper (foreign: warn, managed: fail); malformed lines warn without content, and `%` yields a semantics warning. No run history, next-run, or log-staleness inference is added.

JSON keeps schemaVersion 1 with optional item `command`, `source`, `line`, `runAs`, and cron show `entry: {schedule, command, env}` plus `plist: null`. Existing kinds retain their show shape. Registry kind validation and tag transactions accept cron; tags never grant lifecycle authority. Execute/adopt/create guards refuse cron before plist checks or command execution, including previews and confirmation overrides.

## Lifecycle and creation

- **Read-only inventory.** `list`, `show`, `logs`, `ports`, and `doctor` never mutate launchd or registry files.
- **Lifecycle.** `start`/`stop`/`restart` (`kickstart -k`, `bootout`/`bootstrap`), `enable`/`disable` (launchctl enable/disable plus bootout), `edit` (opens `$EDITOR` on the plist, validates with `plutil -lint`, then reloads), and `remove`. Brew items delegate to `brew services`. Tailscale and cron remain display-only.
- **Foreign items** (unregistered, `managed:false`, or `created_by_tool:false`) require BOTH `--yes` and typed-label confirmation on a TTY for `stop`, `restart`, `disable`, `remove`, and `edit`. Non-TTY callers refuse even with `--yes`. Every mutation supports `--dry-run`, showing launchctl/brew/file operations without execution.
- **Stop** uses `bootout` to unload rather than kill, preventing scheduled/KeepAlive relaunch until bootstrap. **Restart** bootstraps unloaded jobs before `kickstart -k`.
- **`remove`** moves the plist to `~/.config/macjobs/trash/` without deleting it.
- System daemons, system domains, Apple jobs, and System paths are always refused. The tool never invokes sudo. An ancestor-PID guard blocks stop/restart/disable/remove/edit-reload without an override.
- **`create <template>`** — interval, calendar, keepalive, and app-watchdog plists, configurable managed prefix, private plist publication, validation, registry provenance, enable/bootstrap, and rollback.
- **`adopt <label>`** records management and adoption time, preserving foreign provenance unless explicitly asserted with `--as-created`. System and Apple jobs remain protected.
- **`watchdog check`** is part of the installed binary. Bundle main-executable matching avoids treating helpers as the app; absent apps reopen at most once per persisted relaunch interval.
- **Connectivity probes** replace any-TCP heuristics with configurable TCP peer, timestamped app-log, and loopback HTTP probes. All probes must agree; any negative wins, unavailable evidence is unknown, and a positive TCP peer requires fresh app-level corroboration. No machine paths or relay defaults are embedded.
- Freshness uses event timestamps, never app-log modification times. Generated watchdogs pass their scheduled interval as the default maximum evidence age; operators should tune maximum age to the discovered heartbeat cadence. HTTP 200 alone is insufficient: the status response needs an explicit connection boolean and a fresh timestamp for positive state.
- Disconnect recovery requires explicit `--restart-on-disconnect` (default off), a negative result, the existing state/lock rate limit, and a clean quit scoped to that app in the Runner. Reopen only after verified main-process exit; never kill or modify services. JSON remains schemaVersion 1 with additive per-probe diagnostics.
- Creation/adoption previews execute no external commands. Removal optionally purges tool-created registry logs.

## JSON and registry contracts

- All JSON responses carry `schemaVersion: 1`. Additive optional fields retain version 1; consumers tolerate unknown fields. Breaking changes require a version bump and migration documentation.
- **list/doctor:** `{schemaVersion, items, warnings}`. **ports:** `{schemaVersion, listeners, warnings}`. **show:** `{schemaVersion, item, plist, warnings}`. **registry init:** registry document. **logs --json:** one record per line with `schemaVersion`, `id`, `path`, `line`.
- Lifecycle action JSON includes `action`, `id`, `label`, `kind`, `dryRun`, `ops`, `result`, and `error`. Results are `planned`, `completed`, `unchanged`, `refused`, or `failed`.
- Registry YAML uses `schemaVersion: 1`, optional `label_prefix`, and `entries` with strict decoding. Unknown fields and unsupported versions are rejected.

## Safety model

- Inspection Runner allowlist is separate from lifecycle Runner allowlist.
- Tailscale routes, cron entries, and registry-matched processes are display-only for lifecycle; tags are metadata only.
- Foreign-job TTY confirmation, dry-run previews, and ancestor-PID guards apply as documented in README.md.
- Output redaction applies to environment values, credentials, and sensitive paths in diagnostics.

## Skills

- Canonical contributor skill: `.agents/skills/macjobs/SKILL.md`; relative directory symlinks in Claude/Codex/Cursor skill directories.
- Operator skill: `skills/macjobs-bot/SKILL.md`. Refresh command/flag lists from root/subcommand help in a temporary candidate build; document operator refresh from installed help without implicit installation.

## Verification

- **Unit and golden tests.** Sanitized fixtures: `launchctl print` for running, scheduled, signal-killed, and never-exited agents; `launchctl list`; `brew services list --json`; `tailscale serve status --json`; and `get-config --all`. Include plist round-trip and schedule humanizer tests.
- **On-Mac smoke test** (from an isolated worktree):
  - Run `go vet ./...`, `go test ./...`, `go build ./...`, and `staticcheck` when available.
  - Build to a temporary directory outside the repository.
  - Run `list --json`, `doctor`, and `show` against a local example label.
  - Run create, start, logs, stop, remove on `local.macjobs.smoketest` when validating lifecycle.
- **Off-limits during development.** Existing production jobs receive read commands only unless explicitly part of a controlled test job.
- **Cleanup.** Remove smoke-test jobs, worktrees after merge or abandonment, and temporary fixtures/build output. Preserve primary checkouts and unrelated uncommitted work.

## Risks and limitations

- **Domains and privilege.** User agents live in `gui/<uid>`. Daemons (`/Library/LaunchDaemons`, `system/`) may require elevated privileges to read fields or mutate state. System items are shown read-only. The tool never calls sudo or stores credentials.
- **SIP.** `/System/Library/Launch*` and `com.apple.*` jobs cannot be modified. Filter by default and hard-refuse lifecycle actions.
- **TCC.** Deep `log show` and other apps' logs may require Full Disk Access. Degrade gracefully and report `unknown`.
- **Output drift.** `launchctl print` is not an API. Use tolerant parsing, a `launchctl list` fallback, and fixtures for each macOS release.
- **Self-interruption.** Restarting the app hosting a worker kills the worker. Refuse lifecycle mutations for any job whose PID is the current process or an ancestor, rechecking after editing.
- **Watchdog false restarts.** Bundle matching, helper distinction, and rate-limiting limit relaunch loops. Specific fresh app-level evidence is required before reporting connectivity; unavailable signals never trigger disconnect recovery.
- **Port visibility.** Non-root `lsof` on macOS sees only the current user's processes. Root-owned listeners, launchd-held sockets, and some Tailscale front ports may be invisible; findings use `unknown` rather than `fail` when visibility is incomplete.
- **Doctor is report-only.** It does not auto-fix Serve routes or restart jobs.

## Acceptance criteria

- `macjobs list` shows local example jobs, brew services, and Tailscale Serve/Funnel routes with owner, purpose, schedule, last run, last exit, and health.
- Every read command has stable JSON with `schemaVersion` for non-interactive consumers.
- `doctor` flags deliberately unhealthy test agents and passes on a healthy machine.
- Foreign lifecycle operations refuse without confirmation. `--dry-run` makes zero changes, verified by diffing launchctl and plist state before/after.
- `create` plus `remove` on a throwaway template job round-trips without leftover plist, log, registry entry, or temporary file.
- No secrets printed. Environment values remain redacted in all output.

### Apple-owned listeners

The collector reads executable text mappings for visible listening PIDs through the Runner, in batches of at most 64. It uses the first text mapping per PID, never shared libraries or `ps` names, to classify cleaned executable paths under `/System/`, `/usr/libexec/`, `/usr/sbin/`, or `/Library/Apple/`. An attributed `com.apple.*` label is also sufficient. Missing mappings fail open for visibility: unknown listeners remain unattributed. This is a location heuristic, not signature verification.

Listener JSON adds optional `apple: true` while retaining schemaVersion 1 and existing attribution fields. Default ports output hides Apple listeners; `--all` includes them marked Apple; `--unattributed` excludes Apple listeners regardless of `--all`. Doctor excludes them from the no-known-job count but retains them for actual expected-port conflicts. An unowned third-party executable in `/Applications/` still counts as unattributed.
