package ryot

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Release is the fully resolved, calendar-ready view of a single upcoming
// release: a game or movie coming out, or one episode of a show airing.
type Release struct {
	CalendarEventID string
	MetadataID      string
	Title           string
	Lot             MediaLot
	Date            time.Time
	Description     string
	SourceURL       string

	// Season and Episode are set only for the installment-based lots, and
	// identify which installment this event is. Season is meaningful for
	// MediaLotShow; Episode is 0 when Ryot doesn't know the number.
	Season  int
	Episode int
}

// dateLayout matches Ryot's NaiveDate scalar (YYYY-MM-DD).
const dateLayout = "2006-01-02"

// UpcomingReleases fetches every upcoming calendar event the user is
// monitoring, resolves each one's metadata, and returns the entries whose
// lot appears in lots (an empty lots means every lot), sorted by date.
//
// Lookups are done concurrently (bounded by concurrency) since Ryot's
// calendar query does not embed titles or media type -- each event needs its
// own metadataDetails round trip, which is also the only way to know an
// event's lot, so filtering cannot be pushed into the query.
//
// maxEvents caps how many of Ryot's returned events are considered at all.
// It's enforced entirely client-side: Ryot's query input is a oneof, so it
// cannot also be told nextMedia alongside nextDays.
func (c *Client) UpcomingReleases(ctx context.Context, nextDays, maxEvents, concurrency int, lots []MediaLot) ([]Release, error) {
	events, err := c.UpcomingCalendarEvents(ctx, nextDays)
	if err != nil {
		return nil, fmt.Errorf("fetch upcoming calendar events: %w", err)
	}
	if maxEvents > 0 && len(events) > maxEvents {
		events = events[:maxEvents]
	}
	if len(events) == 0 {
		return nil, nil
	}
	if concurrency < 1 {
		concurrency = 1
	}

	wanted := make(map[MediaLot]bool, len(lots))
	for _, lot := range lots {
		wanted[lot] = true
	}

	// One slot per event, so the workers never need to share anything.
	type result struct {
		release Release
		keep    bool
		err     error
	}
	results := make([]result, len(events))

	// Acquiring before the `go` statement bounds live goroutines, not just
	// in-flight requests.
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, ev := range events {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[i].release, results[i].keep, results[i].err = c.release(ctx, ev, wanted)
		}()
	}
	wg.Wait()

	releases := make([]Release, 0, len(events))
	var firstErr error
	for _, r := range results {
		switch {
		case r.err != nil:
			if firstErr == nil {
				firstErr = r.err
			}
		case r.keep:
			releases = append(releases, r.release)
		}
	}
	// A handful of individual metadata lookups failing shouldn't take down
	// the whole feed, but if literally everything failed, surface it.
	if firstErr != nil && len(releases) == 0 {
		return nil, firstErr
	}

	// Ryot returns events in date order, but a feed mixing lots reads better
	// with a total order, and a stable one keeps the .ics byte-identical
	// between refreshes when nothing has changed.
	sort.Slice(releases, func(i, j int) bool {
		a, b := releases[i], releases[j]
		switch {
		case !a.Date.Equal(b.Date):
			return a.Date.Before(b.Date)
		case a.Title != b.Title:
			return a.Title < b.Title
		case a.Season != b.Season:
			return a.Season < b.Season
		default:
			return a.Episode < b.Episode
		}
	})
	return releases, nil
}

// release resolves a single calendar event. A false keep means the event is
// for a lot the caller didn't ask for, which is expected rather than an
// error: Ryot's calendar mixes all media types together.
func (c *Client) release(ctx context.Context, ev CalendarEvent, wanted map[MediaLot]bool) (rel Release, keep bool, err error) {
	details, err := c.MetadataDetails(ctx, ev.MetadataID)
	if err != nil {
		return Release{}, false, fmt.Errorf("metadata %s: %w", ev.MetadataID, err)
	}
	if len(wanted) > 0 && !wanted[details.Lot] {
		return Release{}, false, nil
	}
	date, err := time.Parse(dateLayout, ev.Date)
	if err != nil {
		return Release{}, false, fmt.Errorf("parse date %q for %s: %w", ev.Date, ev.MetadataID, err)
	}

	rel = Release{
		CalendarEventID: ev.CalendarEventID,
		MetadataID:      ev.MetadataID,
		Title:           details.Title,
		Lot:             details.Lot,
		Date:            date,
		Description:     orEmpty(details.Description),
		SourceURL:       orEmpty(details.SourceURL),
	}
	if ev.ShowExtraInformation != nil {
		rel.Season = ev.ShowExtraInformation.Season
		rel.Episode = ev.ShowExtraInformation.Episode
	}
	return rel, true, nil
}

// orEmpty flattens one of Ryot's nullable string fields.
func orEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
