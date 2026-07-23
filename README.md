# module-cinemeta

Mosaic's **default metadata provider**: a client of [Cinemeta](https://v3-cinemeta.strem.io/manifest.json),
Stremio's public film and television metadata service.

It fills three provider roles ([ADR 0027](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0027-modules-as-typed-capability-providers.md)):

| Role | What it answers |
|---|---|
| `RoleMetadata` | Overview, genres, IMDb rating, runtime, poster/backdrop/clearlogo, billed cast names, and for a series a per-episode preview with stills and synopses |
| `RoleSearch` | Free-text search over film and television |
| `RoleCatalog` | Popular and Featured collections, for both types |

**It needs no configuration.** No API key, no addon URL, no settings document —
constructing it is all the setup there is.

## Why it exists

Metadata and search are a **required capability class**
([ADR 0035](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0035-metadata-as-required-capability.md)):
a Mosaic that cannot identify or find content reads as broken rather than as
unconfigured. [ADR 0062](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0062-two-module-tiers.md)
makes a provider for that class a **core module** under its guarantee clause —
compiled into the binary, first-party, with no install step that can fail.

Before this module, that guarantee was met by a Cinemeta addon URL bundled
*inside* `module-stremio-addons`: a default belonging to an extension module,
reached through a general addon protocol, sitting in a user-managed list that
could be emptied. ADR 0035 recorded that placement as unresolved in as many
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
  `RoleSettingsUI` ([ADR 0038](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0038-module-contributed-settings-ui.md)).
- **No clearart, banners, collections, "similar", or cast headshots and
  character names.** These are
  [ADR 0034](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0034-rich-metadata-preview.md)'s
  recorded gaps and they are structural to the source rather than decoding this
  module skipped. A TMDB- or Fanart-class provider is what closes them. This
  module reports honestly empty fields instead of inventing.

## Identity and dedup

Cinemeta's own identifiers **are** IMDb ids — its manifest declares the `tt`
prefix and nothing else — so a Work is bound under the `imdb` scheme rather than
under a name of this module's own. That is not a convenience: it is the accurate
name for what the id is, and it is what makes a title added here *the same Work*
as one a Stremio addon would have added, rather than a duplicate
([ADR 0028](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0028-virtual-and-materialized-content.md)).

## The boundary is the point

This module imports only the published [`sdk`](https://github.com/mosaic-media/sdk)
and the standard library, enforced by `boundary_test.go` parsing every import.
Being a core module is a **delivery** decision, not a contract one
([ADR 0062](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0062-two-module-tiers.md)):
the code is shaped exactly as a third party's would be, does not know which tier
it is in, and could move out of process as a build change rather than a rewrite
([ADR 0064](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0064-extension-module-boundary.md)).

Everything Cinemeta-shaped stops in `cinemeta.go`
([ADR 0051](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0051-modules-as-anti-corruption-layers.md)):
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

MIT-licensed ([ADR 0022](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0022-licensing.md)),
like Mosaic's other modules and unlike the Platform's AGPL.
