package cinemeta

import "testing"

// The translation unit tests. Each one pins a Cinemeta-ism that has to stop at
// this boundary (ADR 0051), and each case below is a shape the live service
// actually returns rather than one imagined for the test.

func TestParseYearReadsARangeWithAnEnDash(t *testing.T) {
	for _, tc := range []struct {
		releaseInfo string
		want        int
	}{
		{"1999", 1999},
		// The separator is U+2013, not a hyphen. Anything splitting on "-" reads
		// this as one long token and reports no year at all.
		{"2005–2008", 2005},
		{"2008–", 2008},
		{"", 0},
		{"soon", 0},
		{"20", 0},
	} {
		if got := parseYear(tc.releaseInfo); got != tc.want {
			t.Errorf("parseYear(%q) = %d, want %d", tc.releaseInfo, got, tc.want)
		}
	}
}

func TestParseRatingReadsTheStringForm(t *testing.T) {
	for _, tc := range []struct {
		rating string
		want   float64
	}{
		{"8.7", 8.7},
		{"10", 10},
		// Absent is zero, which is how the SDK spells "the source did not say" —
		// not an error, because most unreleased titles have no rating.
		{"", 0},
		{"N/A", 0},
	} {
		if got := parseRating(tc.rating); got != tc.want {
			t.Errorf("parseRating(%q) = %v, want %v", tc.rating, got, tc.want)
		}
	}
}

func TestPosterURLUpgradesOnlyTheKnownCDN(t *testing.T) {
	if got := posterURL("https://images.metahub.space/poster/small/tt0133093/img"); got != "https://images.metahub.space/poster/medium/tt0133093/img" {
		t.Errorf("metahub poster = %q, want the medium size", got)
	}
	// A poster from anywhere else is passed through untouched. Search results
	// come back with Amazon URLs, and a substitution applied to those produces a
	// link to nothing — which is worse than a soft image and much harder to spot.
	const amazon = "https://m.media-amazon.com/images/M/matrix._V1_SX250.jpg"
	if got := posterURL(amazon); got != amazon {
		t.Errorf("foreign poster = %q, want it unchanged", got)
	}
	if got := posterURL("https://images.metahub.space/poster/large/tt0133093/img"); got != "https://images.metahub.space/poster/large/tt0133093/img" {
		t.Errorf("an already-large poster = %q, want it unchanged", got)
	}
	if got := posterURL(""); got != "" {
		t.Errorf("empty poster = %q, want it to stay empty so absent stays distinguishable from broken", got)
	}
}

func TestCastPrefersTheLinksArrayAndDeduplicates(t *testing.T) {
	meta := rawMeta{
		// The flat list is not dependably in billing order; the links array is.
		Cast: []string{"Carrie-Anne Moss", "Keanu Reeves", "Hugo Weaving"},
		Links: []rawLink{
			{Name: "8.7", Category: "imdb"},
			{Name: "Keanu Reeves", Category: "Cast"},
			{Name: "Laurence Fishburne", Category: "Cast"},
			{Name: "Action", Category: "Genres"},
		},
	}

	got := castOf(meta)
	want := []string{"Keanu Reeves", "Laurence Fishburne", "Carrie-Anne Moss", "Hugo Weaving"}
	if len(got) != len(want) {
		t.Fatalf("cast = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cast = %v, want %v", got, want)
		}
	}
}

func TestCastFallsBackToTheFlatListAndCaps(t *testing.T) {
	var many []string
	for i := range maxCast + 10 {
		many = append(many, string(rune('A'+i%26))+string(rune('a'+i/26)))
	}
	if got := castOf(rawMeta{Cast: many}); len(got) != maxCast {
		t.Fatalf("cast = %d names, want it capped at %d", len(got), maxCast)
	}
	// No cast at all is nil rather than an empty slice, so a consumer can tell
	// "the source has none" without inspecting a length.
	if got := castOf(rawMeta{}); got != nil {
		t.Fatalf("cast = %v, want nil", got)
	}
}

func TestEpisodesAreOrderedAndNumberedWhicheverKeyCarriesIt(t *testing.T) {
	episodes := episodesOf([]rawVideo{
		{Season: 2, Episode: 1, Name: "Seven Thirty-Seven"},
		// `number` rather than `episode`, and a synopsis under `description`
		// rather than `overview` — both spellings occur in live records.
		{Season: 1, Number: 2, Name: "Cat's in the Bag...", Description: "The cleanup.", FirstAired: "2008-01-27"},
		{Season: 1, Episode: 1, Name: "Pilot", Overview: "A diagnosis.", Released: "2008-01-21"},
		{Season: 0, Episode: 1, Name: "Unaired Pilot"},
	})

	if len(episodes) != 4 {
		t.Fatalf("episodes = %d, want 4", len(episodes))
	}
	for i, want := range []struct {
		season, episode int
	}{{0, 1}, {1, 1}, {1, 2}, {2, 1}} {
		if episodes[i].Season != want.season || episodes[i].Episode != want.episode {
			t.Fatalf("episode %d = S%02dE%02d, want S%02dE%02d", i,
				episodes[i].Season, episodes[i].Episode, want.season, want.episode)
		}
	}
	if episodes[2].Overview != "The cleanup." {
		t.Errorf("overview = %q, want the description fallback read", episodes[2].Overview)
	}
	if episodes[2].Released != "2008-01-27" {
		t.Errorf("released = %q, want the firstAired fallback read", episodes[2].Released)
	}
	// An untitled episode is named by its number rather than left blank: an
	// unaired episode with no title is normal, and a blank row is not a screen.
	if got := episodeName(rawVideo{}, 7); got != "Episode 7" {
		t.Errorf("episodeName = %q, want a generated label", got)
	}
}

func TestSeasonZeroIsNamedSpecials(t *testing.T) {
	// Cinemeta files pilots, extras and specials under season 0, and a container
	// called "Season 0" reads as a bug rather than as a convention.
	if got := seasonTitle(0); got != "Specials" {
		t.Errorf("seasonTitle(0) = %q, want Specials", got)
	}
	if got := seasonTitle(3); got != "Season 3" {
		t.Errorf("seasonTitle(3) = %q, want Season 3", got)
	}
}

func TestGenresFallBackToTheSecondSpelling(t *testing.T) {
	// Cinemeta emits the same fact under `genres` and `genre`, and records exist
	// with only one of them populated.
	title := rawMeta{Genre: []string{"Crime", "Drama"}}.title(typeSeries)
	if len(title.Genres) != 2 || title.Genres[0] != "Crime" {
		t.Fatalf("genres = %v, want the singular key read as a fallback", title.Genres)
	}
}

func TestOnlyASeriesCarriesAnEpisodeList(t *testing.T) {
	// A film's meta has a `videos` array too — it holds trailers, which are not
	// episodes and must not become items in the tree.
	film := rawMeta{Type: typeMovie, Videos: []rawVideo{{Season: 1, Episode: 1, Name: "Trailer"}}}.title(typeMovie)
	if len(film.Episodes) != 0 {
		t.Fatalf("film episodes = %+v, want none", film.Episodes)
	}
}
