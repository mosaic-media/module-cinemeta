package cinemeta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The Cinemeta HTTP client. This file is the anti-corruption layer (ADR 0051):
// every Cinemeta-ism — its two spellings of a genre list, its year ranges with
// an en dash, its cast hidden in a categorised links array, its episodes as a
// flat video list numbered two different ways, its thumbnail-sized posters —
// stops here. Nothing above this file sees a Cinemeta shape.
//
// It speaks the Stremio addon protocol, because that is what Cinemeta serves,
// but it is **not** a client of that protocol: it talks to one known service
// whose resources and catalogs are fixed. There is no resource to negotiate and
// no addon list to order, and the one manifest read — the genre options a
// catalog declares its filter from, in facets.go — decides no catalog's
// existence, only the values of one control. That is the whole difference
// between a module that guarantees metadata and one that sources it from
// whatever a user configured (ADR 0062), and it is why the general Stremio
// addon client is a separate module rather than this one with more options.

const (
	// apiBase is Cinemeta's public endpoint. It is a constant rather than a
	// setting on purpose: a module whose address can be changed is a module that
	// can be pointed at nothing, and the guarantee clause (ADR 0062) is that this
	// one always works.
	//
	// Catalog requests 307-redirect to Cinemeta's catalog host. Go's client
	// follows that by itself; it is noted here because it is invisible in the
	// URLs below and surprising in a packet capture.
	apiBase = "https://v3-cinemeta.strem.io"

	// The two content types Cinemeta serves, per its manifest. There is no third.
	typeMovie  = "movie"
	typeSeries = "series"

	// searchCatalog is the catalog id whose entries accept a search query. Both
	// of Cinemeta's "top" catalogs declare it; the type in the path chooses which
	// is asked.
	searchCatalog = "top"

	// requestTimeout bounds one call when the Platform hands in no client of its
	// own. The Platform always does (its client carries the dial guard and the
	// outbound telemetry seam), so this is the floor for a test or a standalone
	// use rather than the normal case.
	requestTimeout = 20 * time.Second

	// maxCast is how many billed names a detail carries. A detail screen shows
	// the *top* cast, and Cinemeta's links array repeats the same people across
	// categories.
	maxCast = 18
)

// userAgent identifies Mosaic to Cinemeta. Sent for a reason rather than for
// courtesy: the Stremio module found that Cloudflare-fronted addons refuse Go's
// default "Go-http-client/1.1" with a 403, and the failure reads exactly like
// the service being down. A var rather than a const because the version half of
// it is read from the build graph.
var userAgent = "mosaic-module-cinemeta/" + moduleVersion

// Client is a Cinemeta API client. It holds only an HTTP client: there is
// nothing to configure, so there is nothing else to hold.
type Client struct {
	http *http.Client
	// base is the service root. It is a field only so tests can point the client
	// at an httptest server; nothing in the module's own configuration path can
	// reach it, which is what keeps "this module always works" true.
	base string
}

// NewClient builds a client over an HTTP client (nil for a default). The
// Platform passes its own, which carries the netguard dial guard and the
// outbound telemetry seam (ADR 0055); a module that builds its own bypasses
// both.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	return &Client{http: httpClient, base: apiBase}
}

// Meta fetches one title's full record.
//
// One request is the whole story here, and it is worth contrasting with a
// metadata API that splits a record across endpoints: Cinemeta returns the
// description, artwork, rating, runtime, cast and — for a series — every
// episode of every season in a single document. That is the one thing this
// source is unambiguously better at.
func (c *Client) Meta(ctx context.Context, nativeType, id string) (Title, error) {
	if nativeType != typeMovie && nativeType != typeSeries {
		return Title{}, fmt.Errorf("unsupported Cinemeta type %q; expected %q or %q", nativeType, typeMovie, typeSeries)
	}
	if strings.TrimSpace(id) == "" {
		return Title{}, fmt.Errorf("a Cinemeta id is required")
	}

	var resp struct {
		Meta rawMeta `json:"meta"`
	}
	if err := c.getJSON(ctx, c.base+"/meta/"+nativeType+"/"+url.PathEscape(id)+".json", &resp); err != nil {
		return Title{}, err
	}
	// Cinemeta answers 200 for an id it does not know, so an absent record has to
	// be detected rather than caught as an error — and it has two shapes. An
	// unknown series comes back as an empty document; an unknown *film* comes
	// back as a meta echoing the id and the type with nothing else in it, which
	// is the one that matters: a record is unusable without a name, and treating
	// "has an id" as "exists" materialises a Work titled `tt99999999`. The name is
	// therefore the test. (Only a malformed id gets a 404.)
	if strings.TrimSpace(resp.Meta.Name) == "" {
		return Title{}, fmt.Errorf("Cinemeta has no %s with id %s", nativeType, id)
	}
	return resp.Meta.title(nativeType), nil
}

// Search queries Cinemeta's search-capable catalogs for free text and returns
// the union, de-duplicated by id.
//
// nativeTypes selects which catalogs are asked — film, television or both. The
// media-type filter therefore *chooses the endpoint* rather than filtering
// results afterwards, which is one fewer round trip when a caller has narrowed
// the query. A type that errors is skipped rather than failing the search: half
// an answer beats none when the other half is a transient 502.
func (c *Client) Search(ctx context.Context, text string, nativeTypes []string) ([]Preview, error) {
	text = strings.TrimSpace(text)
	if text == "" || len(nativeTypes) == 0 {
		return nil, nil
	}

	// Collected per type and concatenated in the caller's order afterwards, so
	// two concurrent requests cannot make the result order depend on which
	// answered first.
	per := make([][]Preview, len(nativeTypes))
	var wg sync.WaitGroup
	for i, nativeType := range nativeTypes {
		wg.Add(1)
		go func(i int, nativeType string) {
			defer wg.Done()
			endpoint := c.base + "/catalog/" + nativeType + "/" + searchCatalog +
				"/search=" + url.PathEscape(text) + ".json"
			var resp struct {
				Metas []rawPreview `json:"metas"`
			}
			if err := c.getJSON(ctx, endpoint, &resp); err != nil {
				return
			}
			for _, m := range resp.Metas {
				per[i] = append(per[i], m.preview(nativeType))
			}
		}(i, nativeType)
	}
	wg.Wait()

	var out []Preview
	seen := make(map[string]bool)
	for _, list := range per {
		for _, p := range list {
			if p.ID == "" || seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			out = append(out, p)
		}
	}
	return out, nil
}

// Catalogs is the set of collections this module exposes.
//
// It is a curated subset of what Cinemeta's manifest declares, and the omissions
// are deliberate rather than an oversight. Cinemeta's "New" catalogs require a
// year to be supplied as a genre parameter, and its "Last videos" and "Calendar
// videos" catalogs require a list of series ids the caller must already hold —
// neither fits a browse surface that addresses a catalog by id alone (ADR 0028).
// What is left is the four that answer with no argument.
//
// The names are Cinemeta's own, qualified by type. Its manifest calls both of
// the "top" catalogs "Popular", which is unambiguous in a client that shows film
// and television separately and useless in a flat list of collections.
func (c *Client) Catalogs() []CatalogDecl {
	return []CatalogDecl{
		{ID: "top", Type: typeMovie, Name: "Popular Films"},
		{ID: "top", Type: typeSeries, Name: "Popular Series"},
		{ID: "imdbRating", Type: typeMovie, Name: "Featured Films"},
		{ID: "imdbRating", Type: typeSeries, Name: "Featured Series"},
	}
}

// catalogPage is how many entries one Cinemeta catalog page holds. It is fixed
// by the service and it is the only basis this module has for saying whether
// there is another page — see the return value below.
const catalogPage = 100

// CatalogItems lists one catalog's entries. skip is an item offset, which is
// what Cinemeta's own paging parameter means, so it passes through untranslated.
// genre narrows the listing when non-empty, as the addon protocol's `genre`
// extra; the caller has already checked it against what the catalog declared.
//
// It reports whether another page exists, and this is the **weaker** of the two
// statements the SDK describes: Cinemeta returns no total, so a full page is all
// there is to go on. Only the provider can make even that claim, because only
// the provider knows the page size — and its cost is bounded and visible, a
// final page of exactly a hundred asking for one more that comes back empty.
// The alternative is what was here before: paging built, and dead, because
// nothing ever said there was more.
func (c *Client) CatalogItems(ctx context.Context, catalogID, nativeType, genre string, skip int) ([]Preview, bool, error) {
	decl, ok := c.findCatalog(catalogID, nativeType)
	if !ok {
		return nil, false, fmt.Errorf("unknown catalog %q for type %q", catalogID, nativeType)
	}

	endpoint := c.base + "/catalog/" + decl.Type + "/" + decl.ID
	// The protocol spells extras as path segments, `name=value` joined by `&`,
	// before the `.json`. Each value is path-escaped: a genre is somebody else's
	// vocabulary and "Sci-Fi & Fantasy" is a real one, which unescaped would
	// read as a second extra.
	var extras []string
	if genre != "" {
		extras = append(extras, filterGenre+"="+url.PathEscape(genre))
	}
	if skip > 0 {
		extras = append(extras, "skip="+strconv.Itoa(skip))
	}
	if len(extras) > 0 {
		endpoint += "/" + strings.Join(extras, "&")
	}
	endpoint += ".json"

	var resp struct {
		Metas []rawPreview `json:"metas"`
	}
	if err := c.getJSON(ctx, endpoint, &resp); err != nil {
		return nil, false, err
	}

	out := make([]Preview, 0, len(resp.Metas))
	for _, m := range resp.Metas {
		// A catalog's entries carry their own type, but the catalog *is* one
		// type, so the declaration is the authority and a missing field is not a
		// special case.
		out = append(out, m.preview(decl.Type))
	}
	return out, len(resp.Metas) >= catalogPage, nil
}

// findCatalog resolves a catalog by id and type. Two catalogs share an id
// ("top" for film and for television), so the type is part of the key rather
// than decoration.
func (c *Client) findCatalog(id, nativeType string) (CatalogDecl, bool) {
	for _, decl := range c.Catalogs() {
		if decl.ID == id && decl.Type == nativeType {
			return decl, true
		}
	}
	return CatalogDecl{}, false
}

// getJSON performs one GET and decodes the JSON body into out.
func (c *Client) getJSON(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call Cinemeta: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Cinemeta returned %d for %s", resp.StatusCode, requestPath(endpoint))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode Cinemeta response for %s: %w", requestPath(endpoint), err)
	}
	return nil
}

// requestPath reduces a URL to its path for an error message. Error text reaches
// logs, and the host adds nothing when there is only ever one of them.
func requestPath(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil && u.Path != "" {
		return u.Path
	}
	return endpoint
}

// The translated types. Everything above this file speaks these; nothing above
// it speaks Cinemeta's.

// Preview is one search or catalog result — enough to render a row and to
// address the title afterwards.
type Preview struct {
	ID         string
	NativeType string
	Title      string
	Year       int
	Poster     string
}

// CatalogDecl is one collection this module exposes.
type CatalogDecl struct {
	ID   string
	Type string
	Name string
}

// Title is one film or series, as fully as Cinemeta describes it.
type Title struct {
	ID         string
	NativeType string
	Title      string
	Year       int
	Overview   string
	Poster     string
	Backdrop   string
	Logo       string
	Genres     []string
	Rating     float64
	Runtime    string
	// Cast is billed names, and names only. Cinemeta carries no character and no
	// headshot for a credit, which is one of ADR 0034's recorded gaps rather than
	// something this decode drops — so the type says so instead of offering
	// fields that would always be empty.
	Cast []string
	// Episodes is populated for a series, in season/episode order.
	Episodes []Episode
}

// Episode is one episode of a series.
type Episode struct {
	Season    int
	Episode   int
	Title     string
	Overview  string
	Thumbnail string
	Released  string
}

// The raw Cinemeta wire shapes. They exist to be decoded and immediately
// translated; nothing outside this file references them.

type rawPreview struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Poster      string `json:"poster"`
	ReleaseInfo string `json:"releaseInfo"`
}

func (r rawPreview) preview(nativeType string) Preview {
	if r.Type != "" {
		nativeType = r.Type
	}
	return Preview{
		ID:         r.ID,
		NativeType: nativeType,
		Title:      r.Name,
		Year:       parseYear(r.ReleaseInfo),
		Poster:     posterURL(r.Poster),
	}
}

type rawMeta struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Poster      string `json:"poster"`
	Background  string `json:"background"`
	Logo        string `json:"logo"`
	Description string `json:"description"`
	ReleaseInfo string `json:"releaseInfo"`
	// Genres and Genre are the same fact under two keys. Cinemeta emits both and
	// they have been seen to disagree in which is populated, so both are read.
	Genres []string `json:"genres"`
	Genre  []string `json:"genre"`
	// ImdbRating is a string ("8.7") in Cinemeta's API, not a number.
	ImdbRating string `json:"imdbRating"`
	// Runtime is a display string whose format is the source's ("136 min").
	Runtime string `json:"runtime"`
	// Cast is the top-level name list. Links carries the same people as
	// categorised entries, in billing order; both are read (see castOf).
	Cast   []string   `json:"cast"`
	Links  []rawLink  `json:"links"`
	Videos []rawVideo `json:"videos"`
}

// rawLink is one entry of a meta's links array — the shape Cinemeta uses for
// cast, directors, writers and genres as categorised references.
type rawLink struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

// rawVideo is one episode of a series' meta.
type rawVideo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Title   string `json:"title"`
	Season  int    `json:"season"`
	Episode int    `json:"episode"`
	// Number is the same fact as Episode under another key. Cinemeta populates
	// both on current records and only one on some older ones.
	Number int `json:"number"`
	// Overview and Description are, again, the same fact twice.
	Overview    string `json:"overview"`
	Description string `json:"description"`
	Thumbnail   string `json:"thumbnail"`
	// Released is an ISO datetime, null for an episode that has not aired.
	Released   string `json:"released"`
	FirstAired string `json:"firstAired"`
}

// title translates a full Cinemeta record into the module's own shape.
func (r rawMeta) title(nativeType string) Title {
	if r.Type != "" {
		nativeType = r.Type
	}

	genres := r.Genres
	if len(genres) == 0 {
		genres = r.Genre
	}

	out := Title{
		ID:         r.ID,
		NativeType: nativeType,
		Title:      r.Name,
		Year:       parseYear(r.ReleaseInfo),
		Overview:   r.Description,
		Poster:     posterURL(r.Poster),
		Backdrop:   r.Background,
		Logo:       r.Logo,
		Genres:     genres,
		Rating:     parseRating(r.ImdbRating),
		Runtime:    strings.TrimSpace(r.Runtime),
		Cast:       castOf(r),
	}
	if nativeType == typeSeries {
		out.Episodes = episodesOf(r.Videos)
	}
	return out
}

// castOf reads billed names from a meta, preferring the links array and falling
// back to the top-level cast list.
//
// The links array is preferred because it is ordered by billing and the flat
// list is not reliably so — a cast rail whose lead is fourth reads as broken.
// Names are de-duplicated because the two sources overlap, and capped because a
// large production credits hundreds of people and a detail screen shows the top
// of the bill.
func castOf(r rawMeta) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, maxCast)
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || len(out) >= maxCast {
			return
		}
		seen[name] = true
		out = append(out, name)
	}

	for _, l := range r.Links {
		// Cinemeta capitalises the category as "Cast"; older records and other
		// sources of the same shape use "actor". Both are matched case-insensitively
		// rather than assuming either.
		if strings.EqualFold(l.Category, "Cast") || strings.EqualFold(l.Category, "actor") {
			add(l.Name)
		}
	}
	for _, name := range r.Cast {
		add(name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// episodesOf translates and orders a series' flat video list.
//
// Cinemeta returns the videos in no dependable order and numbers an episode
// under either `episode` or `number`, so both the numbering and the ordering are
// settled here rather than left for a caller to discover it cannot rely on them.
func episodesOf(videos []rawVideo) []Episode {
	if len(videos) == 0 {
		return nil
	}
	out := make([]Episode, 0, len(videos))
	for _, v := range videos {
		number := v.Episode
		if number == 0 {
			number = v.Number
		}
		released := v.Released
		if released == "" {
			released = v.FirstAired
		}
		overview := v.Overview
		if overview == "" {
			overview = v.Description
		}
		out = append(out, Episode{
			Season:    v.Season,
			Episode:   number,
			Title:     episodeName(v, number),
			Overview:  overview,
			Thumbnail: v.Thumbnail,
			Released:  released,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Season != out[j].Season {
			return out[i].Season < out[j].Season
		}
		return out[i].Episode < out[j].Episode
	})
	return out
}

// episodeName is the episode's own title, falling back to its number. Cinemeta
// titles an episode under `name`; `title` is read too because the addon shape it
// serves defines that key and some records carry it.
func episodeName(v rawVideo, number int) string {
	if name := strings.TrimSpace(v.Name); name != "" {
		return name
	}
	if title := strings.TrimSpace(v.Title); title != "" {
		return title
	}
	return "Episode " + strconv.Itoa(number)
}

// metahubPosterHost and the size segments are Cinemeta's artwork CDN. A poster
// arrives at "small", which is a 40 kB thumbnail that renders visibly soft on a
// card, while the same asset exists at "medium" for no more than a wider URL.
const (
	metahubPosterPrefix = "https://images.metahub.space/poster/small/"
	metahubPosterMedium = "https://images.metahub.space/poster/medium/"
)

// posterURL upgrades a Cinemeta poster to the size Mosaic renders at.
//
// It rewrites only the exact CDN prefix above. Cinemeta also serves posters from
// other hosts — search results come back with Amazon image URLs — and a
// substitution applied to those would produce a link to nothing, which is worse
// than a soft image and harder to notice.
func posterURL(poster string) string {
	poster = strings.TrimSpace(poster)
	if rest, ok := strings.CutPrefix(poster, metahubPosterPrefix); ok {
		return metahubPosterMedium + rest
	}
	return poster
}

// parseYear reads the leading year from a Cinemeta releaseInfo ("1999",
// "2005–2008" — an en dash, not a hyphen), returning 0 when there is none.
func parseYear(releaseInfo string) int {
	releaseInfo = strings.TrimSpace(releaseInfo)
	if len(releaseInfo) < 4 {
		return 0
	}
	year, err := strconv.Atoi(releaseInfo[:4])
	if err != nil {
		return 0
	}
	return year
}

// parseRating reads Cinemeta's string rating ("8.7") as a number, 0 when absent
// or unparseable. Zero is how the SDK spells "the source did not say".
func parseRating(rating string) float64 {
	rating = strings.TrimSpace(rating)
	if rating == "" {
		return 0
	}
	f, err := strconv.ParseFloat(rating, 64)
	if err != nil {
		return 0
	}
	return f
}
