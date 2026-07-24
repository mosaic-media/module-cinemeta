package cinemeta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	v1 "github.com/mosaic-media/sdk/contracts/platform/v1"
)

const (
	// CapabilityID is the id the Platform registers this module under, the id a
	// ref names to route an import back here, and the key any settings document
	// would be stored under (ADR 0021). There is no settings document — see New.
	CapabilityID = "cinemeta"
	// modulePath is this module's import path, which is how it reads its own
	// version out of the build graph rather than carrying a constant nothing
	// forces to stay true (SDK v0.12.0).
	modulePath = "github.com/mosaic-media/module-cinemeta"
	// providerScheme is the external-id scheme and source-binding provider this
	// module keys content under.
	//
	// Cinemeta's own identifiers *are* IMDb ids — its manifest declares the "tt"
	// prefix and nothing else — so binding under "imdb" rather than under
	// "cinemeta" is not a convenience, it is the accurate name for what the id is.
	// It is also what makes a title added here the same Work as the one a Stremio
	// addon would have added, instead of a duplicate (ADR 0028's dedup).
	providerScheme = "imdb"
)

// moduleVersion is resolved once from the build graph. A var rather than a const
// because it is a fact about the binary, discovered at startup, not a literal.
var moduleVersion = v1.ModuleVersion(modulePath)

// Capability satisfies the SDK's capability contract and every provider role it
// declares. The assertions fail to compile if the module drifts from what the
// Platform invokes or from a role it claims to fill (ADR 0027).
var (
	_ v1.Capability       = (*Capability)(nil)
	_ v1.MetadataProvider = (*Capability)(nil)
	_ v1.SearchProvider   = (*Capability)(nil)
	_ v1.CatalogProvider  = (*Capability)(nil)
)

// Capability is the Cinemeta metadata module. It holds an HTTP client and
// nothing else.
type Capability struct {
	client *Client
}

// New builds the capability over an HTTP client (nil for a default). The
// Platform passes its own, which carries the netguard dial guard and the
// outbound telemetry seam (ADR 0055).
//
// It takes no settings, and that is the point rather than an omission. Every
// provider role receives the module's settings document on each invocation
// (ADR 0021) and this module ignores it, because a guarantee-clause core module
// (ADR 0062) that can be configured is one that can be misconfigured: there is
// no key to be missing, no URL to be wrong, and no list a user can empty. The
// client is therefore built once here rather than per invocation, unlike a
// module whose configuration can change between two calls.
func New(httpClient *http.Client) *Capability {
	return &Capability{client: NewClient(httpClient)}
}

// Manifest is the module's self-declaration, including the provider roles it
// fills (ADR 0027).
//
// Three roles, and the three it does not declare are as deliberate: no stream
// and no subtitles, because Cinemeta describes content rather than indexing it;
// no settings UI, because there is nothing to set.
func (c *Capability) Manifest() v1.Manifest {
	return v1.Manifest{
		ID:       CapabilityID,
		Version:  moduleVersion,
		Name: "Cinemeta metadata",
		Description: "Mosaic's zero-configuration metadata floor: titles, descriptions and artwork " +
			"from Cinemeta, with no key to obtain and nothing to configure. It is what makes a fresh " +
			"install work before you have set anything up.",
		Provides: []v1.Role{v1.RoleMetadata, v1.RoleSearch, v1.RoleCatalog},
	}
}

// Import materialises the virtual item named by req.Ref — a result a search or
// catalog browse produced (ADR 0028) — into the object graph.
//
// It creates the Work with its artwork and external id, binds the source, and
// builds the containment tree: a film as Work → feature item, a series as
// Work → season container → episode item. It attaches **no Parts**, which is
// the shape rather than a gap: Cinemeta knows what exists, not where to get it,
// so an import through this module is a described library that needs a stream
// source installed beside it before anything plays.
func (c *Capability) Import(ctx context.Context, svc v1.ContentService, req v1.ImportRequest) (v1.ImportResult, error) {
	caller := req.Caller
	nativeType, nativeID := req.Ref.NativeType, req.Ref.NativeID
	if nativeType == "" || nativeID == "" {
		return v1.ImportResult{}, fmt.Errorf("ref needs a native type and id, got type=%q id=%q", nativeType, nativeID)
	}

	title, err := c.client.Meta(ctx, nativeType, nativeID)
	if err != nil {
		return v1.ImportResult{}, fmt.Errorf("fetch Cinemeta metadata: %w", err)
	}

	// Dedup before writing. A second import of the same title is a no-op that
	// returns the existing work, and — because the binding scheme is "imdb"
	// rather than a name of this module's own — so is importing a title a
	// Stremio addon already materialised. There is nothing to refresh on a
	// second pass the way a stream source has: a description does not go stale
	// between two clicks.
	if existing, found, err := c.find(ctx, svc, caller, nativeID); err != nil {
		return v1.ImportResult{}, err
	} else if found {
		return v1.ImportResult{WorkID: existing, AlreadyKnown: true}, nil
	}

	name := title.Title
	if name == "" {
		name = nativeID
	}
	work, err := svc.AddContentWork(ctx, v1.AddContentWorkCommand{
		Caller:      caller,
		MediaType:   mediaTypeFor(nativeType),
		Title:       name,
		ExternalIDs: externalIDs(nativeID),
		// Stored on the node rather than re-derived per read (ADR 0071). The
		// import already holds this art, so storing it costs nothing here and
		// saves a provider round trip for every card that later renders the title.
		// Landscape is left empty: Cinemeta has no wide key art, and an empty
		// field is how a consumer tells "the source has none" from "nobody asked".
		Artwork: v1.Artwork{Poster: title.Poster, Backdrop: title.Backdrop, Logo: title.Logo},
	})
	if err != nil {
		return v1.ImportResult{}, fmt.Errorf("create work: %w", err)
	}
	result := v1.ImportResult{WorkID: work.Work.ID}

	if _, err := svc.BindContentSource(ctx, v1.BindContentSourceCommand{
		Caller: caller, NodeID: work.Work.ID,
		SourceProvider: providerScheme, SourceRef: nativeID,
		MatchConfidence: 1, MatchMethod: v1.MatchExternalIDExact, Status: v1.BindingConfirmed,
	}); err != nil {
		return v1.ImportResult{}, fmt.Errorf("bind source: %w", err)
	}

	switch nativeType {
	case typeMovie:
		err = c.importFilm(ctx, svc, caller, work.Work.ID, &result)
	case typeSeries:
		err = c.importSeries(ctx, svc, caller, work.Work.ID, title, &result)
	default:
		// Unreachable through this module's own refs — Cinemeta serves two types
		// — but a ref is data the Platform hands in, so an unknown type lands as
		// a bare Work rather than as a failure.
	}
	if err != nil {
		return v1.ImportResult{}, err
	}

	v1.TelemetryFrom(ctx).Info("cinemeta import complete",
		v1.String("native_type", nativeType),
		v1.String("native_id", nativeID),
		v1.Int("containers", result.Containers),
		v1.Int("items", result.Items))

	return result, nil
}

// importFilm builds a film as Work → feature item. A Part attaches to an item,
// never a work (ADR 0013), so the item exists even with nothing to attach — it
// is where a stream source later hangs a release.
func (c *Capability) importFilm(ctx context.Context, svc v1.ContentService, caller v1.Caller, workID v1.NodeID, result *v1.ImportResult) error {
	if _, err := svc.AddContentChild(ctx, v1.AddContentChildCommand{
		Caller: caller, ParentID: workID,
		Kind: v1.NodeItem, ItemType: v1.ItemFeature,
		Title: "Feature", NaturalOrder: 1,
	}); err != nil {
		return fmt.Errorf("create feature item: %w", err)
	}
	result.Items++
	return nil
}

// importSeries builds a series as Work → season container → episode item over
// the episode list the client already ordered. Each episode carries its still as
// artwork: for an episode node the poster slot is the still (ADR 0071).
func (c *Capability) importSeries(ctx context.Context, svc v1.ContentService, caller v1.Caller, workID v1.NodeID, title Title, result *v1.ImportResult) error {
	for _, season := range groupBySeason(title.Episodes) {
		container, err := svc.AddContentChild(ctx, v1.AddContentChildCommand{
			Caller: caller, ParentID: workID,
			Kind: v1.NodeContainer, ContainerType: v1.ContainerSeason,
			Title: seasonTitle(season.number), NaturalOrder: float64(season.number),
		})
		if err != nil {
			return fmt.Errorf("create season %d: %w", season.number, err)
		}
		result.Containers++

		for _, episode := range season.episodes {
			if _, err := svc.AddContentChild(ctx, v1.AddContentChildCommand{
				Caller: caller, ParentID: container.Node.ID,
				Kind: v1.NodeItem, ItemType: v1.ItemEpisode,
				Title: episode.Title, NaturalOrder: float64(episode.Episode),
				Artwork: v1.Artwork{Poster: episode.Thumbnail},
			}); err != nil {
				return fmt.Errorf("create episode %d of season %d: %w", episode.Episode, season.number, err)
			}
			result.Items++
		}
	}
	return nil
}

// find looks for a Work already bound to this IMDb id, returning the root work's
// id since a match on a child would still mean the tree exists.
func (c *Capability) find(ctx context.Context, svc v1.ContentService, caller v1.Caller, id string) (v1.NodeID, bool, error) {
	found, err := svc.FindContentByExternalID(ctx, v1.FindContentByExternalIDQuery{
		Caller: caller, Scheme: providerScheme, Value: id,
	})
	if err != nil {
		return "", false, fmt.Errorf("search existing content: %w", err)
	}
	for _, node := range found.Nodes {
		if node.IsRoot() {
			return node.ID, true, nil
		}
	}
	return "", false, nil
}

// refFrom builds a ContentRef from a preview. Provider is this module, so an
// import routes back here; the external identity is the IMDb id, which is what
// makes a result for a title already in the library read as *In library* rather
// than as new (ADR 0028).
func refFrom(p Preview) v1.ContentRef {
	return v1.ContentRef{
		Provider:       CapabilityID,
		NativeID:       p.ID,
		NativeType:     p.NativeType,
		MediaType:      mediaTypeFor(p.NativeType),
		ExternalScheme: providerScheme,
		ExternalID:     p.ID,
	}
}

// mediaTypeFor maps a Cinemeta content type to a Platform media type. There are
// two; anything else canonicalises as open text (ADR 0015) rather than being
// rejected.
func mediaTypeFor(nativeType string) v1.MediaType {
	switch nativeType {
	case typeMovie:
		return v1.MediaMovie
	case typeSeries:
		return v1.MediaTVSeries
	default:
		return v1.NormaliseMediaType(nativeType)
	}
}

// nativeTypesFor turns a media-type filter into the Cinemeta types to query. An
// empty filter asks both; a filter naming something Cinemeta does not carry asks
// nothing, which is an honest empty answer rather than an unfiltered one.
func nativeTypesFor(mediaType v1.MediaType) []string {
	switch mediaType {
	case "":
		return []string{typeMovie, typeSeries}
	case v1.MediaMovie:
		return []string{typeMovie}
	case v1.MediaTVSeries:
		return []string{typeSeries}
	default:
		return nil
	}
}

// externalIDs builds the Work's external-id document — the flat scheme-to-id
// shape FindContentByExternalID reads.
func externalIDs(id string) []byte {
	document, _ := json.Marshal(map[string]string{providerScheme: id})
	return document
}

// seasonGroup collects one season's episodes.
type seasonGroup struct {
	number   int
	episodes []Episode
}

// groupBySeason collects an ordered episode list into ordered seasons. The list
// arrives sorted from the client, so this preserves an order rather than
// imposing one.
func groupBySeason(episodes []Episode) []seasonGroup {
	var groups []seasonGroup
	for _, e := range episodes {
		if n := len(groups); n > 0 && groups[n-1].number == e.Season {
			groups[n-1].episodes = append(groups[n-1].episodes, e)
			continue
		}
		groups = append(groups, seasonGroup{number: e.Season, episodes: []Episode{e}})
	}
	return groups
}

// seasonTitle names a season container. Cinemeta numbers specials, pilots and
// extras as season 0, and calling that "Season 0" reads as a bug rather than as
// a convention.
func seasonTitle(number int) string {
	if number == 0 {
		return "Specials"
	}
	return "Season " + strconv.Itoa(number)
}
