# Curio architecture

Curio runs one Go HTTP process with embedded HTML, CSS, JavaScript, and a local editorial collection. SQLite owns accounts, sessions, content, cached photographs and seen history. The container writes only to `/data` and starts without internet access.

## Phase 2 boundary

Phase 1 delivered shared-password username profiles, settings, sessions and the app shell. Phase 2 adds real photography, short reading sections, numeric fact panels, maps, home-distance estimates, stable discovery URLs, cached Wikimedia content and per-user seen history.

Six bundled editorial stories with licensed photographs and 44 sourced quick facts work from first startup. A serial background worker fills a catalogue of 24 additional places from Wikipedia and Wikimedia Commons. A provider failure never blocks startup or a discovery request. Facts use the local editorial provider. External fact providers and admin tools remain future work.

## Repository

- `cmd/curio`: configuration, process lifecycle, health-check command.
- `internal/config`: validated environment configuration.
- `internal/auth`: password hashing and random session tokens.
- `internal/store`: SQLite queries and ordered, transactional migrations.
- `internal/content`: normalized content, editorial stories, licensed photo metadata and the Wikimedia importer.
- `internal/httpapp`: HTTP routes, authentication, CSRF, forms, and templates.
- `web`: embedded templates and static assets. No frontend build step.
- `docs`: design decisions, operations, and delivery roadmap.

HTTP handlers validate input and enforce account ownership before calling the store. Store methods take explicit user IDs and use parameterized queries. Provider networking runs in a cancellable background worker outside request handlers. HTMX can enhance individual routes later; normal links and forms work without JavaScript now.

## Domain and schema

`User` contains an ID, case-insensitive unique username, and creation time. `Settings` contains a display name, appearance, optional home label and coordinate pair. `Session` contains only the SHA-256 digest of an opaque random token, its user ID, and an absolute expiry.

Migration 002 adds `content_items`, `media`, and `user_history`, with indexes for kind, article identity and recent per-user views. Discovery IDs and media digests are stable. An atomic cache update stores article data and its photograph together; replacing a photo removes unreferenced media. Migration 001 creates `users`, `sessions`, `user_settings`, `user_interests`, and `application_settings`. Foreign keys cascade when an account is deleted. Session expiry and user lookup have indexes. Migrations run in transactions and record their versions in `schema_migrations`; startup rejects schemas newer than this binary understands. WAL mode and a busy timeout support household use. A single DB connection keeps transactions predictable. Run one Curio replica per database.

`content.Item` carries ID, kind, title, summary, sections, category, facts, source links, photo credit/licence, optional coordinates, and review or fetch dates. Reading time uses the story word count at 220 words per minute. Editorial stories run 228–255 words. The importer keeps about 180–360 words of source text, ending on a sentence boundary. It rejects non-geographic or short articles, missing/unsupported image licences, suspicious image URLs, oversized responses and invalid image formats.

The catalogue is deliberately bounded. Wikimedia requests identify Curio with a contact/repository URL, use `maxlag=5`, apply a 12-second per-request timeout and a 35-second per-entry deadline, and refresh successful articles after seven days. The worker waits ten seconds between entries, exponentially backs off on failures, and respects Retry-After. Only HTTPS requests to the specified Wikimedia hosts can supply image bytes; redirects cannot escape the allowlist. Photos must decode as JPEG/PNG, fit the dimension limits and stay under 4 MiB. The image endpoint accepts a SHA-256 key, never a caller-supplied remote URL.

Discovery selection reads only SQLite. Signed-in users see unseen content first, then a weighted sample from the older half of eligible history. Guests keep their last 20 IDs in a host-only HttpOnly cookie. Opening a stable story URL records a view for the active user; reloading preserves the story. Settings use explicit user ownership, and one user’s history never selects another’s content. Distance uses the great-circle approximation, not a travel route.


For PostgreSQL later, preserve store method contracts and add a separate SQL implementation. SQLite-specific pragmas, migrations, placeholders, and constraints stay inside the store package. PostgreSQL is not a runtime dependency.

## Security and privacy

The host sets one shared `CURIO_ACCESS_PASSWORD` in Compose. Entry takes a username and that password. New names create profiles; existing names reopen them. There is no separate registration or personal password. Profiles isolate settings but do not authenticate household members from one another: anyone with the shared password can open any username.

The shared password uses PBKDF2-HMAC-SHA256 with 600,000 iterations, 16 random salt bytes, and a 32-byte derived key for verification. The database retains a slow salted hash to detect password changes on restart, then revokes all old sessions. The environment password must contain 12–128 bytes. Session tokens contain 32 random bytes, rotate at login, expire after 30 days, and never enter logs or database rows in raw form.

Every mutation uses POST, a session-bound CSRF token for signed-in users (double-submit cookie for anonymous forms), and Go's cross-origin request protection. Cookies are HttpOnly, SameSite=Lax, host-only, and Secure when `CURIO_BASE_URL` uses HTTPS. HTTPS deployments use the `__Host-` cookie prefix. Forwarded headers never control cookie security or rate-limit identity. Authentication attempts have bounded per-IP and process-wide rate limits. TLS must terminate at a trusted reverse proxy for public access.

Personal pages use `Cache-Control: no-store`. The service worker caches only public static files and a generic offline page; it never stores HTML responses with user data. No analytics, telemetry, remote fonts or remote scripts load. Browser images come from this server; the background worker contacts Wikimedia when enabled. Source and map links contact external sites only when opened.

## UI direction

Default to charcoal green, soft warm text, muted moss and plum cards, with a serif headline and system sans body. Keep bright surfaces subdued for night reading. Offer light and system themes as alternatives. A daily photographic cover and three pinned discovery previews lead the home screen. Places use a photographic field-note layout; facts use typographic question panels; Surprise uses a plum and coral palette; collections use a scrapbook shelf. Topic palettes preserve the selected light, dark, or system color scheme. One discovery occupies each detail screen. Respect reduced motion and system appearance, expose visible focus, use native form controls and labels, and keep touch targets at least 44 pixels. Discovery photographs include descriptive alternatives, captions, source links and licence credits. Space images identify the observation subject and any mosaic treatment.

## Deployment boundary

One non-root container, one local persistent volume, port 8080. Local Compose builds from source until a release image exists. Release CI builds amd64 and arm64 images and publishes version plus latest tags to GHCR. Do not claim publication before the workflow has run.

PWA installation and service workers require HTTPS or localhost. A plain HTTP LAN address still supports ordinary browsing and accounts, but not full PWA behavior. If the server loses internet access, bundled and cached stories, photos and accounts keep working. If the browser loses access to the server, it receives a generic offline page.

## Play and reactions

POST `/play` validates CSRF, content ID, and answer bounds. The server checks the answer against editorial quiz data or imported coordinates/catalogue metadata. Signed-in submissions redirect to the discovery; guests receive a reveal without a saved score. The full text remains available behind a native details control, so the game and reading flow work without JavaScript.

`discoveries` stores the first completion, answer, correctness, local calendar day, and points for each user/content pair. Unique keys make duplicate submissions harmless. Aggregate queries derive points, daily progress, and three milestone badges. POST `/react` saves, changes, or clears one reaction per user/content pair. Reactions affect recommendation weights. The existing provider interface supplies categorized facts from the local editorial collection.

## Personal collection and recommendations

Migration 004 adds favorites, a reaction timestamp, and per-user recommendation reset timestamps. Existing reactions received timestamp zero; migration 006 excludes and prunes these undated signals under the seven-day policy. Reset runs in a transaction: clear interests and advance the cutoff. Reaction records remain intact; subsequent reaction updates receive a new timestamp.

Favorite saves/removals are idempotent POSTs with CSRF and session-derived ownership. Collection pages use fixed 12-item pagination and stable ID tie-breaks. Private HTML remains `no-store`. Recommendation weights are computed from current interests and reactions within seven days and newer than the reset cutoff, with a 25–300 bound. Selection groups eligible stories by category, reserves one in four draws for categories below the strongest available weight, and otherwise samples categories by weight. Equal-weight pools explore uniformly. Explicit filters, unseen-first selection, and immediate-repeat exclusion apply before this step.

## Origin checks on LAN and proxied deployments

Same-origin form POSTs retain Origin under `Referrer-Policy: same-origin`. This is required for plain HTTP LAN browsing, where browsers omit Fetch Metadata and Go falls back to comparing Origin with Host. A configured `CURIO_BASE_URL` is an exact trusted-origin exception for reverse proxies. Neither null origins nor arbitrary forwarded headers receive an exception. The application also validates the form CSRF token on every POST.

## Daily editions

Migration 005 adds `daily_editions(profile_id, day, items)`. A transaction stores three content IDs at the first request for a local calendar day. Profile ID zero is the shared guest edition; session-derived IDs isolate signed-in picks and completion markers. Rows older than seven days are pruned when an edition is created. Selection prioritizes uncompleted content, then content outside the previous edition, then unseen content, with a deterministic day/profile/ID hash for tie-breaking. It chooses one place and two facts, preferring different categories when uncompleted content allows it. Homepage previews do not write history or award points.

`GET /freshness` returns a local date, catalogue size, and SHA-256 digest of sorted content IDs under normal browse authorization and `no-store`. IDs, rather than cache timestamps, prevent provider refreshes from looking like new arrivals. The browser checks only while the homepage is visible, at most once per minute on focus and every five minutes otherwise, with an eight-second timeout. A notice offers navigation; neither questions nor previews are replaced automatically. A localStorage boolean stores the pause preference. An HttpOnly cookie records the last homepage catalogue count; neither value contains account information.

Quick facts carry validated two-option quiz data in their stored payloads. Server-side scoring uses that payload, preserves first-completion semantics, and never trusts submitted correctness. Photo credit validation still applies to all places and any fact with a photograph; photo-free facts render typographic artwork. Topic navigation and validation use the categories of actual fact records.


## Runtime source feeds

Migration 006 adds content expiry and persisted per-provider state. `FeedSource` fetches a bounded batch, and `ImportFeed` validates the complete batch before an atomic publish. NASA Science and NASA Technology are fixed allowlisted HTTPS sources. Stable IDs hash canonical article URLs; updates retain IDs and cannot extend existing expiry. XML parsing and normalization never generate quizzes or claim human review. Feed content renders escaped text with source and publication/fetch dates.

Independent workers poll each source every four hours, with 30-second batch deadlines and 20-second HTTP timeouts. Before fetching, a worker persists its next attempt to avoid restart storms. Failures back off from one minute to 256 minutes; `Retry-After` can extend this to 24 hours. Payloads are limited to 2 MiB and 100 entries. Invalid individual entries are skipped; malformed envelopes fail the fetch. Source errors do not block app startup or cached reading.

Discovery, editions, catalogue counts, and latest-update queries exclude expired imports even before hourly deletion. Saved favorites and today's pinned edition retain payloads but do not make expired items eligible again. Cleanup runs independently of fetching. Seven-day behavior cutoffs apply in queries as well as cleanup. Reaction rows snapshot category and survive payload deletion until their own expiry. Migration 006 detaches completion rows from content deletion while preserving the user cascade and unique user/content award key. Source bodies are not retained by the award ledger. Full source and retention policy: [runtime feeds](runtime-feeds.md).
