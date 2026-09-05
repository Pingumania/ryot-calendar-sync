package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"ryot-calendar-sync/internal/ryot"
)

type config struct {
	RyotBaseURL   string
	RyotToken     string
	SyncToken     string
	ListenAddr    string
	LookaheadDays int
	MaxEvents     int
	CacheTTL      time.Duration
	Concurrency   int
	CalName       string
	MediaTypes    []ryot.MediaLot // Ryot lots to include, in canonical MediaLot form
}

func loadConfig() (config, error) {
	cfg := config{
		RyotBaseURL: os.Getenv("RYOT_BASE_URL"),
		RyotToken:   os.Getenv("RYOT_API_TOKEN"),
		SyncToken:   os.Getenv("SYNC_TOKEN"),
		ListenAddr:  getEnvDefault("LISTEN_ADDR", ":8090"),
		CalName:     getEnvDefault("CALENDAR_NAME", "Ryot: Upcoming Releases"),
	}

	var err error
	cfg.LookaheadDays, err = getEnvIntDefault("LOOKAHEAD_DAYS", 180)
	if err != nil {
		return cfg, err
	}
	cfg.MaxEvents, err = getEnvIntDefault("MAX_EVENTS", 250)
	if err != nil {
		return cfg, err
	}
	cfg.Concurrency, err = getEnvIntDefault("FETCH_CONCURRENCY", 8)
	if err != nil {
		return cfg, err
	}
	ttlMinutes, err := getEnvIntDefault("CACHE_TTL_MINUTES", 15)
	if err != nil {
		return cfg, err
	}
	cfg.CacheTTL = time.Duration(ttlMinutes) * time.Minute

	cfg.MediaTypes, err = parseLots(getEnvDefault("MEDIA_TYPES", "VIDEO_GAME,MOVIE,SHOW"), nil)
	if err != nil {
		return cfg, fmt.Errorf("MEDIA_TYPES: %w", err)
	}

	if cfg.RyotBaseURL == "" {
		return cfg, errors.New("RYOT_BASE_URL is required (e.g. http://ryot:8000)")
	}
	if cfg.RyotToken == "" {
		return cfg, errors.New("RYOT_API_TOKEN is required (generate one in Ryot: Settings -> Profile -> API Keys)")
	}
	if cfg.SyncToken == "" {
		return cfg, errors.New("SYNC_TOKEN is required: pick a long random secret, it protects your feed URL since calendar apps poll it over plain HTTP(S) with no login")
	}
	return cfg, nil
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvIntDefault(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q: %w", key, v, err)
	}
	return n, nil
}

// lotAliases maps a MEDIA_TYPES / ?type= value to Ryot's canonical MediaLot value.
var lotAliases = map[string]ryot.MediaLot{
	"VIDEO_GAME": ryot.MediaLotVideoGame,
	"MOVIE":      ryot.MediaLotMovie,
	"SHOW":       ryot.MediaLotShow,
}

// lotStrings renders lots back to their raw MediaLot values, e.g. for
// listing what a feed collects in an error or log message.
func lotStrings(lots []ryot.MediaLot) []string {
	s := make([]string, len(lots))
	for i, lot := range lots {
		s[i] = string(lot)
	}
	return s
}

// parseLots turns a comma-separated list of media types into canonical
// MediaLot values, rejecting anything unrecognised. When allowed is non-nil
// the result must also be a subset of it.
func parseLots(raw string, allowed []ryot.MediaLot) ([]ryot.MediaLot, error) {
	lots := make([]ryot.MediaLot, 0, 4)
	seen := make(map[ryot.MediaLot]bool, 4)
	for _, field := range strings.Split(raw, ",") {
		name := strings.ToUpper(strings.TrimSpace(field))
		if name == "" {
			continue
		}
		lot, ok := lotAliases[name]
		if !ok {
			return nil, fmt.Errorf("unknown media type %q (valid: video_game, movie, show)", field)
		}
		if allowed != nil && !slices.Contains(allowed, lot) {
			return nil, fmt.Errorf("media type %q is not collected by this feed (it collects: %s)", field, strings.Join(lotStrings(allowed), ", "))
		}
		if !seen[lot] {
			seen[lot] = true
			lots = append(lots, lot)
		}
	}
	if len(lots) == 0 {
		return nil, errors.New("no media types given")
	}
	return lots, nil
}
