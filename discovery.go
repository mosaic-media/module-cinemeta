package cinemeta

import (
	"context"
	"fmt"

	v1 "github.com/mosaic-media/sdk/contracts/platform/v1"
)

// The two discovery roles. They are what produce a ref in the first place: a
// deployment with only RoleMetadata could describe content it had no way to
// name, which is why ADR 0035 makes metadata *and* search one required
// capability class rather than two separate ones.

// Search returns virtual candidates for free text (RoleSearch).
//
// The media-type filter selects which of Cinemeta's catalogs is asked rather
// than filtering what came back, so a narrowed search is one request instead of
// two and cannot return a row it then has to discard.
func (c *Capability) Search(ctx context.Context, req v1.SearchRequest) (v1.SearchResponse, error) {
	previews, err := c.client.Search(ctx, req.Text, nativeTypesFor(req.MediaType))
	if err != nil {
		return v1.SearchResponse{}, fmt.Errorf("search Cinemeta: %w", err)
	}

	results := make([]v1.SearchResult, 0, len(previews))
	for _, p := range previews {
		if req.Limit > 0 && len(results) >= req.Limit {
			break
		}
		results = append(results, v1.SearchResult{
			Ref: refFrom(p), Title: p.Title, Year: p.Year, Poster: p.Poster,
		})
	}
	return v1.SearchResponse{Results: results}, nil
}

// Catalogs lists the collections this module exposes (RoleCatalog). The set is
// fixed rather than fetched: Cinemeta declares its catalogs in a manifest, and
// half of them require an argument a browse surface addressing a catalog by id
// alone has no way to supply, so which of them are usable is a decision that
// belongs at this boundary rather than being rediscovered per request.
func (c *Capability) Catalogs(ctx context.Context, req v1.CatalogsRequest) (v1.CatalogsResponse, error) {
	decls := c.client.Catalogs()
	catalogs := make([]v1.Catalog, 0, len(decls))
	for _, d := range decls {
		catalogs = append(catalogs, v1.Catalog{ID: d.ID, NativeType: d.Type, Name: d.Name})
	}
	return v1.CatalogsResponse{Catalogs: catalogs}, nil
}

// CatalogItems lists one collection's entries as virtual candidates (ADR 0028).
// It touches no part of the object graph: browsing a source must not flood the
// library with everything the source knows about, and Cinemeta knows about
// everything.
func (c *Capability) CatalogItems(ctx context.Context, req v1.CatalogItemsRequest) (v1.CatalogItemsResponse, error) {
	previews, err := c.client.CatalogItems(ctx, req.CatalogID, req.NativeType, req.Skip)
	if err != nil {
		return v1.CatalogItemsResponse{}, fmt.Errorf("list Cinemeta catalog items: %w", err)
	}

	items := make([]v1.CatalogItem, 0, len(previews))
	for _, p := range previews {
		items = append(items, v1.CatalogItem{
			Ref: refFrom(p), Title: p.Title, Year: p.Year, Poster: p.Poster,
		})
	}
	return v1.CatalogItemsResponse{Items: items}, nil
}
