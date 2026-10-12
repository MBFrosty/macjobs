# macjobs

**macjobs: the bot- and agent-first job manager for Mac.** One inventory across launchd, Homebrew services, Tailscale Serve/Funnel and processes, readable and safely operable by AI agents.

- **Stable machine output** — versioned `--json` (`schemaVersion`) for list, show, doctor, ports, logs, and lifecycle actions.
- **Local context** — owner, purpose, and tags in a registry beside live state, so agents know what a job is for.
- **Safe changes** — redacted logs and output, dry-run on every mutation, and typed confirmation for jobs the tool did not create.

## Quickstart

Install with Homebrew (tap must exist before the first public release):

```sh
brew install MBFrosty/tap/macjobs
macjobs --version
```

Or with Go 1.23 or newer:

```sh
go install github.com/MBFrosty/macjobs/cmd/macjobs@latest
macjobs --version
```

Optional convenience in `~/.zshrc` (a shell alias, not a separate binary):

```sh
alias mj=macjobs
```

First commands:

```sh
macjobs list
macjobs doctor --json
macjobs show com.example.worker
```

Homebrew and Tailscale are optional; unavailable collectors produce warnings. Live inspection requires macOS. Fixture tests and builds also work on Linux. No cgo is used. For a source checkout, use `go build ./cmd/macjobs`.

## Give it to your bot

Operator-focused skill for assistants with shell access on a Mac (not for macjobs development):

1. **Skills CLI** ([vercel-labs/skills](https://github.com/vercel-labs/skills)):

   ```sh
   npx skills add MBFrosty/macjobs@macjobs-bot
   npx skills add MBFrosty/macjobs@macjobs-bot -g
   ```

   A bare `npx skills add MBFrosty/macjobs` lists only the public bot skill; the dev skill in `.agents/skills/macjobs` is marked internal.

2. **Fetch or paste**: any assistant that can read a URL can load the skill anonymously:

   `https://raw.githubusercontent.com/MBFrosty/macjobs/main/skills/macjobs-bot/SKILL.md`

   Or copy the contents of [`skills/macjobs-bot/SKILL.md`](skills/macjobs-bot/SKILL.md) into the bot's custom instructions or skills directory.

3. **In this repo**: canonical path `skills/macjobs-bot/SKILL.md`. The contributor skill remains `.agents/skills/macjobs/SKILL.md` (symlinked under `.cursor/`, `.claude/`, and `.codex/`).

## How it compares

| Tool | Interface | Scope | AI / automation | License |
| --- | --- | --- | --- | --- |
| [launchdeck](https://github.com/sderosiaux/launchdeck) | Rust TUI | launchd + Homebrew services | guarded lifecycle, plist edit/delete; creates user LaunchAgents; `/System` inspect-only | (see repo) |
| [lanchr](https://github.com/lu-zhengda/lanchr) | Go CLI + TUI | launchd agents and daemons | `--json` on all commands; ships a Claude Code skill | MIT |
| [LaunchControl](https://www.soma-zone.com/LaunchControl/) | Native GUI | launchd jobs | expert/XML editing, log views, troubleshooting | Commercial |
| [ZLaunch Manager (zlm)](https://github.com/zavora-ai/macos-zlaunch-manager) | Swift GUI + CLI + MCP server | launchd only (user/global/system domains) | MCP tools for AI assistants | Apache-2.0 |
| **macjobs** | Go CLI | launchd, Homebrew, Tailscale Serve/Funnel, registry processes/ports | versioned `--json`, registry, redaction, dry-run, portable bot skill | MIT |

- **[launchdeck](https://github.com/sderosiaux/launchdeck)** unifies launchd jobs and Homebrew services in a Rust TUI with status, schedules, plist metadata, provenance, logs, and guarded lifecycle actions. macjobs focuses on one inventory that also spans Tailscale routes and matched processes, with a versioned JSON contract, a local registry, redaction, and dry-run-first changes.
- **[lanchr](https://github.com/lu-zhengda/lanchr)** is a Go CLI and TUI for launchd with list/info/search, enable/disable/load/unload/restart, logs, doctor, templates, export/import, and `--json` on every command. macjobs adds Homebrew and Tailscale collectors, port attribution, tags, and foreign-job confirmation for safe agent operation.
- **[LaunchControl](https://www.soma-zone.com/LaunchControl/)** is a commercial native GUI for creating, managing, and debugging launchd jobs with expert editing and log views. macjobs is CLI-first for scripts and agents, combines multiple backends, and keeps mutations behind dry-run and confirmation gates.
- **ZLaunch Manager ([zlm](https://github.com/zavora-ai/macos-zlaunch-manager))** pairs a SwiftUI app and a `zlm` CLI with an MCP server, so AI assistants can manage launchd services. macjobs focuses on one inventory that spans launchd, Homebrew services, Tailscale Serve/Funnel and processes, with a versioned `--json` contract, a registry, redaction and dry-run-first changes.

## Install notes

Release binaries (v0.1.1 and later) are **signed with Developer ID and notarized by Apple**, so the Homebrew cask and direct downloads run without Gatekeeper prompts. A bare command-line binary cannot be stapled, so the **first launch needs internet** for Gatekeeper to confirm notarization online.

For direct downloads from GitHub releases, verify with `checksums.txt`. To check the signature:

```sh
codesign --verify --strict --verbose=2 "$(brew --prefix)/bin/macjobs"
spctl -a -vvv -t install "$(brew --prefix)/bin/macjobs"
```

`go install` builds locally and is unaffected by Gatekeeper quarantine on downloaded release binaries.

## Usage

```sh
macjobs list
macjobs list --json --kind launchd --owner team
macjobs list --tag homelab
macjobs list --tag a,b --kind launchd --owner team
macjobs list --tag a --tag b --unhealthy
macjobs list --port 8080
macjobs list --port 8080,443 --kind launchd
macjobs list --unhealthy
macjobs list --all
macjobs ports
macjobs ports --unattributed --json
macjobs show com.example.worker
macjobs show com.example.worker --json
macjobs logs com.example.worker -n 50
macjobs logs com.example.worker -f
macjobs doctor --json
macjobs registry init
```

`list` shows IDs, kinds, owners, tags, state, PID, ports, schedule, inferred last run, last exit, health, purpose, and findings. `--all` includes Apple jobs, which are otherwise hidden. Kinds are `launchd` (user agents), `system` (daemons), `brew`, `tailscale`, and `process`. Homebrew services already present in launchd are shown once as launchd items with Homebrew ownership. `--unhealthy` includes warn, fail, and unknown items. `--port` accepts repeated flags or comma lists and matches any observed, declared, inferred, socket, or Tailscale front/backend port; multiple ports are OR, then AND with kind, owner, tag, and unhealthy.

`show` displays live state and the decoded plist as indented JSON. Environment values are always redacted. The old `--show-env` flag is accepted for compatibility but no longer reveals values. Authentication fields and recognized credentials stay redacted. `logs` reads the configured stdout/stderr files or registry log, keeps the last `-n` lines per file, and can follow appends, truncation, and rotation. Output is credential-redacted, including known job environment values. Large tails or lines above 4 MiB are refused. Missing logs return an error. Treat diagnostic output as private even after redaction.

`doctor` only reports. It exits 1 if any item fails, and 0 if there are no failures (warnings and unknown observations remain visible). Findings cover stopped KeepAlive jobs, possible crash loops, unsuccessful last termination, missing plists, registry orphans, stale scheduled logs, registered or local route target ports that are not listening, overlapping expected ports, and ports held by another job or an unattributed process. Crash-loop detection is a heuristic: at least five runs, no running PID, and an unsuccessful last termination. It does not measure recent restart frequency. A historical nonzero exit or signal remains a finding even after a job has restarted. A missing declared port is `fail` only for a KeepAlive or running job when listeners are visible; system jobs, socket-activated plists, and Tailscale front ports never fail for an invisible listener.

Last-run time is inferred from the latest log modification time, not supplied by launchd. Interval log staleness means older than twice the configured interval. Calendar log checks use a conservative nominal cadence (hourly, daily, weekly, monthly, or yearly), taking the longest cadence for multiple entries. Log activity can differ from actual successful runs. Reading failures are reported as unknown; unavailable collectors and parsing drift produce warnings. Dynamically submitted jobs can legitimately have no plist. Some protected metadata may be unavailable without additional permissions; macjobs never requests sudo.

## Local registry

Default location: `~/.config/macjobs/registry.yaml`. Resolve a different file with `--registry <file>`. Owners are free-form strings. The optional top-level `label_prefix` sets the managed-job naming prefix, defaulting to `local.macjobs.`. It must end in a dot; it does not confer ownership or bypass confirmation.

```yaml
schemaVersion: 1
label_prefix: local.macjobs.
entries:
  - id: com.example.worker
    kind: launchd
    owner: team
    purpose: Example background worker
    tags: [homelab]
    managed: false
    created_by_tool: false
    port: 8080
    ports: [8080, 9090]
    log: ~/Library/Logs/example-worker.log
  - id: example-process
    kind: process
    owner: unknown
    managed: false
    created_by_tool: false
    match:
      name: example-server
      port: 8080
```

Each entry requires a unique `id`. `kind` defaults to `launchd`; `owner` defaults to `unknown` when omitted or empty. Optional fields are `tags`, `created_by`, `template`, `adopted_at`, `purpose`, `port`, `ports`, `log`, and `match` (`name`, `regex`, `port`). `port` and each `ports` value must be 1–65535; `ports` is sorted and deduplicated on load/save. `name` matches the executable path or its basename; `regex` matches the executable path. All specified match conditions must agree, including the listening PID when combined with a port. Process command-line arguments are never collected. A port-only match selects a listening process. Empty or invalid matches, invalid ports, unknown fields, and unsupported schema versions are rejected. Registries without `ports` load unchanged. Older binaries reject the new YAML field because unknown fields are refused; upgrade readers before writing `ports`.

Launchd IDs are job labels. System IDs are `system:<label>`. Homebrew IDs use `homebrew.mxcl.<service>`. Node route IDs are `tailscale:<host:port><mount>` or `tailscale:tcp:<port>`; service endpoint IDs include the service name. Use the exact IDs from `list` when registering routes. Tailscale routes are always display-only; only local loopback proxy targets are probed for a listening port. The listening-port snapshot reflects what the current user can observe.

`registry init` discovers live non-Apple user jobs, with owner `unknown`, `managed: false`, and `created_by_tool: false`. It never hardcodes owners or labels. It refuses to overwrite an existing file without `--force`. `registry init --force` replaces all existing metadata, including tags. Writes use a private temporary file and an atomic publication step. `remove` drops the retired job's entry. Inspection stays read-only; creation provenance and managed status determine lifecycle confirmation requirements. Keep real registry files outside source control.

## Tags

```sh
macjobs tag com.example.worker homelab --dry-run --json
macjobs tag com.example.worker homelab
macjobs untag com.example.worker homelab --dry-run --json
macjobs untag com.example.worker homelab
macjobs list --tag homelab
macjobs list --tag a,b --kind launchd --owner team
macjobs list --tag a --tag b --unhealthy --json
macjobs create interval --name example-tagged --every 10m --tag homelab --dry-run -- /bin/echo example
macjobs adopt com.example.worker --owner team --tag homelab --dry-run
```

Preview state changes first and obtain approval before execution. Tags are registry metadata for launchd, system, Homebrew, Tailscale, and process IDs. Use an exact ID from `list`; tagging never changes services, plists, `managed`, or `created_by_tool`, and never bypasses lifecycle safety gates. Apple jobs (`com.apple.*`, including system-prefixed IDs) are refused.

CLI tags normalize to lowercase and must match `^[a-z0-9][a-z0-9._-]{0,31}$` (1–32 characters). Tags are deduplicated and sorted. Tag/untag accept separate positional tags; list/create/adopt `--tag` accepts repeated flags or comma lists. Multiple filter tags match ANY (OR), ANDed with kind, owner, port, and unhealthy. The TAGS column appears immediately after OWNER, joins tags with commas, and shows `-` when empty. JSON and YAML omit empty tags; schemaVersion stays 1.

Tagging an unregistered item creates only an unmanaged stub with its item kind, owner unknown, and both management/provenance flags false. Other entry metadata is preserved. Untagging a missing tag succeeds unchanged; it never creates an absent entry. Create retains its refusal of existing IDs, including metadata stubs; adoption merges tags. Transactions use the registry lock and atomic save. Dry-runs write nothing, including lock files, while inventory collection remains read-only.

Registry YAML tags must already be lowercase valid tokens. Registries without tags load unchanged. Older binaries reject the new YAML field because unknown fields are refused; upgrade readers before writing tags. `registry init --force` replaces all metadata, tags included.

## Ports

```sh
macjobs ports
macjobs ports --unattributed
macjobs ports --json
macjobs list --port 8080
macjobs list --port 8080,443 --tag homelab
```

`ports` lists every visible TCP listener and every unconnected UDP socket. Each row shows proto, port, host, PID, command, the attributed job (direct PID or a process-tree ancestor), `via` when the match walked `ppid`, and findings. `--unattributed` keeps only listeners with no known job. Tailscale routes expose the front port from the host or TCP key plus the loopback backend; frontends held by tailscaled are usually invisible to a user `lsof`.

Expected ports come from registry `port` / `ports` / `match.port` (`declared`), plist `Sockets` (`socket`), `ProgramArguments` flag heuristics (`inferred`), and Tailscale front/backend ports (`tailscale`). Observed listeners are attributed to the nearest item PID, then by walking parents from one `ps -axo pid=,ppid=,comm=` snapshot (depth at most 32, with a cycle guard). Unattributed listeners appear only in `ports` and as one doctor warning, never as new list items.

The PORTS column follows PID. Observed ports are comma-joined (`8080`, `5353/udp`); expected-but-missing ports are prefixed `!`. At most four values are shown, then `+N`. `-` means none. `list --port` matches observed or expected ports, including Tailscale front and backend ports.

A non-root `lsof` on macOS sees only the current user's processes. Root-owned listeners, launchd-held sockets, and Tailscale front ports are often invisible. macjobs never uses sudo. Collector command failures become warnings; missing visibility is `unknown`, never `fail`. Environment-derived ports are skipped because redaction can mask port numbers that also appear as environment values.

## JSON contract

`list --json` and `doctor --json` return `{ "schemaVersion": 1, "items": [], "warnings": [] }`. `ports --json` returns `{ "schemaVersion": 1, "listeners": [], "warnings": [] }`. `show` returns `{ "schemaVersion": 1, "item": {}, "plist": {}, "warnings": [] }`; `plist` is an empty object when unavailable. `registry init --json` returns the registry document. `logs --json` emits newline-delimited JSON records with `schemaVersion`, `id`, `path`, and `line`, one record per log line. Follow mode keeps the same record shape.

Item fields include `id`, `kind`, `owner`, `state`, `health`, `notes`, `managed`, `created_by_tool`, and `readOnly`. Optional observations include `tags` (omitted when empty), `label`, `domain`, `purpose`, `pid`, `schedule`, `lastRun` (UTC RFC 3339), `lastExit`, `signal`, `runs`, `plistPath`, `logs`, `port`, `ports`, `target`, `funnel`, and `disabled`. `port` is unchanged. Additive `ports` entries are `{proto, port, host?, pid?, source, listening, via?}` with `source` one of `declared`, `socket`, `inferred`, `tailscale`, or `observed`. Listener objects add `command`, `jobId`, and `expectedBy`. Missing `lastExit` means unknown or never exited; zero means an observed successful exit. Missing `lastRun` means no readable log timestamp. Health is `ok`, `warn`, `fail`, or `unknown`. Consumers should tolerate additional fields and unknown states. Collector warnings must be checked before interpreting an empty inventory as complete. schemaVersion stays 1.

## Safety

Inspection uses an explicit read-only Runner allowlist. Lifecycle operations use a separate Runner after ownership, domain, and process-ancestry checks. Every external command, including the editor, plist validation, and parent-PID queries, crosses this interface. Tests replay sanitized data and never call real service managers. The tool never invokes sudo or legacy launchctl load/unload.

For lifecycle operations, system daemons, system domains, Apple labels, and `/System` paths are refused, including dry-runs. Tailscale routes and selected processes support registry tags but remain display-only for lifecycle. Lifecycle targets must be in the current user's `gui/<uid>` domain. Files used for bootstrap, edit, or removal must be regular, matching-label plists in the user's `~/Library/LaunchAgents` or `/Library/LaunchAgents`; symbolic-link paths are refused.

For a job that is unregistered, `managed: false`, or `created_by_tool: false`, **stop, restart, disable, edit, and remove require both `--yes` and a TTY confirmation**: type the exact job label. `--yes` alone never authorizes these actions, and a non-interactive invocation refuses with a nonzero exit status and a structured JSON error. A registered job with both flags true needs no confirmation. Start and enable do not use the foreign-job confirmation gate. Keep provenance accurate: marking an existing job as created by the tool bypasses this protection.

Stop, restart, disable, edit/reload, and remove also refuse any job whose PID is the current process or an ancestor. The tool walks parent PIDs with `ps -o ppid=`, checks fresh live state before mutation, and checks again after editing. It fails closed if ancestry or live state cannot be verified. There is no override.

## Lifecycle usage

```sh
macjobs start com.example.worker --json
macjobs stop com.example.worker --yes
macjobs restart com.example.worker --dry-run --json
macjobs enable com.example.worker
macjobs disable com.example.worker --yes
EDITOR='example-editor --wait' macjobs edit com.example.worker --yes --json
macjobs remove com.example.worker --yes --json
macjobs restart homebrew.mxcl.example --yes
```

- `start` bootstraps an unloaded job from its plist, then uses `kickstart`.
- `stop` uses `bootout`: it unloads the job, preventing interval or KeepAlive relaunch until another start, restart, or enable. It does not set a disabled override. An already-unloaded job is unchanged.
- `restart` bootstraps first if unloaded, then uses `kickstart -k`.
- `disable` sets the disabled override, then boots out a loaded job. `enable` clears the override, then bootstraps if unloaded. Enable does not force an immediate run of an on-demand or interval job.
- `edit` opens a private temporary copy with `$EDITOR`, runs `plutil -lint`, verifies the label, and atomically replaces the original only when valid and changed. A changed plist is reloaded with bootout/bootstrap. Invalid, unchanged, or concurrently modified originals are retained; temporary copies are cleaned up. The original file mode is preserved. Editor arguments support quotes and escapes, without shell expansion. Use a waiting graphical editor or an editor script; subprocess output is suppressed to keep plist secrets out of terminal and JSON output. Editors that require direct terminal output are not supported. Reload failures report that the file was replaced so recovery can be explicit.
- Homebrew items, including deduplicated launchd items, delegate start, stop, and restart to `brew services`. Enable, disable, edit, and remove are refused for Homebrew services.

Every lifecycle command supports `--dry-run`. It skips confirmation and ancestry execution, reports operations without running them, and writes no files or registry data. Inventory collection is still read-only. Edit operations include conditions for the eventual valid, changed copy; temporary and trash paths are selected before execution.

## Create from templates

```sh
macjobs create interval --name example-worker --every 10m --owner team --purpose 'Example worker' -- /bin/echo example
macjobs create calendar --name example-weekly --at 'Sun 03:00' --at 'Wed 15:30' -- /bin/echo example
macjobs create keepalive --name example-service -- /bin/sleep 3600
macjobs create app-watchdog --name example-watchdog --app 'Example App' --every 10m
macjobs create interval --name example-preview --every 10m --dry-run --json -- /bin/echo example
macjobs remove local.macjobs.example-worker --purge-log
```

Templates use Go structures encoded with `howett.net/plist`. `interval` requires a positive whole-second `--every` duration. `calendar` accepts repeated `--at` values in `Day HH:MM` or daily `HH:MM` form, using local wall-clock time. `keepalive` sets `KeepAlive` and `RunAtLoad`. Commands after `--` require an absolute executable path; arguments are preserved without shell expansion. Schedule flags are restricted to their applicable templates.

Common flags are `--name`, `--owner`, `--purpose`, `--tag`, `--log`, `--dry-run`, and `--json`. The default log is `~/Library/Logs/macjobs/<name>.log`. Creation refuses an existing plist, registry entry, or loaded label. It writes a private plist into `~/Library/LaunchAgents/<prefix><name>.plist`, validates it with `plutil -lint`, records `created_by: macjobs`, `created_by_tool: true`, `managed: true`, `template`, and `log`, enables the user agent, and bootstraps it in the current user's GUI domain. Failed creation removes its plist and registry entry and restores a previously disabled override; incomplete rollback is reported explicitly. Metadata transactions use a private registry lock file and fail safely when busy.

Creation previews execute no external commands and write no files. They show resulting tags, a redacted XML plist, and operations in a schema-versioned report; live label collisions can only be checked during execution. Credentials in arguments are redacted in output; the private installed plist retains the supplied arguments. Human-readable creation and adoption output also uses indented JSON.

The watchdog template embeds the absolute path of the running macjobs executable, resolving an `mj` symlink at creation time. Homebrew Caskroom and Cellar installs prefer a stable `bin/macjobs` link that refers to the same file, so a later upgrade does not leave a pinned versioned path. Otherwise install the binary in a permanent location before creating a watchdog. It sets `StartInterval` from `--every` and runs once at load. Its default relaunch limit is ten minutes, independently of the check interval.

## Adopt an existing user agent

```sh
macjobs adopt com.example.worker --owner team --purpose 'Example worker' --dry-run
macjobs adopt com.example.worker --owner team --purpose 'Example worker'
```

Adoption sets `managed: true` and records `adopted_at` without changing the plist or loaded service. Existing metadata is retained unless explicitly replaced. `--tag` merges normalized tags with existing tags, and both creation and adoption validate tags before writing and show resulting tags in previews. Existing creation provenance is preserved; a foreign job stays `created_by_tool: false`. `--as-created` explicitly asserts tool-created provenance and bypasses later foreign-job confirmation, so use it only when that provenance is accurate. System items, Apple jobs, Homebrew services, and non-user plists are refused. Adoption previews read the user plist directly and execute no external commands.

## App watchdog checks

```sh
macjobs watchdog check --app 'Example App' --log ~/Library/Logs/example-watchdog.log
macjobs watchdog check --app 'Example App' --log ~/Library/Logs/example-watchdog.log --relaunch-interval 10m --state ~/.config/macjobs/state/example.json --json
```

The watchdog resolves `CFBundleExecutable` from `/Applications/<app>.app` or `~/Applications/<app>.app` and matches the main executable's complete path in a process snapshot. Helpers alone do not count as a running app. Apps without a discoverable bundle fall back to an exact process-name match. Unavailable or malformed process inspection never causes a reopen.

When the main process is absent, the watchdog invokes `open -a <app>`. A private state file at `~/.config/macjobs/state/<app>.json` records the last attempt before opening; failed attempts are also rate-limited. Atomic state writes and a process lock prevent overlapping checks from reopening repeatedly. A backwards clock suppresses attempts until the recorded interval has elapsed. The private `.lock` file remains beside the state file; the OS releases its lock when the check exits. Invalid state fails closed.

Connection status uses explicitly configured probes. With no probe, it reports `connected=unknown`, `signal=<liveness-method>+tcp-any`, and `reason=no-specific-probe`. Unrelated app/helper sockets can no longer produce `yes`.

The same probe flags work with `watchdog check` and `create app-watchdog`:

- `--tcp-peer relay.example.invalid:443` matches established numeric peers from the bundle's main process and helpers. Repeat it for alternative relays; DNS names resolve on each check, IPv6 uses `[address]:port`, and `*` matches any port. Loopback peers are excluded, including IPv4-mapped loopback addresses. This probe uses `lsof -nP`; it reports socket presence, which requires fresh app-level corroboration before the overall result can be `yes`.
- `--probe-log /absolute/path/example-app.log --connected-regex 'link=connected|heartbeat' --disconnected-regex 'link=disconnected'` reads the last MiB of a regular app log. The last matching record wins; a disconnected match takes precedence if both regexes match. Positive records must begin with an RFC 3339 timestamp (optionally bracketed), end with a newline, and be fresh. File modification times and unrelated writes never refresh connectivity. Missing, malformed, future-dated, or incomplete evidence is `unknown`; an expired positive record is `no`. The source must differ from watchdog output and state files, including file aliases.
- `--probe-http http://127.0.0.1:12345/status` reads a loopback status endpoint. Only HTTP with a literal loopback IP is allowed; credentials, redirects, and environment proxies are excluded. A successful response must contain a boolean `connected` and, for `true`, a fresh RFC 3339 `timestamp`: `{"connected":true,"timestamp":"2026-10-10T18:29:59Z"}`. A generic HTTP 200/health response cannot establish connectivity. Unreachable endpoints, non-2xx responses, and explicit `false` return `no`; invalid state returns `unknown`. Requests time out after five seconds and response bodies are bounded to 64 KiB.
- `--max-age 30s` controls positive evidence freshness. It defaults to `--interval` (ten minutes for manual checks); generated watchdogs pass their `--every` interval automatically. Set it to the app's actual heartbeat cadence, with a small allowance for delivery delay.

All configured probes must say `yes` for the overall result to be `yes`, and at least one must provide fresh app-level evidence (`log` or `http`). Any `no` wins over `unknown`; otherwise an unavailable probe yields `unknown`. A TCP-only positive remains `unknown` with `reason=tcp-peer-needs-freshness`, because half-open sockets can remain established after an outage. Probe configuration has no default relay, app-log path, or status URL.

```sh
macjobs create app-watchdog --name example-watchdog --app 'Example App' --every 10m \
  --tcp-peer relay.example.invalid:443 \
  --probe-log ~/Library/Logs/example-app.log \
  --connected-regex 'link=connected|heartbeat' --disconnected-regex 'link=disconnected' \
  --max-age 30s --dry-run
```

Choose evidence that describes the machine link itself. Startup/sign-in markers, daemon/supervisor liveness, encrypted connection configuration, and telemetry activity do not establish a live machine link. If an app exposes no suitable heartbeat/status signal, retain `unknown` until it does. A fresh positive log is evidence of the last heartbeat, not an instantaneous guarantee: disconnect detection depends on the app recording a disconnect or expiring its heartbeat. A ten-minute schedule can detect a recorded negative at its next check; heartbeat expiry can add up to `max-age` before that next check. Do not claim an end-to-end detection bound without measuring the app's signal and cadence.

`--restart-on-disconnect` is **off by default**. Explicit opt-in permits a clean AppleScript quit only for the configured app after `connected=no`, followed by `open -a` after its main process exits. It uses the same persisted attempt timestamp and lock as missing-app recovery, records attempts before quitting, and rate-limits failures too. A quit refusal, failed exit inspection, or five-second exit timeout prevents reopening; there is no forced termination. `unknown` never triggers disconnect recovery. Turning on this flag changes running-app behavior; generated plists include it only when requested.

Each normal check exits zero and appends exactly one local RFC 3339 timestamped line, retaining existing fields and adding the deciding `probe` and `reason`:

```text
2026-10-10T18:30:00-07:00 app="Example App" running=yes relaunched=no connected=unknown signal=bundle-executable+tcp-any reason=no-specific-probe probe=tcp-any
```

`--json` remains schemaVersion 1 and adds `probe` plus a `probes` array of `{probe, connected, reason}` results. Source log contents, endpoint responses, peer addresses, paths, and subprocess errors are never included in probe diagnostics. Checks with unavailable liveness report configured probes as `unknown` without querying them.

## Trash and restore

`remove` boots out a loaded job, moves its plist into `~/.config/macjobs/trash/<label>-<UTC timestamp>.plist`, and drops its registry entry. It never deletes the plist. `--purge-log` optionally deletes the unshared, absolute registry log of a tool-created job; foreign provenance, shared registry logs, and non-regular files are refused. Watchdog state is retained separately for rate limiting; remove its state and lock files explicitly when retiring a watchdog. Recovery files are not overwritten on a timestamp collision. If a later step fails, operations can be partially completed; inspect the JSON error and trash directory before retrying.

To restore, move the matching recovery file back to its original LaunchAgents location, re-add any desired registry metadata, then run `macjobs enable <label>` if disabled and `macjobs start <label>`. Retain the original creation provenance when restoring metadata. Restoring an unregistered job leaves it subject to the foreign-job confirmation gate. The trash holds the original plist, including any environment values, so treat it as private configuration.

## Action JSON

Lifecycle JSON uses `{ "schemaVersion": 1, "action": "restart", "id": "...", "label": "...", "kind": "launchd", "dryRun": true, "ops": [], "result": "planned", "error": null }`. Each operation has `command` and `args`, and conditional edit steps add `when`. File operation names are `copy`, `atomic-replace`, `remove-temp`, `mkdir`, `move`, and `registry-remove`. They describe Go file operations rather than shell commands. `move` uses a no-overwrite link/unlink operation on the same filesystem; a cross-filesystem failure retains the original.

Results are `planned`, `completed`, `unchanged`, `refused`, or `failed`. Errors contain stable `code` and safe `message` fields. Refusals and execution failures exit nonzero. JSON stdout remains a single document; confirmation prompts use stderr. Operations list the plan, including any steps not reached after a failure, rather than claiming each step succeeded. Raw subprocess output and errors are suppressed. Environment values are redacted in action output, including operation arguments.

launchctl print is an undocumented output format. Only direct service fields are parsed, unknown fields are tolerated, and launchctl list is the fallback for the user domain. XML and binary plists are supported. Tailscale node status and versioned service configuration are combined. Protect local metadata, redact diagnostic attachments, and report vulnerabilities privately as described in SECURITY.md.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development and fixture testing. Architecture and contracts are in [docs/design.md](docs/design.md). The maintained CLI skill is `.agents/skills/macjobs/SKILL.md`, exposed through relative directory symlinks at `.claude/skills/macjobs`, `.codex/skills/macjobs`, and `.cursor/skills/macjobs`. Refresh CLI documentation and the operator skill from binary help when commands or flags change; use a temporary candidate binary for uninstalled changes without replacing the installed executable.

Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md). MIT licensed — see [LICENSE](LICENSE).
