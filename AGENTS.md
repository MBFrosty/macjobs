# Contribution rules

- Keep system inspection read-only. User-agent lifecycle commands must pass the action safety gates and the lifecycle Runner allowlist. Never invoke sudo or mutate system services.
- Route every external command through the Runner interface. Tests use sanitized replay fixtures and never query the host's service managers.
- Keep all output free of secrets. Redact environment values and credentials, including logs, command arguments, and errors.
- Keep machine configuration outside the repository. Resolve the home directory and user ID at runtime; use neutral examples and free-form registry owners.
- Maintain the versioned JSON and registry contracts. Document meaningful behavior and run go vet ./..., go test ./..., and go build ./... before publishing.
- Use the existing global Git identity without overrides. Keep commits and review descriptions focused on the engineering change.
- No tracker references or personal details in commits, branches, or PRs.
- Whoever produces temporary output, scratch files, or worktrees, whether a person or automation, must delete them after merge or abandonment. Preserve uncommitted work and primary checkouts. Remove generated build data and other temporary artifacts after use.

- Any CLI change must update `.agents/skills/macjobs/SKILL.md`, `skills/macjobs-bot/SKILL.md`, and the operator's assistant `macjobs` skill in the same pass, refreshed from the installed binary's `--help` output.
