package cinemeta

import (
	"context"
	"fmt"

	v1 "github.com/mosaic-media/sdk/contracts/platform/v1"
)

// Metadata resolves descriptive detail for a ref (RoleMetadata). It backs the
// detail screen for both a virtual result and an in-library node, and it is the
// detail Import draws on (ADR 0027, ADR 0034).
//
// It is the role the required capability class (ADR 0035) is really about: with
// no provider filling it, a Mosaic can list what it owns and describe none of
// it.
func (c *Capability) Metadata(ctx context.Context, req v1.MetadataRequest) (v1.ContentMetadata, error) {
	title, err := c.client.Meta(ctx, req.Ref.NativeType, req.Ref.NativeID)
	if err != nil {
		return v1.ContentMetadata{}, fmt.Errorf("fetch Cinemeta metadata: %w", err)
	}

	// Through the SDK's ambient telemetry rather than a print (ADR 0059): this
	// lands in the Platform's records, attributed to this module and correlated
	// with the request that caused it. What is worth recording is which of the
	// fields a screen composes around actually arrived — a detail with no logo is
	// otherwise indistinguishable from a detail whose logo failed to load.
	//
	// Nothing here is classified. A Cinemeta id and content type are a public
	// catalogue's own identifiers, and there is no user configuration in the
	// picture to leak — which is itself a consequence of the module having none.
	v1.TelemetryFrom(ctx).Debug("cinemeta metadata resolved",
		v1.String("native_type", req.Ref.NativeType),
		v1.String("native_id", req.Ref.NativeID),
		v1.Bool("has_logo", title.Logo != ""),
		v1.Int("cast", len(title.Cast)),
		v1.Int("episodes", len(title.Episodes)))

	return v1.ContentMetadata{
		Ref:      req.Ref,
		Title:    title.Title,
		Year:     title.Year,
		Overview: title.Overview,
		Poster:   title.Poster,
		Backdrop: title.Backdrop,
		Logo:     title.Logo,
		Genres:   title.Genres,
		Rating:   title.Rating,
		Runtime:  title.Runtime,
		Cast:     castPeople(title.Cast),
		Episodes: episodePreviews(title.Episodes),
	}, nil
}

// castPeople maps billed names onto the SDK's Person.
//
// Role and Photo are left empty, and it is worth being explicit that this is the
// source's limit rather than a decode that stopped early: Cinemeta carries a
// cast as names, with no character and no headshot anywhere in its meta shape.
// That is one of ADR 0034's recorded gaps, and it is what a TMDB-class provider
// exists to close.
func castPeople(names []string) []v1.Person {
	if len(names) == 0 {
		return nil
	}
	out := make([]v1.Person, 0, len(names))
	for _, name := range names {
		out = append(out, v1.Person{Name: name})
	}
	return out
}

// episodePreviews maps the episode list onto the SDK's read-only projection. It
// is deliberately not the materialised tree — Import builds that — but it is
// what lets a user read a series' episode list before deciding to add it
// (ADR 0034).
func episodePreviews(episodes []Episode) []v1.EpisodePreview {
	if len(episodes) == 0 {
		return nil
	}
	out := make([]v1.EpisodePreview, 0, len(episodes))
	for _, e := range episodes {
		out = append(out, v1.EpisodePreview{
			Season:    e.Season,
			Episode:   e.Episode,
			Title:     e.Title,
			Overview:  e.Overview,
			Thumbnail: e.Thumbnail,
			Released:  e.Released,
		})
	}
	return out
}
