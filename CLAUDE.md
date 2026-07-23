# Claude Instructions — module-cinemeta

This repository is Mosaic's **default metadata provider** and its first
guarantee-clause **core module**
([ADR 0062](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0062-two-module-tiers.md)):
metadata and search are a required capability class
([ADR 0035](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0035-metadata-as-required-capability.md)),
so one provider must be present in every binary with no install step that can
fail and no configuration that can be omitted.

It is a client of one service — Cinemeta — not of the Stremio addon protocol.
`module-stremio-addons` is that, and stays that.

## What makes this module different, and what must stay true

- **Zero configuration, permanently.** No API key, no URL, no settings document,
  no `RoleSettingsUI`. A guarantee-clause module that can be configured is one
  that can be misconfigured. If a change here starts to add a setting, stop: the
  thing being asked for probably belongs in `module-stremio-addons`, where a
  user's own addons live.
- **The service address is a constant, not a field a deployment can reach.** The
  `base` field on `Client` exists so tests can point at an `httptest` server and
  for no other reason. Do not export it, do not read it from the environment.
- **No stream and no subtitle role.** Cinemeta describes content; it does not
  index it. An import here creates a Work and its tree with **no Parts**, and
  that is the shape rather than a gap.
- **Report the gaps, do not fill them by inventing.** Cinemeta has no clearart,
  no banners, no collections, no "similar", and no character names or headshots
  on its cast — [ADR 0034](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0034-rich-metadata-preview.md)'s
  recorded gaps. An empty field is how a consumer tells "the source has none"
  from "nobody asked". A TMDB- or Fanart-class provider closes them; this one
  does not pretend to.
- **Content is bound under `imdb`, not under `cinemeta`.** Cinemeta's ids *are*
  IMDb ids, and using the accurate scheme is what makes a title added here the
  same Work as one a Stremio addon added rather than a duplicate (ADR 0028).
  Changing this would silently double a library.

## The boundary is the point

- **Import only [`sdk`](https://github.com/mosaic-media/sdk) and the standard
  library.** `boundary_test.go` parses every import and fails on anything else.
  There is deliberately **no `sdui` exemption**: the Stremio module has one
  because it contributes a settings screen (ADR 0038), and this module has no
  settings.
- **It matters more for a core module than for an optional one.** A core module
  is compiled into the Platform binary and shares its dependency graph
  (ADR 0062), so a dependency added here is one the Platform and every other
  core module must resolve compatibly. The boundary is also what keeps the tier
  a *delivery* decision: this code could move out of process as a build change
  rather than a rewrite
  ([ADR 0064](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0064-extension-module-boundary.md)).
- **This module is an anti-corruption layer**
  ([ADR 0051](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0051-modules-as-anti-corruption-layers.md)).
  Every Cinemeta-ism stops in `cinemeta.go` and the Platform learns none of them.
- **It owns no schema** (ADR 0012): everything it writes goes through
  `ContentService`, acting as the `Caller` it was handed (ADR 0017).
- **MIT-licensed**, like Mosaic's other modules and unlike the Platform's AGPL
  ([ADR 0022](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0022-licensing.md)).

## Check the fake against the live service

The test suite is hermetic and the fake documents are trimmed copies of real
ones. **When you change what the client decodes, fetch the real document and
look at it**, rather than extending the fake from what the code expects:

```bash
curl -sSL https://v3-cinemeta.strem.io/meta/series/tt0903747.json | python3 -m json.tool | head -60
```

This has already caught a bug the fake hid. Cinemeta answers `200` for an id it
does not know, in **two** shapes — an unknown series returns `{}`, an unknown
*film* returns a meta echoing the id and type with no name — so the obvious
emptiness test passes and the Platform materialises a Work titled `tt99999999`.
The test now pins both shapes.

## Versioning and release

The Platform requires this at a **tagged version with no `replace`** — a
`replace` must never land in a commit. A change is a minor bump, tagged and
pushed, then the Platform's `go.mod` require is bumped to match.

```bash
git tag v0.1.0 && git push origin main && git push origin v0.1.0
```

The module reports the version that was **actually linked**, via
`v1.ModuleVersion` reading the build graph — not a hand-maintained constant,
which nothing forces to agree with anything.

## Everything runs in the container, nothing runs on the host

**Do not run `go build`, `go test`, `go vet` or `gofmt` directly on this
machine.** This repository's gates run inside its test container:

```bash
docker compose -f docker-compose.test.yml run --rm test
```

That runs gofmt, `go build ./...`, `go vet ./...` and `go test ./...` against the
Go version pinned in the compose file, which must stay equal to the one in
`go.mod`. Append `bash` for a shell in the same environment.

The container resolves the SDK from the proxy exactly as a consumer does, which
is what makes the boundary test mean what it claims: a host with a populated
module cache, a leftover `go.work` or a stray `replace` can satisfy an import a
third party's machine could not, and the test still passes because the import
resolved.

## Workflow

- Commit and push this repository **separately** from `platform`.
- **Commit author identity** must be `AdamNi-7080 <anicholls41@gmail.com>`.
- The test container green before pushing.
- Observability goes through the SDK's ambient `v1.Telemetry`
  ([ADR 0059](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0059-modules-observe-through-the-sdk.md)),
  reached as `TelemetryFrom(ctx)`. Do not print, and do not configure an
  exporter, a sink or retention — the Platform owns the observability plane.

## The roadmap and the decision records

These rules are identical in every Mosaic repository. They exist because the
state of the build and the reasons behind it are the two things that rot fastest
and report nothing when they do — no build fails, no test goes red.

### The roadmap is maintained, not consulted

**`docs/roadmap.md` in [`architecture`](https://github.com/mosaic-media/architecture)
is the single record of where the build is.** Read it before starting work, and
**update it in the same session as the change that dates it** — not in a
follow-up, which does not happen.

- **A slice that lands is marked landed, with what was left out.** "Built" with
  no qualifier is a claim that the whole slice shipped; if part of it did not,
  say which part and why in the same sentence.
- **Implementation that departs from the plan is recorded where it departed.**
  The roadmap is derived from the code, not from the intention that preceded it,
  and the surprises are the most valuable thing in it.
- **Do not restate the roadmap here.** A second copy of "what is built" in a
  `CLAUDE.md` is how the first copy goes stale unnoticed. This file carries how
  to work in *this* repository; the roadmap carries what has been done across all
  of them.
- **A capability with no client path is not done — it is
  [owed](https://github.com/mosaic-media/architecture/blob/main/docs/unreachable-capability.md).**
  If you delete or fail to build a client path to a working service, add its row
  to that register in the same change.

### Decision records are append-only

An ADR is an account of what was decided and why, at a time. It is evidence, not
documentation, and its value is that it was not edited afterwards.

- **Never rewrite a record's body to match what was built.** Not to correct it,
  not to annotate it, not to add "as built, this differs". That pattern turns a
  record into a running commentary and destroys the thing it is for.
- **State changes in the `**Status:**` line, and nowhere else.** That is where a
  record says it is built, built in part (naming the part), or superseded —
  wholly ("Superseded by ADR N") or partly ("Partly superseded: X was reversed by
  ADR N; the rest stands").
- **A changed decision needs a new record that supersedes it.** If the code
  deliberately does something a record decided against, that is a decision and it
  is written down as one, with its own Context / Decision / Alternatives /
  Consequences. Both records then stand: the old one keeps its reasoning, the new
  one carries the change.
- **An unbuilt decision is not a superseded one.** "We have not done this yet"
  belongs in the Status line and the roadmap. Only a genuine reversal earns a new
  record.
- **Records live only in `architecture/docs/adr/`**, numbered sequentially in
  kebab-case. Adding one means adding it to `nav:` in `mkdocs.yml`, and
  `mkdocs build --strict` must pass.

**If the code and a record disagree, say so rather than quietly picking one.** An
honest "this is unresolved" is worth more than a plausible reconciliation that
reads as settled.
