// Package ryot is a minimal GraphQL client for a self-hosted Ryot
// (https://github.com/IgnisDa/ryot) instance. It only implements the two
// queries this sync tool needs: fetching a user's upcoming calendar events
// and looking up metadata details for a single item.
package ryot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to a Ryot instance's GraphQL endpoint.
//
// Requests are authenticated with:
//
//	Authorization: Bearer <api-token>
//
// The token is generated from within Ryot itself (Settings -> your profile
// -> API Keys).
type Client struct {
	baseURL    string
	apiToken   string
	httpClient *http.Client
}

// New creates a Client. baseURL is the root URL of the Ryot instance, e.g.
// "http://ryot:8000" (container-to-container) or "https://ryot.example.com".
// The GraphQL endpoint is always "<baseURL>/backend/graphql".
func New(baseURL, apiToken string, timeout time.Duration) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		apiToken: apiToken,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

type gqlRequestBody struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type gqlError struct {
	Message string `json:"message"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlError      `json:"errors,omitempty"`
}

// do executes a GraphQL operation and unmarshals the "data" field into out.
func (c *Client) do(ctx context.Context, query string, variables map[string]any, out any) error {
	body, err := json.Marshal(gqlRequestBody{Query: query, Variables: variables})
	if err != nil {
		return fmt.Errorf("encode graphql request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/backend/graphql", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request ryot: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ryot returned HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}

	var gr gqlResponse
	if err := json.Unmarshal(raw, &gr); err != nil {
		return fmt.Errorf("decode graphql envelope: %w", err)
	}
	if len(gr.Errors) > 0 {
		msgs := make([]string, 0, len(gr.Errors))
		for _, e := range gr.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("ryot graphql error(s): %s", strings.Join(msgs, "; "))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(gr.Data, out); err != nil {
		return fmt.Errorf("decode graphql data: %w", err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// CalendarEvent mirrors Ryot's GraphqlCalendarEvent type.
type CalendarEvent struct {
	Date            string  `json:"date"` // YYYY-MM-DD
	MetadataID      string  `json:"metadataId"`
	CalendarEventID string  `json:"calendarEventId"`
	MetadataImage   *string `json:"metadataImage"`

	// Non-nil only for shows.
	ShowExtraInformation *ShowExtra `json:"showExtraInformation"`
}

// ShowExtra identifies which episode of a tracked show an event is for.
// Ryot declares both fields non-null for shows.
type ShowExtra struct {
	Season  int `json:"season"`
	Episode int `json:"episode"`
}

const upcomingCalendarEventsQuery = `
query UpcomingCalendarEvents($input: UserUpcomingCalendarEventInput!) {
  userUpcomingCalendarEvents(input: $input) {
    date
    metadataId
    calendarEventId
    metadataImage
    showExtraInformation { season episode }
  }
}`

// UpcomingCalendarEvents returns every upcoming release Ryot knows about for
// items the authenticated user is monitoring, across ALL media types.
// Callers must filter by lot themselves via MetadataDetails.
func (c *Client) UpcomingCalendarEvents(ctx context.Context, nextDays int) ([]CalendarEvent, error) {
	var data struct {
		UserUpcomingCalendarEvents []CalendarEvent `json:"userUpcomingCalendarEvents"`
	}
	variables := map[string]any{
		"input": map[string]any{
			"nextDays": nextDays,
		},
	}
	if err := c.do(ctx, upcomingCalendarEventsQuery, variables, &data); err != nil {
		return nil, err
	}
	return data.UserUpcomingCalendarEvents, nil
}

// MediaLot identifies which kind of media a Ryot item is.
type MediaLot string

// The MediaLot enum values this tool can present. Ryot also defines
// ANIME, AUDIO_BOOK, BOOK, COMIC_BOOK, MANGA, MUSIC, PODCAST and
// VISUAL_NOVEL.
const (
	MediaLotVideoGame MediaLot = "VIDEO_GAME"
	MediaLotMovie     MediaLot = "MOVIE"
	MediaLotShow      MediaLot = "SHOW"
)

// MetadataDetails mirrors the fields of Ryot's GraphqlMetadataDetails type
// that this tool actually uses.
type MetadataDetails struct {
	Title       string   `json:"title"`
	Lot         MediaLot `json:"lot"`
	SourceURL   *string  `json:"sourceUrl"`
	Description *string  `json:"description"`
	PublishDate *string  `json:"publishDate"`
}

const metadataDetailsQuery = `
query MetadataDetails($metadataId: String!) {
  metadataDetails(metadataId: $metadataId) {
    response {
      title
      lot
      sourceUrl
      description
      publishDate
    }
  }
}`

// MetadataDetails looks up a single metadata item (a game, movie, book, ...)
// by its Ryot-internal metadata ID.
func (c *Client) MetadataDetails(ctx context.Context, metadataID string) (*MetadataDetails, error) {
	var data struct {
		MetadataDetails struct {
			Response MetadataDetails `json:"response"`
		} `json:"metadataDetails"`
	}
	variables := map[string]any{"metadataId": metadataID}
	if err := c.do(ctx, metadataDetailsQuery, variables, &data); err != nil {
		return nil, err
	}
	return &data.MetadataDetails.Response, nil
}
