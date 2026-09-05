# ryot-calendar-sync

Exposes a `.ics` calendar feed of upcoming releases (movies, shows, games)
tracked in [Ryot](https://github.com/IgnisDa/ryot).

## Setup

1. `docker-compose.yml`:
   ```yaml
   services:
     ryot-calendar-sync:
       image: ghcr.io/pingumania/ryot-calendar-sync:latest
       restart: unless-stopped
       environment:
         - RYOT_BASE_URL=http://ryot:8000
         - RYOT_API_TOKEN=your-ryot-api-token
         - SYNC_TOKEN=your-sync-token
       ports:
         - "8090:8090"
   ```
1. `docker compose up -d`

## Env variables

| Variable             | Required | Default                    | Description                                  |
|-----------------------|----------|-----------------------------|-----------------------------------------------|
| `RYOT_BASE_URL`       | yes      | -                            | Root URL of your Ryot instance                |
| `RYOT_API_TOKEN`      | yes      | -                            | Ryot personal API token                       |
| `SYNC_TOKEN`          | yes      | -                            | Secret required as `?token=` on the feed URL  |
| `LISTEN_ADDR`         | no       | `:8090`                      | Server listen address                         |
| `LOOKAHEAD_DAYS`      | no       | `180`                        | Days ahead to query Ryot for                  |
| `MAX_EVENTS`          | no       | `250`                        | Cap on items fetched from Ryot before filtering |
| `MEDIA_TYPES`         | no       | `VIDEO_GAME,MOVIE,SHOW`      | Media types included in the feed              |
| `CACHE_TTL_MINUTES`   | no       | `15`                         | How often the feed refreshes from Ryot        |
| `FETCH_CONCURRENCY`   | no       | `8`                          | Concurrent metadata lookups per refresh       |
| `CALENDAR_NAME`       | no       | `Ryot: Upcoming Releases`    | Base calendar name; `&type=` feeds get it auto-suffixed, e.g. `(Movie)` |

## Subscribing

Calendar apps poll `.ics` URLs with no login, so the token goes in the query
string instead. Add `&type=` to narrow to one or more media types -- each type
gets its own feed URL.

```
https://ryot-calendar.example.com/calendar.ics?token=<SYNC_TOKEN>                    # everything
https://ryot-calendar.example.com/calendar.ics?token=<SYNC_TOKEN>&type=movie
https://ryot-calendar.example.com/calendar.ics?token=<SYNC_TOKEN>&type=show
https://ryot-calendar.example.com/calendar.ics?token=<SYNC_TOKEN>&type=video_game
```

Each `type=` feed is auto-named from `CALENDAR_NAME` plus the type, e.g.
`Ryot: Upcoming Releases (Movie)`, so calendar apps show them as distinct
subscriptions. `CALENDAR_NAME` itself is one global base name set at startup --
there's no way to give arbitrary custom names per URL.

## Health check

`GET /healthz` reports whether the feed has been populated by at least one
successful refresh. It takes no token -- Docker's `HEALTHCHECK` polls it
every 30s, and it's reachable on the published port -- and it never calls out
to Ryot itself, so a poll (or anyone else hitting the port) doesn't cost a
Ryot API call.
