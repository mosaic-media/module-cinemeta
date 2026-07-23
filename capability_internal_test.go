package cinemeta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	v1 "github.com/mosaic-media/sdk/contracts/platform/v1"
)

// These tests run the capability against a hermetic fake Cinemeta over httptest
// and an in-memory ContentService. They prove this module's own behaviour — the
// mapping of Cinemeta's documents onto the Platform's graph and onto the SDK's
// virtual types. The path through the Platform registry and real PostgreSQL is
// a separate test in the platform repository.
//
// They are in-package rather than in cinemeta_test because the service address
// is deliberately not configurable (see New): there is no settings document to
// point the capability somewhere else, which is the property that makes this a
// guarantee-clause core module, so a test has to reach past the constructor to
// redirect it. That is the right trade — the seam exists for tests only and
// cannot be reached by a deployment.

func newTestCapability(server *httptest.Server) *Capability {
	return &Capability{client: &Client{http: server.Client(), base: server.URL}}
}

func TestManifestDeclaresTheRolesItImplements(t *testing.T) {
	m := New(nil).Manifest()

	if m.ID != CapabilityID {
		t.Fatalf("manifest id = %q, want %q", m.ID, CapabilityID)
	}
	want := map[v1.Role]bool{v1.RoleMetadata: true, v1.RoleSearch: true, v1.RoleCatalog: true}
	if len(m.Provides) != len(want) {
		t.Fatalf("provides = %v, want exactly %d roles", m.Provides, len(want))
	}
	for _, role := range m.Provides {
		if !want[role] {
			t.Errorf("manifest declares unexpected role %q", role)
		}
	}
	// The Platform refuses to boot on a role declared but unbacked (ADR 0027).
	// The compile-time assertions in capability.go are the primary guard; this
	// checks the manifest agrees with them rather than drifting from them.
	if _, ok := any(New(nil)).(v1.StreamProvider); ok {
		t.Error("this module must not implement StreamProvider: Cinemeta indexes nothing")
	}
	if _, ok := any(New(nil)).(v1.SettingsUIProvider); ok {
		t.Error("this module must not implement SettingsUIProvider: it has no settings")
	}
}

func TestImportFilm(t *testing.T) {
	server := fakeCinemeta()
	defer server.Close()
	content := newFakeContent()

	result, err := newTestCapability(server).Import(context.Background(), content, v1.ImportRequest{
		Caller: v1.CallerFromSession("s-1"), Ref: movieRef("tt0133093"),
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if result.AlreadyKnown {
		t.Fatal("a first import must not be AlreadyKnown")
	}
	work := content.nodes[result.WorkID]
	if work.Kind != v1.NodeWork || work.MediaType != v1.MediaMovie {
		t.Fatalf("work kind/media = %q/%q, want work/movie", work.Kind, work.MediaType)
	}
	if work.Title != "The Matrix" {
		t.Fatalf("work title = %q, want the meta name", work.Title)
	}
	// A film is Work -> feature item, and no Parts: Cinemeta knows what exists,
	// not where to get it.
	if result.Containers != 0 || result.Items != 1 || result.Parts != 0 {
		t.Fatalf("counts = containers %d items %d parts %d, want 0/1/0", result.Containers, result.Items, result.Parts)
	}
	if len(content.parts) != 0 {
		t.Fatalf("attached %d parts; a metadata module must attach none", len(content.parts))
	}
	// Artwork is stored on the node rather than re-derived per read (ADR 0071),
	// and the poster is the upgraded size.
	if work.Artwork.Poster != "https://images.metahub.space/poster/medium/tt0133093/img" {
		t.Fatalf("work poster = %q, want the medium metahub poster", work.Artwork.Poster)
	}
	if work.Artwork.Logo == "" || work.Artwork.Backdrop == "" {
		t.Fatalf("work artwork = %+v, want a logo and a backdrop", work.Artwork)
	}
	// The binding ties the work to its IMDb id — the scheme a Stremio addon
	// would have used, which is what stops the same film becoming two Works.
	if len(content.binds) != 1 || content.binds[0].SourceProvider != "imdb" || content.binds[0].SourceRef != "tt0133093" {
		t.Fatalf("bindings = %+v, want one imdb/tt0133093", content.binds)
	}
}

func TestImportSeriesBuildsTheSeasonTree(t *testing.T) {
	server := fakeCinemeta()
	defer server.Close()
	content := newFakeContent()

	result, err := newTestCapability(server).Import(context.Background(), content, v1.ImportRequest{
		Caller: v1.CallerFromSession("s-1"), Ref: seriesRef("tt0903747"),
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	// The fake serves a special (season 0) and two episodes of season 1, out of
	// order and with the episode number under both spellings.
	if result.Containers != 2 || result.Items != 3 {
		t.Fatalf("counts = containers %d items %d, want 2/3", result.Containers, result.Items)
	}

	var seasons []string
	for _, n := range content.ordered() {
		if n.Kind == v1.NodeContainer {
			seasons = append(seasons, n.Title)
		}
	}
	if len(seasons) != 2 || seasons[0] != "Specials" || seasons[1] != "Season 1" {
		t.Fatalf("season containers = %v, want [Specials, Season 1]", seasons)
	}

	// Episodes land under their season, in order, each carrying its still as the
	// poster slot (ADR 0071).
	var episodes []string
	for _, n := range content.ordered() {
		if n.ItemType == v1.ItemEpisode {
			episodes = append(episodes, n.Title)
			if n.Artwork.Poster == "" {
				t.Errorf("episode %q has no still stored", n.Title)
			}
		}
	}
	want := []string{"Unaired Pilot", "Pilot", "Cat's in the Bag..."}
	if strings.Join(episodes, "|") != strings.Join(want, "|") {
		t.Fatalf("episodes = %v, want %v", episodes, want)
	}
}

func TestImportIsIdempotentAcrossProviders(t *testing.T) {
	server := fakeCinemeta()
	defer server.Close()
	content := newFakeContent()
	capability := newTestCapability(server)
	ctx := context.Background()
	caller := v1.CallerFromSession("s-1")

	first, err := capability.Import(ctx, content, v1.ImportRequest{Caller: caller, Ref: movieRef("tt0133093")})
	if err != nil {
		t.Fatalf("first Import: %v", err)
	}
	second, err := capability.Import(ctx, content, v1.ImportRequest{Caller: caller, Ref: movieRef("tt0133093")})
	if err != nil {
		t.Fatalf("second Import: %v", err)
	}

	if !second.AlreadyKnown || second.WorkID != first.WorkID {
		t.Fatalf("second import = %+v, want AlreadyKnown with work %s", second, first.WorkID)
	}
	if len(content.nodes) != 2 {
		t.Fatalf("graph holds %d nodes after two imports of one film, want 2", len(content.nodes))
	}
	// The dedup is on the IMDb id, not on a name of this module's own — so a
	// title another source already materialised under that scheme is found too.
	// That is the whole reason the binding scheme is "imdb" (ADR 0028).
	if len(content.binds) != 1 {
		t.Fatalf("bindings = %d, want the first import's only", len(content.binds))
	}
}

func TestImportRejectsARefWithNoIdentity(t *testing.T) {
	server := fakeCinemeta()
	defer server.Close()

	_, err := newTestCapability(server).Import(context.Background(), newFakeContent(), v1.ImportRequest{
		Caller: v1.CallerFromSession("s-1"), Ref: v1.ContentRef{Provider: CapabilityID},
	})
	if err == nil {
		t.Fatal("importing a ref with no native type or id must fail")
	}
}

func TestMetadataTranslatesTheDetailSurface(t *testing.T) {
	server := fakeCinemeta()
	defer server.Close()

	meta, err := newTestCapability(server).Metadata(context.Background(), v1.MetadataRequest{
		Caller: v1.CallerFromSession("s-1"), Ref: movieRef("tt0133093"),
	})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}

	if meta.Title != "The Matrix" || meta.Year != 1999 {
		t.Fatalf("title/year = %q/%d, want The Matrix/1999", meta.Title, meta.Year)
	}
	if meta.Rating != 8.7 {
		t.Fatalf("rating = %v, want 8.7 parsed from the string form", meta.Rating)
	}
	if meta.Runtime != "136 min" {
		t.Fatalf("runtime = %q, want the source's display string", meta.Runtime)
	}
	if len(meta.Genres) != 2 || meta.Genres[0] != "Action" {
		t.Fatalf("genres = %v, want the meta's list", meta.Genres)
	}
	// Cast comes from the links array, in billing order, names only — the source
	// carries no character and no headshot (ADR 0034's recorded gap).
	if len(meta.Cast) != 3 || meta.Cast[0].Name != "Keanu Reeves" {
		t.Fatalf("cast = %+v, want three billed names led by Keanu Reeves", meta.Cast)
	}
	for _, person := range meta.Cast {
		if person.Role != "" || person.Photo != "" {
			t.Errorf("cast member %q carries a role or photo; Cinemeta has neither", person.Name)
		}
	}
	if len(meta.Episodes) != 0 {
		t.Fatalf("a film has %d episode previews, want none", len(meta.Episodes))
	}
	// The ref is echoed back so the caller can tie the answer to what it asked.
	if meta.Ref.NativeID != "tt0133093" {
		t.Fatalf("ref = %+v, want the requested ref", meta.Ref)
	}
}

func TestMetadataOrdersASeriesEpisodePreview(t *testing.T) {
	server := fakeCinemeta()
	defer server.Close()

	meta, err := newTestCapability(server).Metadata(context.Background(), v1.MetadataRequest{
		Caller: v1.CallerFromSession("s-1"), Ref: seriesRef("tt0903747"),
	})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}

	if meta.Year != 2008 {
		t.Fatalf("year = %d, want 2008 read from a range with an en dash", meta.Year)
	}
	if len(meta.Episodes) != 3 {
		t.Fatalf("episodes = %d, want 3", len(meta.Episodes))
	}
	first, last := meta.Episodes[0], meta.Episodes[2]
	if first.Season != 0 || first.Episode != 1 {
		t.Fatalf("first preview = S%02dE%02d, want the special first", first.Season, first.Episode)
	}
	if last.Season != 1 || last.Episode != 2 {
		t.Fatalf("last preview = S%02dE%02d, want S01E02", last.Season, last.Episode)
	}
	if meta.Episodes[1].Overview == "" || meta.Episodes[1].Thumbnail == "" {
		t.Fatalf("episode preview = %+v, want a synopsis and a still", meta.Episodes[1])
	}
}

func TestMetadataFailsForAnIdCinemetaDoesNotKnow(t *testing.T) {
	server := fakeCinemeta()
	defer server.Close()
	capability := newTestCapability(server)
	ctx := context.Background()
	caller := v1.CallerFromSession("s-1")

	// Cinemeta answers 200 rather than 404 for an id it does not know, in two
	// different shapes — an unknown film echoes the id back inside an otherwise
	// bare meta, an unknown series returns no meta at all — so absence has to be
	// detected rather than caught. This was found against the live service and
	// not against an earlier version of the fake, which is the whole argument for
	// checking real sources: "the meta has an id" reads as "the title exists",
	// and the consequence downstream is a library Work titled `tt99999999`.
	for _, ref := range []v1.ContentRef{movieRef("tt99999999"), seriesRef("tt99999999")} {
		if _, err := capability.Metadata(ctx, v1.MetadataRequest{Caller: caller, Ref: ref}); err == nil {
			t.Errorf("%s: an unknown id must be an error, not a nameless record", ref.NativeType)
		}
		if _, err := capability.Import(ctx, newFakeContent(), v1.ImportRequest{Caller: caller, Ref: ref}); err == nil {
			t.Errorf("%s: importing an unknown id must fail rather than create a nameless Work", ref.NativeType)
		}
	}
}

func TestSearchUnionsBothTypesAndCarriesADedupableRef(t *testing.T) {
	server := fakeCinemeta()
	defer server.Close()

	resp, err := newTestCapability(server).Search(context.Background(), v1.SearchRequest{
		Caller: v1.CallerFromSession("s-1"), Text: "matrix",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(resp.Results) != 2 {
		t.Fatalf("results = %d, want one per type", len(resp.Results))
	}
	// Provider routes an import back here; the external identity is what the
	// Platform marks in-library on (ADR 0028).
	first := resp.Results[0]
	if first.Ref.Provider != CapabilityID {
		t.Fatalf("ref provider = %q, want %q", first.Ref.Provider, CapabilityID)
	}
	if first.Ref.ExternalScheme != "imdb" || first.Ref.ExternalID != first.Ref.NativeID {
		t.Fatalf("ref = %+v, want the imdb id as the external identity", first.Ref)
	}
	if first.Ref.MediaType != v1.MediaMovie {
		t.Fatalf("media type = %q, want movie", first.Ref.MediaType)
	}
}

func TestSearchMediaTypeChoosesTheEndpointRatherThanFiltering(t *testing.T) {
	server := fakeCinemeta()
	defer server.Close()

	resp, err := newTestCapability(server).Search(context.Background(), v1.SearchRequest{
		Caller: v1.CallerFromSession("s-1"), Text: "matrix", MediaType: v1.MediaTVSeries,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Ref.MediaType != v1.MediaTVSeries {
		t.Fatalf("results = %+v, want only the series", resp.Results)
	}

	// The saving is the point: a narrowed search must not pay for the request it
	// would only throw away.
	for _, path := range requestedPaths(server) {
		if strings.HasPrefix(path, "/catalog/movie/") {
			t.Fatalf("a series-only search still requested %q", path)
		}
	}
}

func TestSearchReturnsNothingForAMediaTypeCinemetaDoesNotCarry(t *testing.T) {
	server := fakeCinemeta()
	defer server.Close()

	resp, err := newTestCapability(server).Search(context.Background(), v1.SearchRequest{
		Caller: v1.CallerFromSession("s-1"), Text: "akira", MediaType: v1.MediaAnimeSeries,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// An honest empty answer, not an unfiltered one. Returning film and
	// television for an anime filter would look like the filter working.
	if len(resp.Results) != 0 {
		t.Fatalf("results = %+v, want none", resp.Results)
	}
	if len(requestedPaths(server)) != 0 {
		t.Fatalf("requests = %v, want none", requestedPaths(server))
	}
}

func TestSearchHonoursTheLimitHint(t *testing.T) {
	server := fakeCinemeta()
	defer server.Close()

	resp, err := newTestCapability(server).Search(context.Background(), v1.SearchRequest{
		Caller: v1.CallerFromSession("s-1"), Text: "matrix", Limit: 1,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results = %d, want the limit respected", len(resp.Results))
	}
}

func TestSearchSurvivesOneTypeFailing(t *testing.T) {
	server := fakeCinemeta(failMovieSearch)
	defer server.Close()

	resp, err := newTestCapability(server).Search(context.Background(), v1.SearchRequest{
		Caller: v1.CallerFromSession("s-1"), Text: "matrix",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// Half an answer beats none: a 502 on one catalog must not blank the other.
	if len(resp.Results) != 1 || resp.Results[0].Ref.MediaType != v1.MediaTVSeries {
		t.Fatalf("results = %+v, want the series half to survive", resp.Results)
	}
}

func TestCatalogsAreTheOnesAnswerableWithNoArgument(t *testing.T) {
	server := fakeCinemeta()
	defer server.Close()

	resp, err := newTestCapability(server).Catalogs(context.Background(), v1.CatalogsRequest{
		Caller: v1.CallerFromSession("s-1"),
	})
	if err != nil {
		t.Fatalf("Catalogs: %v", err)
	}

	if len(resp.Catalogs) != 4 {
		t.Fatalf("catalogs = %d, want 4", len(resp.Catalogs))
	}
	names := make(map[string]string)
	for _, c := range resp.Catalogs {
		// Two catalogs share an id, so the pair is the key — a flat list keyed on
		// id alone would silently hold one of each.
		names[c.NativeType+"/"+c.ID] = c.Name
	}
	for key, want := range map[string]string{
		"movie/top":         "Popular Films",
		"series/top":        "Popular Series",
		"movie/imdbRating":  "Featured Films",
		"series/imdbRating": "Featured Series",
	} {
		if names[key] != want {
			t.Errorf("catalog %s = %q, want %q", key, names[key], want)
		}
	}
}

func TestCatalogItemsPageAndRejectAnUnknownCatalog(t *testing.T) {
	server := fakeCinemeta()
	defer server.Close()
	capability := newTestCapability(server)
	ctx := context.Background()
	caller := v1.CallerFromSession("s-1")

	resp, err := capability.CatalogItems(ctx, v1.CatalogItemsRequest{
		Caller: caller, CatalogID: "top", NativeType: "movie", Skip: 100,
	})
	if err != nil {
		t.Fatalf("CatalogItems: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Title != "The Matrix" {
		t.Fatalf("items = %+v, want the film", resp.Items)
	}
	// Skip is an item offset on both sides, so it passes through untranslated.
	if !contains(requestedPaths(server), "/catalog/movie/top/skip=100.json") {
		t.Fatalf("requests = %v, want the skip in the path", requestedPaths(server))
	}

	if _, err := capability.CatalogItems(ctx, v1.CatalogItemsRequest{
		Caller: caller, CatalogID: "nonesuch", NativeType: "movie",
	}); err == nil {
		t.Fatal("an unknown catalog must fail rather than requesting a made-up path")
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func movieRef(id string) v1.ContentRef {
	return v1.ContentRef{Provider: CapabilityID, NativeID: id, NativeType: "movie", MediaType: v1.MediaMovie, ExternalScheme: "imdb", ExternalID: id}
}

func seriesRef(id string) v1.ContentRef {
	return v1.ContentRef{Provider: CapabilityID, NativeID: id, NativeType: "series", MediaType: v1.MediaTVSeries, ExternalScheme: "imdb", ExternalID: id}
}

// ---- fake Cinemeta ----

// The documents below are trimmed copies of what the live service returns,
// including the parts that look like mistakes and are not: a rating as a string,
// a year range with an en dash, a cast in the links array, an episode numbered
// under `number` rather than `episode`, videos out of order, and a poster served
// at the small size.

type serverMode int

const (
	healthy serverMode = iota
	failMovieSearch
)

var recorded sync.Map // *httptest.Server URL -> *[]string

// requestedPaths returns the paths the fake has been asked for, in order.
func requestedPaths(server *httptest.Server) []string {
	value, ok := recorded.Load(server.URL)
	if !ok {
		return nil
	}
	paths := value.(*pathLog)
	return paths.snapshot()
}

type pathLog struct {
	mu    sync.Mutex
	paths []string
}

func (p *pathLog) add(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paths = append(p.paths, path)
}

func (p *pathLog) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.paths...)
}

func fakeCinemeta(modes ...serverMode) *httptest.Server {
	mode := healthy
	if len(modes) > 0 {
		mode = modes[0]
	}
	log := &pathLog{}

	movieMeta := map[string]any{
		"id": "tt0133093", "type": "movie", "name": "The Matrix",
		"poster":     "https://images.metahub.space/poster/small/tt0133093/img",
		"background": "https://images.metahub.space/background/medium/tt0133093/img",
		"logo":       "https://images.metahub.space/logo/medium/tt0133093/img",
		"description": "When a beautiful stranger leads computer hacker Neo to a forbidding " +
			"underworld, he discovers the shocking truth.",
		"releaseInfo": "1999",
		"genres":      []string{"Action", "Sci-Fi"},
		"imdbRating":  "8.7",
		"runtime":     "136 min",
		"cast":        []string{"Keanu Reeves", "Laurence Fishburne", "Carrie-Anne Moss"},
		"links": []map[string]string{
			{"name": "8.7", "category": "imdb"},
			{"name": "Keanu Reeves", "category": "Cast"},
			{"name": "Laurence Fishburne", "category": "Cast"},
			{"name": "Carrie-Anne Moss", "category": "Cast"},
			{"name": "Action", "category": "Genres"},
		},
	}

	seriesMeta := map[string]any{
		"id": "tt0903747", "type": "series", "name": "Breaking Bad",
		"poster":      "https://images.metahub.space/poster/small/tt0903747/img",
		"background":  "https://images.metahub.space/background/medium/tt0903747/img",
		"releaseInfo": "2008–2013",
		"genre":       []string{"Crime", "Drama"},
		"imdbRating":  "9.5",
		"videos": []map[string]any{
			// Out of order, and the second one numbers its episode under `number`
			// with `episode` absent — both shapes occur in live records.
			{"id": "tt0903747:1:1", "name": "Pilot", "season": 1, "episode": 1,
				"overview": "A chemistry teacher is diagnosed.", "thumbnail": "https://episodes.metahub.space/tt0903747/1/1/w780.jpg",
				"released": "2008-01-21T05:00:00.000Z"},
			{"id": "tt0903747:0:1", "name": "Unaired Pilot", "season": 0, "number": 1,
				"description": "A special.", "thumbnail": "https://episodes.metahub.space/tt0903747/0/1/w780.jpg",
				"firstAired": "2008-01-01T05:00:00.000Z"},
			{"id": "tt0903747:1:2", "name": "Cat's in the Bag...", "season": 1, "episode": 2,
				"overview": "The cleanup.", "thumbnail": "https://episodes.metahub.space/tt0903747/1/2/w780.jpg",
				"released": "2008-01-27T05:00:00.000Z"},
		},
	}

	moviePreview := map[string]any{
		"id": "tt0133093", "type": "movie", "name": "The Matrix",
		// A search result's poster comes from a different CDN than a meta's, which
		// is why the size upgrade must not be applied blindly.
		"poster":      "https://m.media-amazon.com/images/M/matrix._V1_SX250.jpg",
		"releaseInfo": "1999",
	}
	seriesPreview := map[string]any{
		"id": "tt0903747", "type": "series", "name": "Breaking Bad",
		"poster": "https://images.metahub.space/poster/small/tt0903747/img", "releaseInfo": "2008–2013",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		log.add(path)
		switch {
		case path == "/meta/movie/tt0133093.json":
			writeJSON(w, map[string]any{"meta": movieMeta})
		case path == "/meta/series/tt0903747.json":
			writeJSON(w, map[string]any{"meta": seriesMeta})
		case strings.HasPrefix(path, "/meta/movie/"):
			// The unknown-film shape, copied from the live service: 200, with a
			// meta that echoes the id and the type and carries nothing else. It is
			// the shape that catches an emptiness test out, because the record is
			// not empty — it just has no name.
			writeJSON(w, map[string]any{"meta": map[string]any{
				"id": strings.TrimSuffix(strings.TrimPrefix(path, "/meta/movie/"), ".json"), "type": "movie",
			}})
		case strings.HasPrefix(path, "/meta/series/"):
			// The unknown-series shape, also from the live service: 200 with no
			// meta at all. Two shapes for one condition, so both are pinned.
			writeJSON(w, map[string]any{})
		case strings.HasPrefix(path, "/catalog/movie/"):
			if mode == failMovieSearch && strings.Contains(path, "/search=") {
				http.Error(w, "upstream is unwell", http.StatusBadGateway)
				return
			}
			writeJSON(w, map[string]any{"metas": []map[string]any{moviePreview}})
		case strings.HasPrefix(path, "/catalog/series/"):
			writeJSON(w, map[string]any{"metas": []map[string]any{seriesPreview}})
		default:
			http.NotFound(w, r)
		}
	}))
	recorded.Store(server.URL, log)
	return server
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// ---- in-memory ContentService ----

// fakeContent is a minimal but faithful v1.ContentService: it assigns ids, keeps
// nodes in memory, inherits a child's work id and media type from its parent as
// the real service does, and resolves FindContentByExternalID against stored
// works — enough to exercise the capability including its dedup.
type fakeContent struct {
	seq   int
	order []v1.NodeID
	nodes map[v1.NodeID]v1.Node
	parts []v1.Part
	binds []v1.BindContentSourceCommand
}

func newFakeContent() *fakeContent {
	return &fakeContent{nodes: make(map[v1.NodeID]v1.Node)}
}

// ordered returns the nodes in creation order, which is what lets a test assert
// the shape of the tree the capability built rather than a map's whim.
func (f *fakeContent) ordered() []v1.Node {
	out := make([]v1.Node, 0, len(f.order))
	for _, id := range f.order {
		out = append(out, f.nodes[id])
	}
	return out
}

func (f *fakeContent) nextID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%d", prefix, f.seq)
}

func (f *fakeContent) put(n v1.Node) {
	f.nodes[n.ID] = n
	f.order = append(f.order, n.ID)
}

func (f *fakeContent) AddContentWork(_ context.Context, cmd v1.AddContentWorkCommand) (v1.AddContentWorkResult, error) {
	id := v1.NodeID(f.nextID("work"))
	n := v1.Node{
		ID: id, WorkID: id, Kind: v1.NodeWork,
		MediaType: cmd.MediaType, Title: cmd.Title, Status: v1.NodeActive,
		ExternalIDs: cmd.ExternalIDs, Artwork: cmd.Artwork,
	}
	f.put(n)
	return v1.AddContentWorkResult{Work: n}, nil
}

func (f *fakeContent) AddContentChild(_ context.Context, cmd v1.AddContentChildCommand) (v1.AddContentChildResult, error) {
	parent := f.nodes[cmd.ParentID]
	id := v1.NodeID(f.nextID("node"))
	parentID := cmd.ParentID
	n := v1.Node{
		ID: id, WorkID: parent.WorkID, ParentID: &parentID,
		Kind: cmd.Kind, MediaType: parent.MediaType,
		ContainerType: cmd.ContainerType, ItemType: cmd.ItemType,
		Title: cmd.Title, NaturalOrder: cmd.NaturalOrder, Status: v1.NodeActive,
		Artwork: cmd.Artwork,
	}
	f.put(n)
	return v1.AddContentChildResult{Node: n}, nil
}

func (f *fakeContent) AttachContentPart(_ context.Context, cmd v1.AttachContentPartCommand) (v1.AttachContentPartResult, error) {
	p := v1.Part{ID: v1.PartID(f.nextID("part")), NodeID: cmd.NodeID, Role: cmd.Role, Location: cmd.Location}
	f.parts = append(f.parts, p)
	return v1.AttachContentPartResult{Part: p}, nil
}

func (f *fakeContent) BindContentSource(_ context.Context, cmd v1.BindContentSourceCommand) (v1.BindContentSourceResult, error) {
	f.binds = append(f.binds, cmd)
	b := v1.SourceBinding{
		ID: v1.SourceBindingID(f.nextID("bind")), NodeID: cmd.NodeID,
		SourceProvider: cmd.SourceProvider, SourceRef: cmd.SourceRef, Status: cmd.Status,
	}
	return v1.BindContentSourceResult{Binding: b}, nil
}

func (f *fakeContent) FindContentByExternalID(_ context.Context, q v1.FindContentByExternalIDQuery) (v1.FindContentByExternalIDResult, error) {
	var out []v1.Node
	for _, n := range f.ordered() {
		if !n.IsRoot() || len(n.ExternalIDs) == 0 {
			continue
		}
		ids := map[string]string{}
		if err := json.Unmarshal(n.ExternalIDs, &ids); err != nil {
			continue
		}
		if ids[q.Scheme] == q.Value {
			out = append(out, n)
		}
	}
	return v1.FindContentByExternalIDResult{Nodes: out}, nil
}

// The rest of the ContentService is not exercised by a metadata module — it
// writes a tree and reads bindings, and touches neither parts nor playback — so
// the remainder is stubbed to satisfy the interface.
func (f *fakeContent) SearchContent(context.Context, v1.SearchContentQuery) (v1.SearchContentResult, error) {
	return v1.SearchContentResult{}, nil
}
func (f *fakeContent) GetContentNode(context.Context, v1.GetContentNodeQuery) (v1.GetContentNodeResult, error) {
	return v1.GetContentNodeResult{}, nil
}
func (f *fakeContent) ListContentParts(context.Context, v1.ListContentPartsQuery) (v1.ListContentPartsResult, error) {
	return v1.ListContentPartsResult{}, nil
}
func (f *fakeContent) RelateContent(context.Context, v1.RelateContentCommand) (v1.RelateContentResult, error) {
	return v1.RelateContentResult{}, nil
}
func (f *fakeContent) ResolveContentBinding(context.Context, v1.ResolveContentBindingCommand) (v1.ResolveContentBindingResult, error) {
	return v1.ResolveContentBindingResult{}, nil
}
func (f *fakeContent) RecordPlaybackProgress(context.Context, v1.RecordPlaybackProgressCommand) (v1.RecordPlaybackProgressResult, error) {
	return v1.RecordPlaybackProgressResult{}, nil
}
func (f *fakeContent) SetPlaybackFinished(context.Context, v1.SetPlaybackFinishedCommand) (v1.SetPlaybackFinishedResult, error) {
	return v1.SetPlaybackFinishedResult{}, nil
}
func (f *fakeContent) GetPlaybackState(context.Context, v1.GetPlaybackStateQuery) (v1.GetPlaybackStateResult, error) {
	return v1.GetPlaybackStateResult{}, nil
}
func (f *fakeContent) ListPlaybackStates(context.Context, v1.ListPlaybackStatesQuery) (v1.ListPlaybackStatesResult, error) {
	return v1.ListPlaybackStatesResult{}, nil
}
func (f *fakeContent) ListInProgress(context.Context, v1.ListInProgressQuery) (v1.ListInProgressResult, error) {
	return v1.ListInProgressResult{}, nil
}

var _ v1.ContentService = (*fakeContent)(nil)
