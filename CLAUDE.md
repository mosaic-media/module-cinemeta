# Claude Instructions — module-cinemeta

Mosaic's **default metadata provider**: a client of one service, Cinemeta,
filling the metadata, search and catalog roles with no credential of any kind.

`module-stremio-addons` is the client of the Stremio *addon protocol*, and stays
that. This module is not that, and must not become it.

It is a **core module** under the guarantee clause of
[architecture#3](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0003-two-module-tiers.md):
metadata and search are a required capability class
([platform#23](https://github.com/mosaic-media/platform/blob/main/docs/adr/0023-metadata-as-required-capability.md)),
so one provider must be present in every binary with no install step that can
fail and no configuration that can be omitted. Why *this* module is the one that
answers for it is [module-cinemeta#1](docs/adr/0001-the-guaranteed-metadata-provider-needs-no-credential.md).

## What makes this module different, and what must stay true

- **Zero configuration, permanently.** No API key, no URL, no settings document,
  no `RoleSettingsUI`. A guarantee-clause module that can be configured is one
  that can be misconfigured. If a change here starts to add a setting, stop: the
  thing being asked for probably belongs in `module-stremio-addons`, where a
  user's own addons live.

  **This survived a decision that went the other way for everybody else.** Mosaic
  now does ship project credentials in official builds
  ([architecture#4](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0004-project-credentials-in-official-builds.md)),
  which reversed the Mosaic-held-key alternative
  [module-cinemeta#1](docs/adr/0001-the-guaranteed-metadata-provider-needs-no-credential.md)
  rejected — and it explicitly left this module as the zero-configuration floor,
  because a shared credential can be revoked or throttled and a guarantee resting
  on one is not a guarantee. **A bundled key here would dissolve the floor**, so
  the pattern the other modules follow is the one thing not to copy into this
  repository.
- **The service address is a constant, not a field a deployment can reach.** The
  `base` field on `Client` exists so tests can point at an `httptest` server and
  for no other reason. Do not export it, do not read it from the environment.
- **No stream and no subtitle role.** Cinemeta describes content; it does not
  index it. An import here creates a Work and its tree with **no Parts**, and
  that is the shape rather than a gap.
- **Report the gaps, do not fill them by inventing.** Cinemeta has no clearart,
  no banners, no collections, no "similar", and no character names or headshots
  on its cast —
  [sdk#3](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0003-rich-metadata-preview.md)'s
  recorded gaps. An empty field is how a consumer tells "the source has none"
  from "nobody asked". A TMDB- or Fanart-class provider closes them; this one
  does not pretend to.
- **Content is bound under `imdb`, not under `cinemeta`.** Cinemeta's ids *are*
  IMDb ids, so `providerScheme = "imdb"` is the accurate scheme, and it is what
  makes a title added here the same Work as one a Stremio addon added rather than
  a duplicate
  ([platform#18](https://github.com/mosaic-media/platform/blob/main/docs/adr/0018-virtual-and-materialized-content.md)).
  Changing this would silently double a library.

  Know the cost it carries: binding only `imdb` is why a series imported through
  this module cannot be enriched by a provider that keys television on a TVDB id.
  That is a real limit of the floor, not a defect to fix by inventing an id.

## The boundary is the point

- **Import only [`sdk`](https://github.com/mosaic-media/sdk) and the standard
  library.** `boundary_test.go` parses every import and fails on anything else.
  There is deliberately **no `contracts` exemption**: a module gets one to author
  its own settings screen
  ([sdk#4](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0004-module-contributed-settings-ui.md)),
  and this module has no settings, so it has no reason to reach the UI contract
  at all. Adding that import means a setting arrived — see the first rule above.
- **It matters more for a core module than for an optional one.** A core module
  is compiled into the Platform binary and shares its dependency graph, so a
  dependency added here is one the Platform and every other core module must
  resolve compatibly. The boundary is also what keeps the tier a *delivery*
  decision: this code could move out of process as a build change rather than a
  rewrite
  ([platform#39](https://github.com/mosaic-media/platform/blob/main/docs/adr/0039-extension-module-boundary.md)).
- **"Only the SDK" is only as good as the SDK's own graph.** Check `go.mod`'s
  indirect requires rather than assuming the transitive cost is zero;
  [sdk#10](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0010-the-sdk-carries-no-implementation.md)
  is the decision aimed at that, and its Status says where it has got to. Read
  the Status; do not restate the decision here as though it were built.
- **This module is an anti-corruption layer**
  ([module-stremio-addons#2](https://github.com/mosaic-media/module-stremio-addons/blob/main/docs/adr/0002-modules-as-anti-corruption-layers.md)).
  Every Cinemeta-ism stops in `cinemeta.go` and the Platform learns none of them.
- **It owns no schema**: everything it writes goes through `ContentService`,
  acting as the `Caller` it was handed.

## Check the fake against the live service

The test suite is hermetic and the fake documents are trimmed copies of real
ones. **When you change what the client decodes, fetch the real document and look
at it**, rather than extending the fake from what the code expects:

```bash
curl -sSL https://v3-cinemeta.strem.io/meta/series/tt0903747.json | python3 -m json.tool | head -60
```

This has already caught a bug the fake hid. **Cinemeta answers `200` for an id it
does not know, in two different shapes** — an unknown series returns no meta at
all, an unknown *film* returns a meta echoing the id and type back with no name.
So the obvious emptiness test passes on one shape and not the other, and the
consequence downstream is a library Work titled `tt99999999`. The test pins both
shapes; keep it that way, and assume a third shape exists until you have looked.

## Everything runs in the container, nothing runs on the host

**Do not run `go build`, `go test`, `go vet` or `gofmt` directly on this
machine.**

```bash
docker compose -f docker-compose.test.yml run --rm test
```

That runs gofmt, `go build ./...`, `go vet ./...` and `go test ./...` against the
Go version pinned in the compose file, which must stay equal to `go.mod`'s.
Append `bash` for a shell in the same environment.

The container resolves the SDK from the proxy exactly as a consumer does, which
is what makes the boundary test mean what it claims: a host with a populated
module cache, a leftover `go.work` or a stray `replace` can satisfy an import a
third party's machine could not, and the test still passes because the import
resolved.

## Versioning and release

A change is a **minor bump**, tagged and pushed. Consumers then move their own
`require`; **a `replace` must never land in a commit.**

```bash
git tag v0.1.0 && git push origin main && git push origin v0.1.0
```

The module reports the version that was **actually linked**, via
`v1.ModuleVersion` reading the build graph — not a hand-maintained constant,
which nothing forces to agree with anything.

## Decision records

This repository owns the records whose mechanism it holds. They are in
[`docs/adr/`](docs/adr/), and **[`docs/adr/README.md`](docs/adr/README.md) is a
generated index — read it first, and do not hand-edit it.** It also lists the
records held elsewhere that this repository's decisions depend on.

## Workflow

- Observability goes through the SDK's ambient `v1.Telemetry`
  ([sdk#5](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0005-modules-observe-through-the-sdk.md)),
  reached as `TelemetryFrom(ctx)`. Do not print, and do not configure an
  exporter, a sink or retention — the Platform owns the observability plane.
- **MIT-licensed**
  ([architecture#1](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0001-licensing.md)).

<!-- shared-rules:begin -->
<!-- shared-rules:end -->
