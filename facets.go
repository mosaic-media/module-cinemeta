package cinemeta

import (
	"context"
	"sync"
	"time"

	v1 "github.com/mosaic-media/sdk/contracts/platform/v1"
)

// The narrowings a catalog accepts.
//
// A catalog filter is declared with its permitted values (SDK v0.25.0), and
// Cinemeta publishes exactly that list in its manifest — an `extra` named
// `genre` with an `options` array, per catalog and per type. So the options are
// read from the source rather than written down here, and the reason is the same
// one that keeps the fake honest: a hardcoded copy of somebody else's vocabulary
// is wrong the first time they change it, with nothing to report it.
//
// **This is the one manifest read in this module, and it does not make it a
// client of the addon protocol.** The catalog *set* stays fixed and curated in
// Catalogs() — which resources exist, which are usable and what they are called
// is still decided here rather than negotiated — because half of what Cinemeta
// declares takes an argument a browse surface addressing a catalog by id cannot
// supply. What is fetched is one list of values for a control, and nothing about
// which catalogs there are.
//
// Cinemeta's `genre` extra is also the sharpest example of why
// `CatalogFilter.Label` exists separately from `Name`: on its "New" catalogs,
// the parameter called `genre` carries a **year**. Those catalogs are not
// exposed, so the collision never reaches a screen — but it is why a source's
// parameter name is not fit to be shown to a user.

// filterGenre is the addon-protocol extra this module offers, and its name is
// the protocol's own — it goes straight back into a catalog path.
const filterGenre = "genre"

// facetTTL is how long a fetched manifest is trusted. Cinemeta's genre list is
// the same nineteen words it has been for years, so a day is generous rather
// than tuned.
const facetTTL = 24 * time.Hour

// facetCache holds the genre options per catalog, keyed by type and id.
//
// A failed fetch caches nothing and yields no options, so a catalog declares no
// filter and renders exactly as it did before this existed. That is the correct
// degradation: an absent control is visibly absent, where a control offering
// values the source will not honour is not.
type facetCache struct {
	mu      sync.Mutex
	value   map[string][]v1.CatalogFilterOption
	fetched time.Time
}

// genresFor returns the declared options for one catalog, refreshing the
// manifest when stale.
//
// The lock is held across the fetch, which serialises a burst of concurrent
// catalog renders into one request rather than letting each issue its own.
func (c *facetCache) genresFor(ctx context.Context, client *Client, nativeType, catalogID string) []v1.CatalogFilterOption {
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Since(c.fetched) >= facetTTL {
		// Stamped before the result is known, so a service that is failing is
		// retried on the next TTL rather than on every single call.
		c.fetched = time.Now()
		if fetched, err := client.catalogGenres(ctx); err == nil {
			c.value = fetched
		}
	}
	return c.value[nativeType+"/"+catalogID]
}

// filtersFor renders a catalog's declaration, dropping an empty option list
// rather than declaring a control that cannot be exercised.
func (c *facetCache) filtersFor(ctx context.Context, client *Client, nativeType, catalogID string) []v1.CatalogFilter {
	options := c.genresFor(ctx, client, nativeType, catalogID)
	if len(options) == 0 {
		return nil
	}
	return []v1.CatalogFilter{{Name: filterGenre, Label: "Genre", Options: options}}
}

// catalogGenres reads the manifest and returns each catalog's genre options,
// keyed by "type/id".
//
// Value and label are the same string here, because the addon protocol's genre
// extra is addressed by its own words. That is not true of every source — TMDB
// addresses a genre by a numeric id — which is why the SDK carries both.
func (c *Client) catalogGenres(ctx context.Context) (map[string][]v1.CatalogFilterOption, error) {
	var manifest struct {
		Catalogs []struct {
			Type  string `json:"type"`
			ID    string `json:"id"`
			Extra []struct {
				Name    string   `json:"name"`
				Options []string `json:"options"`
			} `json:"extra"`
		} `json:"catalogs"`
	}
	if err := c.getJSON(ctx, c.base+"/manifest.json", &manifest); err != nil {
		return nil, err
	}

	out := map[string][]v1.CatalogFilterOption{}
	for _, cat := range manifest.Catalogs {
		for _, extra := range cat.Extra {
			if extra.Name != filterGenre {
				continue
			}
			options := make([]v1.CatalogFilterOption, 0, len(extra.Options))
			for _, o := range extra.Options {
				if o == "" {
					continue
				}
				options = append(options, v1.CatalogFilterOption{Value: o, Label: o})
			}
			if len(options) > 0 {
				out[cat.Type+"/"+cat.ID] = options
			}
		}
	}
	return out, nil
}
