// Package cinemeta is Mosaic's default metadata provider: a client of
// Cinemeta, Stremio's public film and television metadata service, filling the
// metadata, search and catalog provider roles (sdk#2).
//
// It is a core module (architecture#3) under the guarantee clause. Metadata and
// search are a required capability class (platform#23) — a Mosaic that cannot
// identify or find content reads as broken rather than as unconfigured — so at
// least one provider must be present in every binary, with no install step that
// can fail and no configuration that can be omitted. This module is that
// provider. It needs no credential, no key and no addon URL: constructing it is
// all the configuration there is.
//
// A guarantee cannot be delegated to a module that is not guaranteed to be
// there, which is why this is core rather than an extension a deployment might
// not install.
//
// The tier is a delivery decision, not a contract decision. This module is
// shaped exactly like an extension module — its own Go repository importing
// only the published SDK and the standard library — and it does not know which
// tier it is in.
//
// It is not a second Stremio addon client. It is a direct client of one
// service, with no manifest negotiation, no addon list, no resource discovery
// and no ordering policy, none of which a fixed and known upstream needs. What
// it gives up in generality it gains in being unable to be misconfigured; the
// general addon client is a separate module.
//
// # What it provides
//
//   - RoleMetadata — descriptive detail for a ref: overview, genres, IMDb
//     rating, runtime, poster/backdrop/clearlogo, billed cast names, and for a
//     series a per-episode preview with stills and synopses.
//   - RoleSearch — free-text search over film and television. It is the half
//     that produces a ref at all: without it nothing could name the content the
//     metadata role answers for, which is why platform#23 makes the two one
//     capability class rather than two.
//   - RoleCatalog — Cinemeta's Popular and Featured collections for both types,
//     so a fresh install has rails to render rather than an empty home screen.
//
// It fills no stream or subtitle role, and it fills no settings-UI role.
// Cinemeta describes content and does not index or host it, so an import
// through this module materialises a Work and its season/episode tree with no
// Parts — the meta-only shape the Platform already supports. A deployment that
// wants something to play installs a stream source alongside it.
//
// # What it deliberately cannot do
//
// Cinemeta has no clearart or banner artwork, no franchise collections, no
// "similar titles", and no character names or headshots on its cast. Those are
// sdk#3's recorded gaps and they are structural to the source, not decoding
// this module skipped. A TMDB- or Fanart-class provider is what closes them;
// this module reports honestly empty fields rather than inventing.
//
// It owns no schema (platform#8): everything it writes goes through
// ContentService, acting as the Caller the Platform hands it (platform#13).
package cinemeta
