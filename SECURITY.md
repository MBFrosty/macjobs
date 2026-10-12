# Security

Report vulnerabilities through this repository's **private vulnerability reporting** (GitHub → Security → Report a vulnerability). Do not open a public issue for security-sensitive findings.

Include the affected version, a minimal reproduction, and the potential impact. Remove credentials and personal system information from attachments.

## Supported versions

Only the **latest minor release** receives security fixes. Upgrade to the current release before reporting issues on older versions.

## In scope

- Redaction guarantees for logs, environment variables, and command output
- The Runner command allowlist and sanitized replay fixtures in tests
- Ownership, TTY, and ancestor-PID gates for user-agent lifecycle changes
- Refusal to run as root or invoke `sudo`

## Out of scope

- Vulnerabilities in launchd, Homebrew, Tailscale, or other third-party tools macjobs inspects or invokes
- Misconfiguration of jobs on your machine outside macjobs' control

## Release binaries

Since v0.1.1, release builds are **signed with Developer ID and notarized by Apple**. A bare binary cannot be stapled, so the first launch needs internet for Gatekeeper to check notarization. Verify release artifacts using `checksums.txt` from the GitHub release before installing.

Registry files, recovery plists, and job logs may contain private metadata and should remain local.
