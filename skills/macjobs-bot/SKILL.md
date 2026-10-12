---
name: macjobs-bot
description: Use when a user asks about, inspects, troubleshoots, or wants to change background jobs, launch agents, Homebrew services, Tailscale serve routes, or listening ports on a Mac with the macjobs CLI.
---

# macjobs (operator)

macjobs is a macOS CLI that inventories launchd user agents, system daemons (read-only), Homebrew services, Tailscale serve/funnel routes, registry-selected processes, and listening ports. Prefer it over ad hoc `launchctl`, `brew services`, or `lsof` when you need one consistent view, redacted output, and safe lifecycle previews.

## Check installation

```sh
macjobs --version
```

If missing, ask the human to install from the README: `brew install --cask MBFrosty/tap/macjobs`, `go install github.com/MBFrosty/macjobs/cmd/macjobs@latest`, or a built binary on `PATH`. Do not install or overwrite binaries unless the human explicitly asks.

Optional `mj` is a shell alias (`alias mj=macjobs` in `~/.zshrc`) or a symlink to the same executable, not a CLI alias.

## When to use macjobs

- **Inventory and health**: what is running, who owns it, tags, ports, schedules, last run, and doctor findings.
- **Logs**: tail job logs with credentials redacted.
- **Ports**: which TCP listeners and UDP sockets exist and which job owns them.
- **Controlled changes**: start/stop/restart, enable/disable, edit plists, create from templates, adopt, tag/untag, remove — always preview first.

Tailscale routes and registry-matched processes are display-only for lifecycle. Never use `sudo`.

## Safe read-only commands

Run these freely; they do not mutate jobs or registry files.

```sh
macjobs list
macjobs list --all
macjobs list --kind launchd --owner team --unhealthy
macjobs list --tag homelab
macjobs list --tag a,b --json
macjobs list --port 8080
macjobs list --port 8080,443 --kind launchd
macjobs ports
macjobs ports --unattributed --json
macjobs show com.example.worker --json
macjobs logs com.example.worker -n 100
macjobs logs com.example.worker -f
macjobs doctor --json
```

**list** filters: `--kind` (launchd, system, brew, tailscale, process), `--owner`, `--tag` (repeat or comma list; ANY tag matches, ANDed with other filters), `--port` (repeat or comma list; ANY observed or expected port), `--unhealthy`, plus global `--all` and `--json`.

**ports** lists visible TCP listeners and unconnected UDP sockets with job attribution. `--unattributed` keeps only listeners with no known job.

**show** returns live state and a redacted plist. `--show-env` is deprecated and never reveals values.

**logs** uses `-n/--lines` (default 50) and `-f/--follow`.

- `doctor` exits 1 when it has findings (any `fail` item), 0 otherwise. `logs --json` is NDJSON: one object per line, not one document. `list --kind` only accepts launchd/system/brew/tailscale/process and exits 1 otherwise. A foreign-job `--dry-run` of stop/restart/disable/edit/remove includes a `note` that the real run needs `--yes` plus typed TTY confirmation.

**doctor** reports only; it never fixes. It exits 1 when any item has `fail` health, 0 otherwise (warnings and unknown still print).

Global flags on most commands: `--json`, `--registry <file>` (default `~/.config/macjobs/registry.yaml`), `--all` (include Apple jobs on list-style inventory).

Use exact IDs from `list`: launchd labels, `system:<label>`, `homebrew.mxcl.<service>`, `tailscale:…` route IDs, and registered process IDs.

## Making changes

1. Run the same command with **`--dry-run`** (and `--json` when parsing). Show the human the planned operations.
2. Obtain **explicit human approval** before running without `--dry-run`.
3. Never **`remove`** without explicit confirmation of that specific job.
4. **Foreign jobs** (unregistered, `managed: false`, or `created_by_tool: false`) require **`--yes`** plus a **typed TTY confirmation** of the exact job label for stop, restart, disable, edit, and remove. You must not bypass, script, or fake this prompt — ask the human to run the confirmed command in their terminal if you cannot satisfy the TTY gate.
5. Start and enable do not use the foreign TTY gate but still need preview and approval.
6. **`registry init`** and **`watchdog check`** have no dry-run; they write files and may reopen apps. Get approval first.

Lifecycle examples (preview first):

```sh
macjobs restart com.example.worker --dry-run --json
macjobs tag com.example.worker homelab --dry-run --json
macjobs adopt com.example.worker --owner team --purpose 'Example worker' --tag homelab --dry-run
```

Homebrew services delegate start/stop/restart to `brew services`; enable, disable, edit, and remove are refused for brew items.

## JSON contract

- Always check **`schemaVersion`** (currently **1**) before parsing.
- Treat all fields beyond the documented core as optional; **ignore unknown fields**.
- **list** / **doctor**: `{ schemaVersion, items, warnings }`.
- **ports**: `{ schemaVersion, listeners, warnings }`.
- **show**: `{ schemaVersion, item, plist, warnings }`.
- Lifecycle actions: `{ schemaVersion, action, id, dryRun, ops, result, error, … }` with results `planned`, `completed`, `unchanged`, `refused`, or `failed`.
- **logs --json**: one JSON object per line with `schemaVersion`, `id`, `path`, `line`.

Item health is `ok`, `warn`, `fail`, or `unknown`. Times ending in **`Z`** are UTC. **`lastRun`** is inferred from log modification time, not launchd.

## Reading health and doctor findings

Common benign or contextual cases:

- **`fail` on an `application.*` registry orphan** may be stale app metadata after a restart — confirm live inventory before removing registry entries.
- **`system:org.cups.cupsd` not running** is normal when printing is idle.
- **Scheduled job with no log yet** may show `unknown` health before the first run.
- **Watchdog `connected=unknown`** means no fresh app-level probe evidence; it does not prove a disconnect.

Port-related doctor behavior:

- **Missing declared port** is **`fail`** only for a KeepAlive or **running** job when listeners are visible. System jobs, socket-activated plists, and Tailscale **front** ports never fail for an invisible listener.
- **Overlapping expected ports**, a port held by **another job** or an **unattributed PID**, and **missing inferred ports** are **`warn`**.
- A **Tailscale route to a dead local backend** remains **`fail`** and names the job that expected that backend when known.
- Doctor may warn that **N listeners have no known job** — inspect with `macjobs ports --unattributed`. Non-root `lsof` cannot see all sockets; macjobs never uses sudo.
- A launchd job whose **`ProgramArguments[0]` or `Program`** is an absolute path that does not exist is **`fail`** when the job is managed or adopted, and **`warn`** when it is foreign. The path is redacted. System jobs and Apple jobs skip this finding.

Check **`warnings`** in JSON before treating an empty inventory as complete.

## Secrets and privacy

- CLI output **redacts** credentials and sensitive environment values.
- Never paste secrets into chat, tickets, or logs — including partial matches.
- Do not try to “un-redact” or recover secrets from plists, env, or logs via other tools.
- Treat diagnostic output as private even after redaction.

## Reporting back to the human

When you finish an inspection or change:

1. State which commands you ran (or that the human ran for TTY-gated steps).
2. Summarize **health** and the top **doctor findings** (ok/warn/fail/unknown), including port conflicts if relevant.
3. Call out **warnings** from collectors (Homebrew/Tailscale/lsof unavailable, partial visibility).
4. For changes, show whether the result was **planned/completed/refused/failed** and any safe error `code`/`message` from JSON.
5. Propose next steps; do not run further lifecycle commands without approval.
