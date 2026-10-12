# Releasing macjobs

Maintainer-facing notes for tagging and publishing releases.

## One-time setup

1. Create a public `homebrew-tap` repository under the GitHub org or user that owns macjobs (for example `MBFrosty/homebrew-tap`).
2. Create a fine-grained personal access token with **Contents** read/write on that tap repository only.
3. Add the token as the `HOMEBREW_TAP_TOKEN` Actions secret on the macjobs repository.
4. Add the five signing and notarization secrets listed under [Signing and notarization](#signing-and-notarization).

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
3. On macOS: `brew install <owner>/tap/macjobs`, then `macjobs --version` and `spctl -a -vvv -t install "$(brew --prefix)/bin/macjobs"` (expect `source=Notarized Developer ID`).
4. Optional: `go install github.com/MBFrosty/macjobs/cmd/macjobs@vX.Y.Z` and compare `--version`.

## Signing and notarization

Since v0.1.1, release binaries are signed with a Developer ID Application certificate (hardened runtime) and notarized by Apple via the GoReleaser `notarize.macos` pipe. A bare Mach-O cannot be stapled, so the first launch needs internet for Gatekeeper to check notarization online.

Required Actions secrets (notarization is skipped unless all five are set):

- `MACOS_SIGN_P12`: base64 of the Developer ID Application `.p12` (export with `-legacy` if quill fails to decode it)
- `MACOS_SIGN_PASSWORD`: the `.p12` export password
- `MACOS_NOTARY_KEY`: base64 of the App Store Connect API key `.p8` (Developer access)
- `MACOS_NOTARY_KEY_ID`: the API key ID
- `MACOS_NOTARY_ISSUER_ID`: the App Store Connect issuer ID

Rotation: the Developer ID certificate is valid for 5 years; re-export and replace `MACOS_SIGN_P12`/`MACOS_SIGN_PASSWORD` before it expires. Revoke and replace the API key (and its three secrets) if it may have leaked.

The release workflow verifies each darwin binary with `codesign --verify --strict` and requires the Developer ID Authority chain and hardened runtime.

### Legacy quarantine hook (off)

The generated cask can run a macOS-only post-install step that clears the quarantine extended attribute (`xattr -dr com.apple.quarantine`). It is no longer needed and stays commented out in `.goreleaser.yaml`.

**Enable:** in `.goreleaser.yaml` under `homebrew_casks`, uncomment the `hooks.post.install` block (see the comment above it) and extend `caveats` to mention that the cask clears quarantine on install.

**Disable:** leave the `hooks` block commented out (current default).

After toggling, run `goreleaser check` and a snapshot build to inspect `dist/homebrew/Casks/macjobs.rb` (expect a `postflight` stanza only when enabled).
