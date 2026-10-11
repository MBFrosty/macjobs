# Releasing macjobs

Maintainer-facing notes for tagging and publishing releases.

## One-time setup

1. Create a public `homebrew-tap` repository under the GitHub org or user that owns macjobs (for example `MBFrosty/homebrew-tap`).
2. Create a fine-grained personal access token with **Contents** read/write on that tap repository only.
3. Add the token as the `HOMEBREW_TAP_TOKEN` Actions secret on the macjobs repository.

The release workflow references this secret; it is not required for CI snapshot builds.

## Pre-release checks

- CI is green on `ubuntu-latest` and `macos-latest`, including the GoReleaser check and snapshot job.
- `goreleaser check` passes locally (for example `go run github.com/goreleaser/goreleaser/v2@v2.18.3 check`).
- Optional: `goreleaser release --snapshot --clean --skip=publish` produces darwin arm64/amd64 archives and the Homebrew cask under `dist/`.
- CLI skills under the repository skill paths match `--help` output.
- Release notes or changelog entries are ready.

## Tagging

Create and push an **annotated** tag `vX.Y.Z` to `origin`. Pushing a `v*` tag triggers `.github/workflows/release.yml`, which runs tests and GoReleaser `release --clean` to publish GitHub release assets and update the tap cask (when `HOMEBREW_TAP_TOKEN` is set).

Do not push tags from automation in pull requests; tagging is a maintainer action after merge.

## Post-release verification

1. Confirm GitHub release assets and `checksums.txt`.
2. Confirm the cask update in `homebrew-tap` (`Casks/macjobs.rb`).
3. On macOS: `brew install <owner>/tap/macjobs`, then `macjobs --version`.
4. Optional: `go install github.com/MBFrosty/macjobs/cmd/macjobs@vX.Y.Z` and compare `--version`.

## Gatekeeper and quarantine (optional cask hook)

Release binaries are ad-hoc signed, not notarized. Users may see Gatekeeper or quarantine prompts until notarization is added; always verify downloads with `checksums.txt`.

The generated cask can run a macOS-only post-install step that clears the quarantine extended attribute on the installed binary (`xattr -dr com.apple.quarantine`). **This hook is off by default** in `.goreleaser.yaml` so maintainers can decide before the first public release.

**Enable:** in `.goreleaser.yaml` under `homebrew_casks`, uncomment the `hooks.post.install` block (see the comment above it) and extend `caveats` to mention that the cask clears quarantine on install.

**Disable:** leave the `hooks` block commented out (current default).

After toggling, run `goreleaser check` and a snapshot build to inspect `dist/homebrew/Casks/macjobs.rb` (expect a `postflight` stanza only when enabled).
