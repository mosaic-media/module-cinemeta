# module-cinemeta

Mosaic's **default metadata provider**: a client of [Cinemeta](https://v3-cinemeta.strem.io/manifest.json),
Stremio's public film and television metadata service.

It fills three provider roles ([sdk#2](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0002-modules-as-typed-capability-providers.md)):

| Role | What it answers |
|---|---|
| `RoleMetadata` | Overview, genres, IMDb rating, runtime, poster/backdrop/clearlogo, billed cast names, and for a series a per-episode preview with stills and synopses |
| `RoleSearch` | Free-text search over film and television |
| `RoleCatalog` | Popular and Featured collections, for both types |

**It needs no configuration.** No API key, no addon URL, no settings document —
constructing it is all the setup there is.

## Why it exists

Metadata and search are a **required capability class**
([platform#23](https://github.com/mosaic-media/platform/blob/main/docs/adr/0023-metadata-as-required-capability.md)):
a Mosaic that cannot identify or find content reads as broken rather than as
unconfigured. [platform#3](https://github.com/mosaic-media/platform/blob/main/docs/adr/0003-platform-as-execution-kernel.md)
makes a provider for that class a **core module** under its guarantee clause —
compiled into the binary, first-party, with no install step that can fail.

Before this module, that guarantee was met by a Cinemeta addon URL bundled
*inside* `module-stremio-addons`: a default belonging to an extension module,
reached through a general addon protocol, sitting in a user-managed list that
could be emptied. [platform#23](https://github.com/mosaic-media/platform/blob/main/docs/adr/0023-metadata-as-required-capability.md) recorded that placement as unresolved in as many
words. A guarantee cannot be delegated to something that is not itself
guaranteed to be present.

So this is **not** a second Stremio addon client. It speaks the Stremio addon
protocol because that is what Cinemeta serves, but it talks to one known service
whose resources are fixed: no manifest to fetch, no resources to negotiate, no
addon list, no ordering policy. What it gives up in generality it gains in being
unable to be misconfigured — and `module-stremio-addons` remains the right place
for everything general.

## What it deliberately does not do

- **No streams, no subtitles.** Cinemeta describes content; it does not index or
  host it. An import through this module materialises a Work and its
  season/episode tree with **no Parts** — the meta-only shape the Platform
  already supports. A deployment that wants something to play installs a stream
  source alongside it.
- **No settings screen.** There is nothing to set, so there is no
  `RoleSettingsUI` ([sdk#4](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0004-module-contributed-settings-ui.md)).
- **No clearart, banners, collections, "similar", or cast headshots and
  character names.** These are
  [sdk#3](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0003-rich-metadata-preview.md)'s
  recorded gaps and they are structural to the source rather than decoding this
  module skipped. A TMDB- or Fanart-class provider is what closes them. This
  module reports honestly empty fields instead of inventing.

## Identity and dedup

Cinemeta's own identifiers **are** IMDb ids — its manifest declares the `tt`
prefix and nothing else — so a Work is bound under the `imdb` scheme rather than
under a name of this module's own. That is not a convenience: it is the accurate
name for what the id is, and it is what makes a title added here *the same Work*
as one a Stremio addon would have added, rather than a duplicate
([platform#18](https://github.com/mosaic-media/platform/blob/main/docs/adr/0018-virtual-and-materialized-content.md)).

## The boundary is the point

This module imports only the published [`sdk`](https://github.com/mosaic-media/sdk)
and the standard library, enforced by `boundary_test.go` parsing every import.
Being a core module is a **delivery** decision, not a contract one
([platform#3](https://github.com/mosaic-media/platform/blob/main/docs/adr/0003-platform-as-execution-kernel.md)):
the code is shaped exactly as a third party's would be, does not know which tier
it is in, and could move out of process as a build change rather than a rewrite
([platform#39](https://github.com/mosaic-media/platform/blob/main/docs/adr/0039-extension-module-boundary.md)).

Everything Cinemeta-shaped stops in `cinemeta.go`
([module-stremio-addons#2](https://github.com/mosaic-media/module-stremio-addons/blob/main/docs/adr/0002-modules-as-anti-corruption-layers.md)):
a rating as a string, a year range with an en dash, a cast hidden in a
categorised links array, episodes numbered under either `episode` or `number`,
genres under either `genres` or `genre`, and posters served at a thumbnail size
that is upgraded to the one Mosaic renders at.

## Build and test

**Everything runs in a container; nothing is built or tested on the host.**

```bash
docker compose -f docker-compose.test.yml run --rm test
```

That runs gofmt, `go build ./...`, `go vet ./...` and `go test ./...` against a
pinned toolchain. The tests are hermetic — the fake Cinemeta is an `httptest`
server — so they need no network beyond resolving the SDK.

**The fake is checked against the live service by hand, and that has already
paid.** Cinemeta answers `200` for an id it does not know, in two different
shapes: an unknown series returns an empty document, and an unknown *film*
returns a meta echoing the id and the type with no name. Testing emptiness the
obvious way passed the fake and would have materialised a library Work titled
`tt99999999`.

## Status

Built: the three roles, the import path for films and series, and the hermetic
test suite. Verified against the live service by hand — search, both metadata
shapes, all four catalogs, paging, and the unknown-id shapes above.

MIT-licensed ([platform#1](https://github.com/mosaic-media/platform/blob/main/docs/adr/0001-transactional-store-extensibility.md)),
like Mosaic's other modules and unlike the Platform's AGPL.
