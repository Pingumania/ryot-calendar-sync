// Command ryot-calendar-sync exposes upcoming releases tracked in a
// self-hosted Ryot instance (https://github.com/IgnisDa/ryot) -- video games
// and movies coming out, episodes of shows airing -- as a subscribable .ics
// calendar feed, so Google Calendar / Apple Calendar / Outlook can sync them
// directly without a browser extension or manual exports.
package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"ryot-calendar-sync/internal/ical"
	"ryot-calendar-sync/internal/ryot"
)

// feedCache holds the last successfully fetched release list so requests do
// not have to wait on Ryot, and so a transient Ryot outage does not take the
// whole feed down. It caches releases rather than rendered .ics bytes because
// each request may ask for a different subset of media types; rendering a few
// hundred events per request is far cheaper than the Ryot round trips.
type feedCache struct {
	mu          sync.RWMutex
	releases    []ryot.Release
	generatedAt time.Time
}

func (c *feedCache) get() ([]ryot.Release, time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.releases, c.generatedAt
}

func (c *feedCache) set(releases []ryot.Release) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.releases = releases
	c.generatedAt = time.Now()
}

// refresher owns everything a periodic feed refresh needs, so it doesn't
// have to travel as a separate (cfg, client, cache) tuple through every
// function that touches it.
type refresher struct {
	cfg    config
	client *ryot.Client
	cache  *feedCache
}

func (rf *refresher) refresh(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	releases, err := rf.client.UpcomingReleases(ctx, rf.cfg.LookaheadDays, rf.cfg.MaxEvents, rf.cfg.Concurrency, rf.cfg.MediaTypes)
	if err != nil {
		// Keep serving whatever was cached before rather than emptying the feed.
		log.Printf("refresh failed, keeping previous feed: %v", err)
		return
	}
	rf.cache.set(releases)
	log.Printf("refreshed feed: %d upcoming release(s) (%s)", len(releases), countByLot(releases))
}

// loop refreshes on cfg.CacheTTL until ctx is cancelled.
func (rf *refresher) loop(ctx context.Context) {
	ticker := time.NewTicker(rf.cfg.CacheTTL)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rf.refresh(ctx)
		}
	}
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	rf := &refresher{
		cfg:    cfg,
		client: ryot.New(cfg.RyotBaseURL, cfg.RyotToken, 20*time.Second),
		cache:  &feedCache{},
	}

	// Populate the cache once before serving so the first request isn't slow
	// or, worse, empty because of a race with the background refresher.
	rf.refresh(ctx)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		rf.loop(ctx)
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz(rf.cache))
	mux.HandleFunc("/calendar.ics", handleCalendar(cfg, rf.cache))

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Reported on this channel instead of log.Fatalf so a listen failure goes
	// through the same graceful-shutdown path as SIGINT/SIGTERM, rather than
	// os.Exit-ing straight out of a background goroutine.
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("ryot-calendar-sync listening on %s (feed: /calendar.ics?token=***, media types: %s)",
			cfg.ListenAddr, strings.Join(lotStrings(cfg.MediaTypes), ","))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if err != nil {
			log.Printf("server error: %v", err)
		}
	}
	stop() // also unblocks rf.loop if we got here via serverErr rather than a signal

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	wg.Wait()
}

func handleCalendar(cfg config, cache *feedCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.SyncToken)) != 1 {
			http.Error(w, "not found", http.StatusNotFound) // don't hint that a token exists
			return
		}

		// ?type=show, or ?type=movie,video_game -- narrows the feed to one calendar
		// per media type. Absent, the feed is everything this service collects.
		lots := cfg.MediaTypes
		if raw := r.URL.Query().Get("type"); raw != "" {
			var err error
			if lots, err = parseLots(raw, cfg.MediaTypes); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		releases, generatedAt := cache.get()
		if generatedAt.IsZero() {
			http.Error(w, "feed not available yet, try again shortly", http.StatusServiceUnavailable)
			return
		}

		body := ical.Build(calendarName(cfg.CalName, lots, cfg.MediaTypes), icalEvents(releases, lots))

		w.Header().Set("Content-Type", ical.ContentType)
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Last-Modified", generatedAt.UTC().Format(http.TimeFormat))
		w.Header().Set("Content-Disposition", `inline; filename="ryot-upcoming.ics"`)
		_, _ = w.Write(body)
	}
}

// handleHealthz reports local readiness only: whether the feed cache has been
// populated by at least one successful refresh. It deliberately does not call
// out to Ryot -- this endpoint is unauthenticated (Docker's HEALTHCHECK polls
// it every 30s, and it's reachable on the published port) so making it hit
// Ryot would let every poll, or anyone on the port, trigger a Ryot API call.
// A bad RYOT_BASE_URL/RYOT_API_TOKEN still surfaces here indirectly: refresh
// logs the failure and the cache never gets its first populate.
func handleHealthz(cache *feedCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, generatedAt := cache.get(); generatedAt.IsZero() {
			http.Error(w, "feed not populated yet", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}
