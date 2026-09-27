# Curio

A self-hosted curiosity break. Pick a place, a fact, or a surprise. Take a quick guess, reveal the answer, then decide whether you want the story or another round.

**Developed using AI.** OpenAI Codex helps build Curio’s code, UI, tests, and documentation. Running Curio requires no AI service or AI API key.

Curio runs as one Go server with SQLite, server-rendered HTML, and local assets. It needs one Docker container and one persistent volume.

## Start with Docker

```sh
git clone git@github.com:Venkatpandey/curio.git
cd curio
cp .env.example .env
# Edit .env and set CURIO_ACCESS_PASSWORD to your household password.
docker compose up -d --build
```

Open [localhost:8090](http://localhost:8090), or `http://<your-nas-ip>:8090` from your home network.

Enter a username and the shared household password. A new name creates a profile; the same name reopens it. Usernames are case-insensitive. There is no signup page, email, or per-user password. Curio remembers your session for 30 days. “Leave this profile” signs out.

Set the preset password in `.env`, which Docker Compose passes as `CURIO_ACCESS_PASSWORD`. You can also replace the variable in `docker-compose.yml` with a quoted password. Keep real passwords out of Git. Changing the password and recreating the container invalidates old sessions.

Profiles have separate settings, but the shared password is household access: anyone who knows it can open any username. Guest browsing is enabled by default; set `CURIO_GUEST_ENABLED=false` if all access should require the password.

The named volume `curio-data` holds `/data/curio.db` and SQLite WAL files. Docker initializes its ownership for UID 10001. Do not use `docker compose down -v` unless you intend to delete all stored data.

## Run on a NAS

Copy [deploy/nas/compose.yml](deploy/nas/compose.yml) to a folder on your NAS as `compose.yml`. Replace its example household password, then run:

```sh
docker compose pull
docker compose up -d
```

Open `http://<NAS-IP>:8090` on your phone or computer. This file pulls the public `edge` image, needs no source checkout or build tools, and stores data in a named Docker volume. It requires a username and the same shared password for each profile; set `CURIO_GUEST_ENABLED` to `"true"` to allow guest browsing.

Leave `CURIO_BASE_URL` empty for direct LAN HTTP. If you use a reverse proxy, set it to the exact browser origin, such as `https://curio.example.com`, including any non-default port and no path. Compose treats `$` as interpolation; write `$$` for a literal dollar sign in an inline password. Keep your edited file private.

## Current build: Daily editions and varied play

- Simple username plus shared Compose password; separate profile settings and sessions.
- Dark reading layout with real photographs, source links, photo credits and licences.
- Fifty bundled discoveries: six illustrated long reads and 44 short, sourced true-or-false and comparison rounds.
- A daily cover and three pinned picks per profile, with completion markers and background freshness notices.
- Distinct Places, Facts, Surprise, and Collection layouts, topic palettes, and reduced-motion-aware reveals.
- A background Wikimedia importer for a catalogue of 24 geographical places. It fetches article excerpts, coordinates and licensed Commons photographs, then stores them in SQLite.
- Persistent per-user seen tracking: show unseen content first, then sample from older stories. Guests keep a bounded recent list in a cookie.
- Stable discovery links, maps, and approximate straight-line distance from an optional saved home location.
- Photo quizzes, sourced answer reveals, and optional 1–2 minute reads. Guests can play without a profile.
- Space and Animals fact filters, with saved Interesting / Not for me reactions.
- Per-profile Curiosity Points, a daily three-discovery challenge, and milestone badges.
- Per-profile favorites and recent-history pages, with 12 stories per page.
- A visible, resettable topic mix with bounded weights and a 25% exploration branch.
- Non-root Docker image, health endpoint, tests, and CI/release workflow definitions.

“Surprise Me” mixes photo mysteries, true-or-false claims, and two-choice comparisons. Facts cover Animals, History, Nature, Science, Space, and Technology; topic filters come from the available content. Interests and reactions shape recommendations. External fact providers and themed journeys remain future work. See [the roadmap](docs/roadmap.md).

The importer adds one place at a time, with at least ten seconds between entries. It refreshes cached articles after seven days and backs off on provider errors. Some catalogue entries may be skipped when their article has too little text, no geographic coordinates, or no suitable licensed photograph. This is a bounded location catalogue, not an unrestricted random-article feed.

Every reading page includes its sources. Short fact cards link to NASA, NOAA, USGS, CERN, or the National Park Service and carry a review date; they use typographic artwork rather than unrelated photographs. Live article excerpts retain Wikipedia wording, identify the retrieval date, and link the CC BY-SA 4.0 licence. Editorial stories identify their review date. Photos carry separate creator/licence links; telescope and spacecraft pictures explain what the image represents. [Bundled photo credits](web/static/photos/ATTRIBUTION.md) document the assets distributed with the app.

Curio serves photos locally. The browser makes no requests to Wikimedia during ordinary reading. The server contacts Wikimedia in the background; set `CURIO_WIKIMEDIA_ENABLED=false` to disable this. Existing cached stories and photographs, plus the bundled collection, remain available without internet access.

## Local development

Go 1.27.1 or later is required. No Node.js or frontend build step is needed.

```sh
cp .env.example .env
# Set your household password, then load the environment:
set -a
. ./.env
set +a
CURIO_DATA_DIR=./data CURIO_PORT=8080 go run ./cmd/curio
```

Open [localhost:8080](http://localhost:8080).

```sh
go test -race ./...
go vet ./...
go build -o bin/curio ./cmd/curio
```

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `CURIO_ACCESS_PASSWORD` | Required | Shared household password, 12–128 bytes. |
| `CURIO_PORT` | `8080` | Server port inside the container. |
| `CURIO_HOST_PORT` | `8090` | Published port in Compose. |
| `CURIO_DATA_DIR` | `/data` | SQLite storage directory. |
| `CURIO_BASE_URL` | Empty | Public origin, e.g. `https://curio.example.com`. HTTPS enables Secure cookies and HSTS. |
| `CURIO_GUEST_ENABLED` | `true` | Allow unsigned visitors to browse discoveries. |
| `CURIO_WIKIMEDIA_ENABLED` | `true` | Fetch and refresh place stories in the background. |
| `CURIO_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`. |
| `TZ` | `Europe/Berlin` in Compose | Container time zone. |
| `CURIO_IMAGE` | `ghcr.io/venkatpandey/curio:latest` | Container image name for Compose. |

For access beyond a trusted LAN, terminate HTTPS at your reverse proxy and set `CURIO_BASE_URL` to that HTTPS origin. Curio explicitly trusts the configured base origin for form submissions, including proxies that rewrite Host. It does not trust arbitrary forwarded headers when checking origins, deciding cookie security, or determining rate-limit identity. Authentication limits apply to the proxy address if you put every request behind one proxy.

PWA installation and service workers need HTTPS or localhost. Plain HTTP on a LAN IP supports normal browsing but does not provide full PWA installation. The service worker caches a generic offline page, not private account pages. When the server loses internet access, locally bundled content still works. When the browser cannot reach the server, it shows a reconnect page.

## Health and operations

`GET /health` returns status and application version. Docker checks it using `curio healthcheck`.

For backup, stop Curio and archive the entire named volume, including any SQLite sidecar files. For restore, stop Curio and restore that volume with UID/GID 10001 ownership. Do not copy only the live `.db` file while WAL mode is active. See [operations](docs/operations.md) for commands.

GitHub Actions checks formatting, module integrity, race tests, vet, Chromium/WebKit browser flows, and Docker builds for `linux/amd64` and `linux/arm64`. Pull requests run checks without publishing. Passing `main` builds publish `ghcr.io/venkatpandey/curio:edge` and a `sha-<commit>` tag. Version tags such as `v0.1.0` rerun the same checks before publishing versioned images; stable versions also update `latest`. Prerelease tags do not update `latest`. No personal registry token is required: publishing uses the repository’s `GITHUB_TOKEN`. The initial GHCR package may need its visibility changed to public for anonymous pulls.

For the current development image, set `CURIO_IMAGE=ghcr.io/venkatpandey/curio:edge` in `.env`. Use `latest` or a version tag after a release exists:

```sh
docker compose pull
docker compose up -d --no-build
```

Architecture and schema decisions live in [docs/architecture.md](docs/architecture.md). Code uses the MIT license. Bundled photographs and cached Wikipedia excerpts retain their separately stated licences.

## Points and the daily trio

A first reveal earns 1 Curiosity Point. Taking a guess earns 2 more, whether correct or wrong. Each story earns points once per profile; replaying, refreshing, or changing an answer cannot award more. Skipping locks in the 1-point reveal. Guests can guess and read, but do not save scores or reactions.

Reveal three previously uncompleted stories for the daily trio. The day follows the server’s `TZ` setting. Total points and badges persist across restarts. Badges unlock at the first discovery, ten discoveries, and three guesses. There are no timers, streak penalties, or rewards for time spent. The bounded catalogue means new daily discoveries eventually run out until more content arrives.

## Your collection and mix

Use “Keep this one” to save a discovery. “My collection” holds favorites, recent detours, and your topic mix. These views belong to the signed-in profile and work without JavaScript. Removing a favorite does not remove its history or points.

Selection prefers unseen stories, then samples from the older half of eligible history. Categories get a baseline weight of 100, plus 50 for a chosen interest, plus 25 per Interesting reaction and minus 25 per Not for me reaction. Weights stay between 25 and 300. Three quarters of draws use those weights; one quarter chooses uniformly among available categories below the strongest weight, or all categories when tied. A story is then selected uniformly from the chosen category. Topic filters and available unseen content bound the pool. This prevents the larger place catalogue from overwhelming smaller fact categories.

“Reset my mix” clears chosen interests and ignores previous reactions when calculating weights. It keeps reaction records, favorites, history, points, and badges. Changing a reaction after reset makes that reaction count again.

## Daily editions and freshness

The homepage chooses one place and two facts on the first visit of each day, using the server’s `TZ` setting. Picks stay fixed for that profile through refreshes, scoring, new imports, and restarts. Selection prefers uncompleted stories, avoids the previous edition when possible, and mixes fact categories. Guest visitors share a public daily edition. Opening the homepage does not mark its previews as seen.

The homepage checks for changes on return and every five minutes while visible. It shows a link to the latest edition instead of changing content beneath the reader. The checkbox pauses checks; that preference is the only new localStorage value. Catalogue checks require the same access as browsing and return no profile data. Network failures leave the current page usable. Daily picks and quizzes also work without JavaScript.

“New since your last homepage visit” compares the current catalogue size with an HttpOnly browser cookie. Re-fetching existing Wikimedia articles and restarting the app do not count as new content. Today's pinned picks remain the same if content arrives during the day; Surprise Me can select the new arrivals. The importer still uses its bounded 24-place catalogue, and the bundled fact pack grows through app updates. This release does not add an external fact feed, scheduled editorial publishing, or themed journeys.
