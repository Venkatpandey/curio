# Operations

## Password changes

Change `CURIO_ACCESS_PASSWORD` in `.env` or the Compose environment block, then run:

```sh
docker compose up -d --force-recreate
```

Curio detects the changed password at startup and invalidates all sessions. Profiles and settings remain. Everyone enters the new shared password next time. The database contains a slow salted verification hash for rotation detection, not the plaintext secret.

## Backup and restore

Use the actual volume name from `docker volume ls`. Compose normally names it `curio_curio-data`, but the project name may change that prefix.

Stop the service before backup. Create a private backup directory, then archive the entire volume:

```sh
mkdir -m 700 -p backups
docker compose stop curio
docker run --rm -v curio_curio-data:/data:ro -v "$PWD/backups:/backup" alpine:3.22 sh -c 'tar czf /backup/curio-data.tgz -C /data . && chmod 600 /backup/curio-data.tgz'
docker compose start curio
```

To restore, stop Curio, preserve a backup of the current volume, then restore the archive into an empty replacement volume. Start Curio with that replacement volume mounted at `/data` and ensure ownership is UID/GID 10001. Restore the matching password configuration if you want existing sessions to remain valid. A changed password revokes them.

Never put SQLite on an SMB/NFS share or share one database among several Curio replicas. Use a local Docker volume and one process. Migrations run at startup in a transaction; use a matching application version for restores. The server refuses schemas newer than it understands.

## Provider and browser outages

The server refreshes Wikipedia and Commons data in the background. Set `CURIO_WIKIMEDIA_ENABLED=false` to stop refreshes; bundled and cached stories, photos and accounts remain available without internet access. Browser-to-server network access is still required for account pages; the service worker intentionally does not cache them.

## Browser checks

`scripts/browser-smoke.cjs` exercises username entry, settings, logout, profile isolation, quiz/reveal flows, point totals, reactions, daily progress, favorites, collection isolation, mix reset, discovery links, mobile widths, themes, and service-worker cache privacy in Chromium and WebKit. Run it against a disposable server with the password `curio-local-qa-password`, or provide `CURIO_TEST_PASSWORD`. It creates test profiles.

The script requires Node.js and Playwright as development-only tools. Set `CURIO_TEST_URL` (default `http://127.0.0.1:18080`), and optionally `CURIO_BROWSER_EXECUTABLE` for an installed Chromium browser. Set `CURIO_TEST_BROWSER` to `chromium` or `webkit` to run one engine. Screenshots go to ignored `test-results/`.

Offline navigation is checked in Chromium. Playwright's WebKit offline emulation fails navigation before the service worker handles it, so the WebKit test checks cache contents and online flows instead. These desktop browser-engine checks do not replace a physical iPhone installation test.


## Phase 1 upgrades

Back up the volume before upgrading. Migration 002 creates content/media/history tables without changing users, settings, sessions or the shared password. The first startup seeds the six editorial stories locally, then starts optional background fetching. A schema-1 binary cannot reopen a schema-2 database; restore the pre-upgrade backup if rolling back.

Provider logs distinguish successful cache additions from deferred fetches. `/health` reflects database availability and does not fail merely because Wikimedia is unavailable. The bounded catalogue limits content growth; photos are at most 4 MiB each. Replaced photos are removed after their last article reference disappears.

## Play upgrade

Migration 003 adds discoveries and reactions. Existing profiles, sessions, content, media, and seen history stay in place. Back up before updating; a pre-play binary cannot open schema 3. Restore the matching backup to roll back. Scoring uses first completion per user/content pair and the server’s local date (`TZ`), with a unique database key protecting concurrent submissions.

## Personalization upgrade

Migration 004 adds favorites and recommendation metadata. Back up before updating. An older binary cannot open schema 4; rollback requires the matching pre-upgrade backup. No content, history, score, or profile records are rewritten. Collection and mix checks run against disposable profiles in the browser smoke script.

## CI and image publishing

The reusable `checks.yml` workflow runs Go checks, isolated Chromium/WebKit smoke tests, and multi-architecture Docker builds. Browser jobs install the locked development dependencies with `npm ci` and use a disposable database with Wikimedia fetching disabled. Screenshots and server logs stay in short-lived Actions artifacts. No production secrets or household data enter CI.

`ci.yml` runs on pull requests, pushes to `main`, and manual dispatch. Only successful main runs publish development images (`edge` and `sha-<commit>`). `release.yml` validates `vMAJOR.MINOR.PATCH` tags, allows prerelease suffixes, reruns all checks, and publishes version tags. Stable tags update `latest`; prereleases do not. The publish workflow includes provenance and an SBOM. Both architectures build with Buildx and QEMU.

To run browser checks locally, install Node.js, run `npm ci`, then `npx playwright install chromium webkit`. Start a disposable Curio server and run `npm run test:browser`. Runtime images contain neither Node.js nor these development dependencies.
