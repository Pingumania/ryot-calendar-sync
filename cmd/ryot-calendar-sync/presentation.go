package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"ryot-calendar-sync/internal/ical"
	"ryot-calendar-sync/internal/ryot"
)

// lotLabels are the plural human names used in refresh-log breakdowns, e.g.
// "Movies: 3".
var lotLabels = map[ryot.MediaLot]string{
	ryot.MediaLotVideoGame: "Games",
	ryot.MediaLotMovie:     "Movies",
	ryot.MediaLotShow:      "Shows",
}

// lotNames are the singular human names used in the per-type calendar name
// suffix, e.g. "Ryot: Upcoming Releases (Movie)".
var lotNames = map[ryot.MediaLot]string{
	ryot.MediaLotVideoGame: "Video Game",
	ryot.MediaLotMovie:     "Movie",
	ryot.MediaLotShow:      "Show",
}

func labelFor(lot ryot.MediaLot) string {
	if label, ok := lotLabels[lot]; ok {
		return label
	}
	return string(lot)
}

func nameFor(lot ryot.MediaLot) string {
	if name, ok := lotNames[lot]; ok {
		return name
	}
	return string(lot)
}

// countByLot renders a per-media-type breakdown for the refresh log line.
func countByLot(releases []ryot.Release) string {
	counts := make(map[ryot.MediaLot]int, len(lotLabels))
	for _, r := range releases {
		counts[r.Lot]++
	}
	if len(counts) == 0 {
		return "nothing upcoming"
	}
	parts := make([]string, 0, len(counts))
	for lot, n := range counts {
		parts = append(parts, fmt.Sprintf("%s: %d", labelFor(lot), n))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// icalEvents renders the cached releases whose lot is in lots.
func icalEvents(releases []ryot.Release, lots []ryot.MediaLot) []ical.Event {
	events := make([]ical.Event, 0, len(releases))
	for _, r := range releases {
		if !slices.Contains(lots, r.Lot) {
			continue
		}
		events = append(events, ical.Event{
			UID:         r.CalendarEventID,
			Summary:     summaryFor(r),
			Description: r.Description,
			URL:         r.SourceURL,
			Date:        r.Date,
		})
	}
	return events
}

// summaryFor titles an event.
func summaryFor(r ryot.Release) string {
	switch r.Lot {
	case ryot.MediaLotShow:
		return fmt.Sprintf("%s S%02dE%02d", r.Title, r.Season, r.Episode)
	default:
		return r.Title
	}
}

// calendarName keeps the filtered feeds distinguishable.
func calendarName(base string, lots, all []ryot.MediaLot) string {
	if len(lots) == len(all) {
		return base
	}
	names := make([]string, len(lots))
	for i, lot := range lots {
		names[i] = nameFor(lot)
	}
	return base + " (" + strings.Join(names, ", ") + ")"
}
