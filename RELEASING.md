# Releasing

Releases are tag-driven and fully automated through GitHub Actions. Every
release artifact is a zip that a self-hosted Grafana user can unzip into their
plugins directory and run.

## How releases are cut

Releases happen automatically:

1. **Merge a PR to `main`.** When CI succeeds on `main`, the
   [Version Bump & Tag workflow](.github/workflows/version-bump.yml) bumps the
   **patch** version in `package.json` and `pkg/honeycomb/client.go`, commits
   `chore: release vX.Y.Z [skip ci]`, and pushes the `vX.Y.Z` tag.
   (`src/plugin.json` uses `%VERSION%`/`%TODAY%` placeholders stamped by
   webpack at build time, so it needs no edit.)

2. **For a minor or major bump**, trigger the same workflow manually:
   GitHub → Actions → *Version Bump & Tag* → *Run workflow* → choose
   `minor`/`major`. Do this **before** merging further PRs, or the auto-patch
   will fire first.

3. **The tag triggers the [Release workflow](.github/workflows/release.yml):**
   - verifies the tag matches `package.json`
   - runs the full Go and frontend test suites
   - builds the frontend and backend binaries for all platforms
     (linux amd64/arm/arm64, darwin amd64/arm64, windows amd64)
   - packages the plugin with [`scripts/package.sh`](scripts/package.sh) —
     the same script CI dry-runs on every PR
   - **smoke tests the actual zip** in a real Grafana container
     ([`scripts/smoke-test.sh`](scripts/smoke-test.sh)): the plugin must load
     and the backend binary must start and answer a health check
   - publishes a GitHub Release with the zip, a SHA1 checksum, and
     auto-generated release notes

If any step fails, no release is published. Fix the problem, delete the tag
(`git push --delete origin v0.2.0`), and re-tag (or re-run the bump workflow).

Update `CHANGELOG.md` in every user-visible PR — since each merge to `main`
auto-releases, the changelog entry ships with the release it describes.

## What users get

Each release contains `honeycombio-honeycomb-datasource-<version>.zip`. To
install on self-hosted Grafana:

```bash
unzip honeycombio-honeycomb-datasource-<version>.zip -d /var/lib/grafana/plugins/
```

The plugin is unsigned, so it must be allowed in `grafana.ini`:

```ini
[plugins]
allow_loading_unsigned_plugins = honeycombio-honeycomb-datasource
```

(or `GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=honeycombio-honeycomb-datasource`),
then restart Grafana.

## Signing and the Grafana catalog (future)

Publishing to the [Grafana plugin catalog](https://grafana.com/developers/plugin-tools/publish-a-plugin/publish-a-plugin)
would give users one-command installs (`grafana-cli plugins install …`) and a
community signature, removing the unsigned-plugin step. That requires a plugin
submission review by Grafana Labs. Until then, GitHub Releases are the
distribution channel. The `npm run sign` script exists for when a Grafana
access policy token is available.

## Quality gates (why we trust a release)

This plugin is primarily AI-authored, so the pipeline is designed to make
correctness observable rather than assumed:

| Gate | Where | What it proves |
|------|-------|----------------|
| Go tests with `-race` + 60% coverage floor | CI + release | backend logic behaves; no data races |
| Frontend unit tests (Jest) | CI + release | query filtering/variable logic behaves |
| Typecheck + ESLint + golangci-lint | CI | no unsound types or common bug patterns |
| Packaging dry-run | CI (every PR) | a release from this commit would package |
| Container smoke test | CI + release | the exact artifact loads in real Grafana and the backend binary runs |
| Playwright e2e vs Grafana 11.0.0 & latest | CI | config + query editors work in a real browser across supported versions |
| Tag ↔ `package.json` guard | release | releases are reproducible from tagged source |

Recommended repo settings (configure on GitHub):
- Branch protection on `main`: require the `Backend`, `Frontend`,
  `Package & smoke test`, and `E2E` checks to pass; require PR review.
- Enable Dependabot security updates and GitHub secret scanning.
