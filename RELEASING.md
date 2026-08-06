# Releasing

Releases are tag-driven and fully automated through GitHub Actions. Every
release artifact is a zip that a self-hosted Grafana user can unzip into their
plugins directory and run.

## How releases are cut

Releases happen automatically:

1. **Merge a PR to `main` that changes shipped code.** When CI succeeds on
   `main`, the [Version Bump & Tag workflow](.github/workflows/version-bump.yml)
   bumps the **patch** version in `package.json` and `pkg/honeycomb/client.go`,
   renames the CHANGELOG's `## [Unreleased]` heading to `## [X.Y.Z] — <date>`,
   commits `chore: release vX.Y.Z [skip ci]`, and pushes the annotated `vX.Y.Z`
   tag. (`src/plugin.json` uses `%VERSION%`/`%TODAY%` placeholders stamped by
   webpack at build time, so it needs no edit.)

   Merges that cannot change the artifact — CI config, `scripts/`, `tests/`,
   docs — **do not** cut a release. That decision lives in
   [`scripts/release-relevant.sh`](scripts/release-relevant.sh), which you can
   run yourself against any commit range:

   ```bash
   ./scripts/release-relevant.sh HEAD^1 HEAD   # exit 0 = would release, 1 = would skip
   ```

   Note that `README.md` and `CHANGELOG.md` *do* ship inside the zip but are
   still treated as skip-worthy; those edits ride along with the next code
   release rather than burning a version of their own.

2. **For a minor or major bump — or to force a release regardless of what
   changed** — trigger the workflow manually: GitHub → Actions →
   *Version Bump & Tag* → *Run workflow* → choose `minor`/`major`. A manual
   dispatch skips both the release-relevance check and the "last commit was a
   release" guard. Do this **before** merging further PRs, or the auto-patch
   will fire first.

3. **Pushing that tag starts the
   [Release workflow](.github/workflows/release.yml)**, which:
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

   A tag pushed by a human triggers exactly the same run, so manual tags work
   identically. This depends on the bump pushing with a PAT rather than
   `GITHUB_TOKEN` — see [`RELEASE_TOKEN`](#-required-the-release_token-secret)
   below.

If any step fails, no release is published. Fix the problem, then either re-run
the Release workflow against the existing tag (Actions → *Release* → *Run
workflow* → enter the tag), or delete the tag
(`git push --delete origin v0.2.0`) and re-run the bump workflow.

Add your entries under `## [Unreleased]` in every user-visible PR — since each
merge to `main` auto-releases, the changelog entry ships with the release it
describes, and [`scripts/promote-changelog.js`](scripts/promote-changelog.js)
renames that heading to the released version during the bump. If `[Unreleased]`
is empty, the release still happens; it just gets no changelog section.

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
| Go tests with `-race` + 65% coverage floor | CI + release | backend logic behaves; no data races |
| Frontend unit tests (Jest) | CI + release | query filtering/variable logic behaves |
| Typecheck + ESLint + golangci-lint | CI | no unsound types or common bug patterns |
| Packaging dry-run, all 6 platform binaries | CI (every PR) | a release from this commit would build and package |
| Container smoke test | CI + release | the exact artifact loads in real Grafana and the backend binary runs |
| Playwright e2e vs Grafana 11.0.0 & latest | CI | config + query editors work in a real browser across supported versions |
| Tag ↔ `package.json` guard | release | the release is built from the source that tag points at |
| Deterministic packaging | CI + release | re-packaging the same commit reproduces the same zip and SHA1 |

On that last row: `scripts/package.sh` stamps `info.updated` and every file mtime
from the commit date rather than the wall clock, and feeds `zip` a sorted file
list, so the published SHA1 is a property of the commit. This matters because
re-running the Release workflow against an existing tag is the documented
recovery path — without it, doing so would silently change the checksum users
were told to verify against. Reproducibility holds for a given commit *and*
toolchain version; it is not a claim of bit-identity across Go or webpack
upgrades.

## Repo settings

### Required status check

Require exactly one check on `main`: **`CI required`**.

That job (`ci-required` in [ci.yml](.github/workflows/ci.yml)) depends on every
other CI job and fails if any of them did not succeed. Requiring it rather than
the individual jobs is deliberate — job names drift, and matrix jobs produce one
check context per entry (`E2E (Grafana 11.0.0)`, `E2E (Grafana latest)`), so a
hand-maintained list silently stops guarding a new Grafana version and blocks
every PR forever when an old one is removed. Adding a job to that workflow's
`needs:` list covers it automatically.

### ⚠️ Required: the `RELEASE_TOKEN` secret

**Releases do not work without this.** The Version Bump & Tag workflow pushes the
release commit and tag **directly to `main`**, and the `main` ruleset requires
pull requests, so the push needs an identity that is on the ruleset's bypass list.

`GITHUB_TOKEN` cannot be that identity. Ruleset bypass actors may only be
org-owned GitHub Apps, and the first-party GitHub Actions app is not one:

```
$ gh api -X PUT repos/honeycombio/honeycomb-grafana-plugin/rulesets/19977291 \
    --input bypass-with-actions.json
422 Validation Failed
"Actor GitHub Actions integration must be part of the ruleset source or owner organization"
```

So the workflow authenticates with a PAT instead.

#### Who creates it

**It must be a user already on the ruleset bypass list** — currently org admins,
repo admins, `@McSick`, `@sumitabhattacharjee`, and the
`field-reliability-engineering` team (Settings → Rules → Rulesets → main → Bypass
list). Bypass is keyed to the token's *owner*, not to the token's permissions, so
no amount of scope on a non-bypassed user's token will get past `GH013`.

#### Exact permissions (fine-grained PAT)

github.com → Settings → Developer settings → Personal access tokens →
Fine-grained tokens → Generate new token:

| Field | Value |
|---|---|
| Resource owner | **honeycombio** |
| Repository access | Only select repositories → **honeycomb-grafana-plugin** |
| Repository permissions → **Contents** | **Read and write** |
| Repository permissions → Metadata | Read-only (GitHub selects this automatically) |

**That is the whole list.** `Contents: Read and write` is what grants `git push`,
and it covers both branches and tags — refs under `refs/tags/` need no separate
permission, which is the most common point of confusion here.

Deliberately *not* needed:

- **Workflows** — a PAT without it is rejected when a push touches
  `.github/workflows/`, but the bump commit only stages `package.json`,
  `pkg/honeycomb/client.go`, and `CHANGELOG.md`. Add it only if the bump ever
  starts editing workflow files.
- **Actions**, **Pull requests**, **Administration** — the bump neither reads
  workflow state, opens PRs, nor changes settings.
- Anything for the GitHub Release itself. The Release workflow publishes with the
  standard `GITHUB_TOKEN` and its own `contents: write`, which is unaffected.

Note that organizations can require admin approval for fine-grained tokens, and
can disable them entirely. If fine-grained is not available here, a **classic**
token works: scope **`public_repo`** is sufficient, since this repo is public —
`repo` grants more than is needed.

#### Then

1. Store it as a repository secret named **`RELEASE_TOKEN`**
   (Settings → Secrets and variables → Actions → New repository secret).
2. Note the expiry. When it lapses, releases stop — with exactly the failure
   signature below, which is why the workflow checks for the secret up front.

This is not hypothetical. It is exactly why the first run of this pipeline failed
and `v0.1.3` was never published:

```
remote: error: GH013: Repository rule violations found for refs/heads/main.
remote: - Changes must be made through a pull request.
 ! [remote rejected] main -> main (push declined due to repository rule violations)
```

**The failure mode is quiet**: the bump workflow goes red while CI stays green, so
nothing looks broken on the PR that triggered it. If releases stop appearing,
check the Version Bump & Tag run first — an expired or missing `RELEASE_TOKEN`
looks exactly like the error above.

Because a PAT is tied to a person, it is worth migrating to an org-owned GitHub
App (installed on the repo, added as a bypass actor, token minted with
`actions/create-github-app-token`) if this repo outlives its current owners.

### Why the tag push is what triggers a release

Using a PAT has a second effect that the design now depends on: events created
with a PAT trigger workflows, while `GITHUB_TOKEN`-created events are suppressed
to prevent recursion. So pushing the tag starts
[Release](.github/workflows/release.yml) on its own, and a bot tag and a
human-pushed tag follow one identical path.

An earlier version invoked the release workflow through `workflow_call`
specifically to work around the `GITHUB_TOKEN` suppression. That is no longer
needed, and keeping it would mean two full runs racing to publish the same tag.

### Other

- Enable Dependabot security updates. (Secret scanning and push protection are
  already on — default for public repos under the enterprise account.)
