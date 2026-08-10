# The guaranteed metadata provider needs no credential

**Status:** Built. `module-cinemeta` v0.1.0 is published and registered in the
Platform's composition root, and `module-stremio-addons` v0.20.0 has dropped its
bundled Cinemeta default. Verified live: a fresh session's home screen renders
Cinemeta's catalogs and search returns its results with no configuration.
Resolves the question [platform#23](https://github.com/mosaic-media/platform/blob/main/docs/adr/0023-metadata-as-required-capability.md)'s
Status line records as open. Refines
[platform#3](https://github.com/mosaic-media/platform/blob/main/docs/adr/0003-platform-as-execution-kernel.md)'s guarantee clause. **Partly superseded:
the "ship a Mosaic-held TMDB key" alternative rejected below was reversed by
[supervisor#1](https://github.com/mosaic-media/supervisor/blob/main/docs/adr/0001-supervisor-as-host-manager.md); the rest of this record stands,
and `module-cinemeta` remains the core zero-configuration floor.**
**Date:** 2026-07-23

## Context

[platform#23](https://github.com/mosaic-media/platform/blob/main/docs/adr/0023-metadata-as-required-capability.md) makes metadata and search a
required capability class: a Mosaic that cannot identify or find content is not
a degraded Mosaic, it is inert. [platform#3](https://github.com/mosaic-media/platform/blob/main/docs/adr/0003-platform-as-execution-kernel.md) makes a
provider for that class a **core module** under its guarantee clause — present
in every binary, "with no install step that can fail."

`module-tmdb` was built as that provider, and it is the right module for what it
does. It closes two of [sdk#3](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0003-rich-metadata-preview.md)'s recorded gaps
that no addon protocol can carry, and it made [platform#23](https://github.com/mosaic-media/platform/blob/main/docs/adr/0023-metadata-as-required-capability.md)'s composition-root check
real. But it exposed a gap in the guarantee clause itself, which its own record
of what it did not do states plainly: **TMDB has no anonymous access.** The check
passes because a provider is *registered*; a user still sees nothing until they
paste an API key. The requirement is met in letter and not in spirit, and no
TMDB-based module can meet it in spirit.

"No install step that can fail" and "no configuration that can be omitted" are
not the same property, and [platform#3](https://github.com/mosaic-media/platform/blob/main/docs/adr/0003-platform-as-execution-kernel.md) only asked for the first. The distinction
did not matter while the guarantee was being met by an addon URL bundled inside
`module-stremio-addons`, because that URL needed nothing from the user either.
It matters now, and it is what the two clauses were reaching for: a fresh
install that works.

Three things are tangled together and are separated here.

- **Where the default lives.** [platform#23](https://github.com/mosaic-media/platform/blob/main/docs/adr/0023-metadata-as-required-capability.md) specified a Platform bootstrap seed; the
  code put it inside an extension module; [platform#3](https://github.com/mosaic-media/platform/blob/main/docs/adr/0003-platform-as-execution-kernel.md) said a metadata provider is
  core. All three are on record and they disagree.
- **What the guarantee actually requires of a provider.** Registration, or
  usability without configuration.
- **What happens to the bundled default.** Removing it was correctly declined
  while TMDB was the only core metadata module, because it would have left a
  fresh install with no addons, no key and no metadata at all — strictly worse
  than the arrangement being replaced.

## Decision

**The guarantee clause requires a provider that works with no credential and no
configuration. Mosaic ships one: `module-cinemeta`, a core module that is a
direct client of Cinemeta and has no settings at all.**

Four things follow, and each is a choice rather than a detail.

**It is a client of one service, not a second addon client.** Cinemeta serves the
Stremio addon protocol, and this module speaks enough of it to talk to Cinemeta —
but it fetches no manifest, negotiates no resources, holds no addon list and
applies no ordering policy, because a fixed known upstream needs none of them.
The general client is `module-stremio-addons` and stays there. What this module
gives up in generality it gains in being **unable to be misconfigured**, which is
the property the guarantee is actually about.

**It has no settings, and that is load-bearing rather than incidental.** No API
key, no URL, no list, and no `RoleSettingsUI`
([sdk#4](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0004-module-contributed-settings-ui.md)) — there is nothing to
render. A guarantee-clause module that can be configured is one that can be
misconfigured, and the service address is a constant rather than a field any
deployment can reach.

**It binds content under `imdb`, not under a scheme of its own.** Cinemeta's
identifiers *are* IMDb ids — its manifest declares the `tt` prefix and nothing
else — so the accurate scheme is the shared one. That is what makes a title added
here the same Work as one a Stremio addon added rather than a duplicate
([platform#18](https://github.com/mosaic-media/platform/blob/main/docs/adr/0018-virtual-and-materialized-content.md)), and it means this
module's search results dedup against IMDb-keyed sources natively, which is a
gap `module-tmdb` has and cannot cheaply close.

**Two core modules fill one role class, which is the arity the class already
declared.** [platform#38](https://github.com/mosaic-media/platform/blob/main/docs/adr/0038-platform-binary-built-by-ci.md)'s role-class table
gives metadata/search "one or more"; this is the first exercise of that rather
than a change to it. They are complementary rather than redundant: Cinemeta
guarantees a floor with no configuration, TMDB gives richer data to a user
willing to hold a key.

**Therefore the bundled Cinemeta default in `module-stremio-addons` should be
removed.** The objection that correctly blocked its removal — that with TMDB
needing a key, deleting it would leave a fresh install with nothing — no longer
holds once a zero-configuration core provider is registered. It is now not merely
redundant but actively harmful: the Platform unions search providers without
cross-provider dedup, so a deployment running both would show every title twice.

## Alternatives considered

**Keep the bundled Cinemeta default in `module-stremio-addons` (the status
quo).** *Rejected.* It delegates a guarantee to an extension module — something
a deployment might not install, whose addon list a user can empty, and which
under [platform#39](https://github.com/mosaic-media/platform/blob/main/docs/adr/0039-extension-module-boundary.md) will not even share the
Platform's process. [platform#23](https://github.com/mosaic-media/platform/blob/main/docs/adr/0023-metadata-as-required-capability.md) recorded this placement as unresolved rather than
decided, and this record is the resolution.

**Ship a Mosaic-held TMDB key so `module-tmdb` alone suffices.** *Rejected.* A
key embedded in a public binary is not a secret, one key shared across every
install carries a rate limit shared across every install, and it makes Mosaic a
party to a third party's API terms on behalf of its users. It is also a
distribution and cost commitment that a self-hosted project should not take on
to solve a configuration problem.

**Seed a default addon into the Platform's settings at bootstrap
([platform#23](https://github.com/mosaic-media/platform/blob/main/docs/adr/0023-metadata-as-required-capability.md)'s own mechanism).**
*Rejected*, and this reverses that record's specified approach rather than
merely departing from it. Seeding means the Platform holds an addon URL and
therefore knows one ecosystem's protocol — the coupling [platform#23](https://github.com/mosaic-media/platform/blob/main/docs/adr/0023-metadata-as-required-capability.md)'s own Context
argues against two paragraphs earlier. It also makes the guarantee only as
durable as a settings document a user is invited to edit.

**Extend `module-tmdb` to fall back to Cinemeta when it holds no key.**
*Rejected.* A module sourcing from a second upstream is two anti-corruption
layers wearing one name ([module-stremio-addons#2](https://github.com/mosaic-media/module-stremio-addons/blob/main/docs/adr/0002-modules-as-anti-corruption-layers.md)),
and it makes provenance unanswerable: "where did this description come from" has
no answer a user can act on. Two modules, each honest about its source, is the
same capability with none of that.

**Make the core provider a general Stremio addon client with a fixed default.**
*Rejected.* That is `module-stremio-addons` with a different constructor, and
having two clients of one protocol in the binary means every dialect fix lands
in one of them and not the other.

**Promote `module-stremio-addons` itself into the core tier.** *Rejected*, for
the reason [platform#23](https://github.com/mosaic-media/platform/blob/main/docs/adr/0023-metadata-as-required-capability.md) gave and
[platform#3](https://github.com/mosaic-media/platform/blob/main/docs/adr/0003-platform-as-execution-kernel.md) restated: a third-party content ecosystem is
not infrastructure, and the module is load-bearing as the proof that the
extension path exists.

## Consequences

- **First boot works with no configuration, in spirit and not only in letter.**
  `RequireRoles` passes because a provider is registered *and* because that
  provider can answer. This is the property [platform#23](https://github.com/mosaic-media/platform/blob/main/docs/adr/0023-metadata-as-required-capability.md) asked for and that the
  seeded-addon stopgap was reaching for.
- **The guarantee now depends on a service Mosaic does not operate, and that
  should be said plainly rather than implied.** Cinemeta is a free public service
  run by a third party. If it goes away, the floor goes with it, and the
  guarantee is honest about installation rather than about uptime. The mitigation
  is that the class has arity "one or more" — another provider can be selected —
  not that this one is durable.
- **`module-stremio-addons` has a deletion owed to it**, and until it is taken a
  deployment running both shows duplicate search results. The removal is a
  version bump on that module and a `go.mod` bump in the Platform.
- **Neither core metadata module attaches a Part, so a library built through one
  plays nothing on its own** — and the Platform cannot bridge that today. Import
  routes solely to `ref.Provider`
  ([platform#18](https://github.com/mosaic-media/platform/blob/main/docs/adr/0018-virtual-and-materialized-content.md)), so streams from a
  provider that did not source the metadata are never attached. Composing a
  metadata provider with a stream provider is the **provider-precedence seam the
  roadmap has kept open**, and this record deliberately does not close it: it is
  the first time the seam is load-bearing rather than hypothetical, which is
  information, not a licence to invent an answer.
- **The core set grew by one, and [platform#3](https://github.com/mosaic-media/platform/blob/main/docs/adr/0003-platform-as-execution-kernel.md) warns it must stay small.** The
  justification is the guarantee clause and nothing else; it belongs in the
  register of what is and is not core that [platform#3](https://github.com/mosaic-media/platform/blob/main/docs/adr/0003-platform-as-execution-kernel.md) asks for and that still has
  no home.
- **A source checked against a fake is not a source that was checked.** Building
  this found that Cinemeta answers `200` for an unknown id in two different
  shapes — an unknown series returns an empty document, an unknown *film* returns
  a record echoing the id and type with no name — so the obvious emptiness test
  passes and the Platform materialises a library Work titled `tt99999999`. The
  same lesson the Stremio module learned from a User-Agent header, in a different
  place.
