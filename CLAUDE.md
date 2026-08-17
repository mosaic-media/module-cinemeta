# Claude Instructions — module-cinemeta

Fleet-wide conventions — commits, decision records, citation form, the roadmap —
are in [`architecture`](https://github.com/mosaic-media/architecture/blob/main/CLAUDE.md).
This file is what is specific to `module-cinemeta`.

This repository is Mosaic's **zero-configuration metadata floor**: a client of
one fixed service, Cinemeta, with no key to obtain, no URL to point anywhere and
nothing to set. It is not a general Stremio addon client — that is
[`module-stremio-addons`](https://github.com/mosaic-media/module-stremio-addons)
— and it must not become one: there is no manifest to negotiate here, no addon
list and no ordering policy.

## It must never acquire settings

This is the rule the rest of the repository is arranged around. A provider that
can be configured is one that can be misconfigured, and this module's whole value
is that it cannot be.

- **`New` takes no settings document** and builds its client once, rather than
  per invocation. Every role receives a settings document and this module ignores
  it; nothing outside a test reads `req.Settings`.
- **`apiBase` is a constant.** `Client.base` is a field *only* so a test can point
  at an `httptest` server — that is why the capability tests are in-package.
  Never export it, never read it from the environment, never surface it in a
  settings document.
- **No `RoleSettingsUI`, and therefore no `contracts` import.** `boundary_test.go`
  has no SDUI exemption; if you find yourself adding one, a setting has arrived
  and the change belongs somewhere else.
- **No bundled credential either.** Mosaic ships project credentials in official
  builds ([architecture#4](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0004-project-credentials-in-official-builds.md)),
  and that decision deliberately left this module as the zero-configuration
  floor: a shared key can be revoked or throttled, so a guarantee resting on one
  is not a guarantee. The build-time credential pattern is the one thing not to
  copy here.

## What it declares

`Capability.Manifest` in `capability.go` declares `RoleMetadata`, `RoleSearch`
and `RoleCatalog`. The three it does not are as deliberate:
`TestManifestDeclaresTheRolesItImplements` asserts the exact count *and* that the
type does not satisfy `StreamProvider` or `SettingsUIProvider`. Keep it.

- **No stream or subtitle role.** Cinemeta describes content; it does not index
  it. An import creates a Work and its season/episode tree with **no Parts**, and
  that is the shape rather than a gap.
- **Metadata and search are one class, not two.** Search is what produces a ref
  at all — a deployment with only the metadata role could describe content it had
  no way to name.
- **Report the gaps, do not fill them by inventing.** Cinemeta has no clearart,
  banners, collections, "similar", or character names and headshots on its cast.
  An empty field is how a consumer tells "the source has none" from "nobody
  asked".
- `Import` rejects a ref with no native type and id rather than materialising
  something nameless.

## The boundary

`boundary_test.go` parses every non-test import and allows only the standard
library and `sdk/…`; a Platform import or a third-party one fails. **It reads
this directory only** — sound while every source file is here, and not sound the
moment a subdirectory is added, which means widening it in the same change.

It matters more here than in an optional module: this one is compiled into the
Platform binary and shares its dependency graph, so a dependency added here is
one the Platform and every other core module must resolve compatibly. Read
`go.mod`'s indirect requires for what that currently costs.

## Cinemeta's dialect stops in `cinemeta.go`

The Platform must learn none of it. What is already handled there, and must stay
handled there: a rating arriving as a string, a year range written with an en
dash, a cast hidden in a categorised links array, episodes numbered under either
`episode` or `number`, genres under either `genres` or `genre`, and a poster
served at a thumbnail size.

- **`posterURL` rewrites only the exact metahub prefix.** Cinemeta also serves
  posters from other hosts, and a substitution applied to those produces a link
  to nothing — worse than a soft image and harder to notice.
- **Cinemeta answers `200` for an id it does not know, in two shapes** — an
  unknown series returns no meta, an unknown *film* returns a meta echoing the id
  and type with no name. `Meta` therefore tests the **name**, not the presence of
  an id. Treating "has an id" as "exists" materialises a Work titled `tt99999999`.
  `TestMetadataFailsForAnIdCinemetaDoesNotKnow` pins both shapes, against
  `Metadata` and `Import` alike; assume a third exists until you have looked.
- **The catalog set is fixed in `Catalogs()`, not fetched**; half of what Cinemeta
  declares takes an argument a browse surface addressing a catalog by id cannot
  supply. Only the *genre options* are read from the manifest, cached in
  `facets.go`, and that one read does not make this a client of the addon
  protocol.
- **A filter this module cannot honour is refused, not dropped.** The addon
  protocol answers an unknown genre with the unfiltered listing, so passing one
  through returns a plausible page for a question nobody asked.
- **The User-Agent is set for reachability, not courtesy** — Cloudflare-fronted
  addons answer Go's default `Go-http-client/1.1` with a 403, which reads exactly
  like the service being down. No test asserts it.

**Content is bound under `imdb`** (`providerScheme`), not under a name of this
module's own: Cinemeta's ids *are* IMDb ids, and that scheme is what makes a
title added here the same Work another IMDb-keyed source added rather than a
duplicate. Changing it doubles a library. It is also the *only* id written —
`externalIDs` marshals a single-key document — so a title sourced here has
nothing for a provider keying television on a TVDB id to match against. That is a
limit of the floor, not a defect to close by inventing an id.

## The gate

```bash
docker compose -f docker-compose.test.yml run --rm test
```

That is the record-index check, the citation lint, gofmt, `go build`, `go vet`
and `go test`, against the Go version pinned in the compose file — keep that
version equal to `go.mod`'s. Append `bash` for a shell in the same environment.
`.github/workflows/verify.yml` runs the same checks on a `setup-go` runner and is
what refuses a push; keep the two in step. Do not run any of them on the host: a
populated module cache, a leftover `go.work` or a stray `replace` can satisfy an
import a third party's machine could not, and `boundary_test.go` passes anyway
because the import resolved.

**The tests are hermetic** — a fake Cinemeta over `httptest` and an in-memory
`ContentService`. The fake's documents are trimmed copies of real ones, so **when
you change what the client decodes, fetch the real document and read it** rather
than extending the fake from what the code expects.

## Release

A change is a minor bump, tagged and pushed; **a `replace` must never land in a
commit**, and the version is read from the build graph by `v1.ModuleVersion`
rather than held in a constant. `release.yml` reuses `verify.yml`, checks the tag
is semver and that `go.mod`'s path matches the repository, then proves a consumer
can resolve it through the public proxy. Only then does `dispatch` fire, sending
`core-module-release` **and** `graph-check` to `mosaic-media/platform`; both
steps fail rather than warn when `PLATFORM_DISPATCH_TOKEN` is unset, because the
release is already complete by then and a red run is how a broken bump chain
becomes visible.

## Records, licence, observability

[`docs/adr/README.md`](docs/adr/README.md) is the generated index of the records
this repository owns; read it rather than counting files, and never hand-edit it.
`scripts/adr_index.py` and `scripts/adr_lint.py` are **vendored** from
`architecture/scripts/` and run by this gate — change them there and re-vendor.

MIT-licensed; files carry **no SPDX header**. Observability goes through
`v1.TelemetryFrom(ctx)`; do not print, and do not configure an exporter, a sink
or retention.
