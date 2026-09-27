# Runtime feeds

## Sources and publication policy

The first provider imports the RSS descriptions from [NASA Science](https://science.nasa.gov/feed/) and [NASA Technology](https://www.nasa.gov/technology/feed/). NASA documents its feeds on its [RSS page](https://www.nasa.gov/rss-feeds/); its [media usage guidelines](https://www.nasa.gov/nasa-brand-center/images-and-media/) describe permitted use and third-party exceptions. No credentials or paid API are required. The importer makes one request per source every four hours during normal operation. It obeys upstream retry instructions and does not assume a contractual request quota or publication frequency.

Curio displays source-authored excerpts with NASA attribution and links to the original article. It does not fetch full article bodies, images, logos, or third-party media from these feeds. APOD entries are excluded because their feed descriptions contain navigation boilerplate and can involve third-party credits. Additional publishers or media require a separate source/rights review before enabling them.

Validation precedes publication: parse RSS, enforce response/item/text size bounds, require an allowlisted HTTPS article URL and a publication timestamp within the preceding seven days, normalize text formatting, reject executable/style blocks and empty/short entries, deduplicate canonical URLs, then commit the accepted batch. The database repeats identity/metadata validation before writing. An invalid batch rolls back. Feed records carry `source-excerpt-v1` validation metadata; they have no editorial review date or generated quiz. Automated checks cannot independently establish the truth of a source's claims. Owner review and correction tooling remains in #8.

Categories use the feed topic with limited explicit mappings for galaxy/star, solar-system, Earth, and ocean tags. This first source covers science, space, nature, and technology, not every Curio category. The source excerpts may be partial sentences because RSS publishers sometimes truncate descriptions; readers can follow the full-source link.

## Fetching and failure

`CURIO_FEEDS_ENABLED=true` enables both workers. Each source persists last attempt, last success, next attempt, failure count, a sanitized error, and accepted batch size in SQLite. Before a request, the worker saves the next scheduled attempt. Restarting an app during an outage does not reset the schedule. Each request has a user agent, a 20-second client timeout, a 30-second batch context, a 2 MiB response limit, and an allowlisted redirect policy with a three-hop bound.

Successful fetches schedule the next attempt four hours later. Failure retries start at one minute and double to 256 minutes. HTTP `Retry-After`, in seconds or date form, may extend the delay up to 24 hours. Workers honor shutdown cancellation. Malformed envelopes do not replace cached content; individual invalid entries are omitted. A valid feed with no eligible entries is a successful empty batch, not evidence of fresh supply. No request receives usernames, interests, reactions, or other profile data.

## Retention

| Record | Policy |
| --- | --- |
| Imported source excerpt | Eligible for seven days from original publication; repeated fetches cannot extend a cached item's expiry. |
| Expired unsaved excerpt | Removed during startup/hourly cleanup, even when fetching is disabled. |
| Today's pinned pick | Kept readable until the next server-local day; excluded from new selections once expired. |
| Saved favorite | Kept until explicitly removed; excluded from new selections once expired. |
| Reactions and browsing history | Seven-day maximum; queries ignore old records before cleanup runs. Recent reaction category snapshots can outlive their source payload. |
| Explicit interests and profile settings | Kept until changed by the user. |
| Completion ledger | Minimal stable identity and award fields retained to preserve earned points and prevent repeat awards. No source body. |
| Bundled stories / Wikimedia places | Existing offline collection and separate Wikimedia cache policy. |

The database reuses deleted pages; cleanup does not promise to shrink the SQLite file. The source catalogue remains small, but saved favorites and the award ledger can grow with deliberate use. Upstream outages eventually exhaust the seven-day feed window; the bundled collection remains available.

## Follow-up

#5 adds morning, afternoon, and evening personalized editions using this supply and seven-day signals. The current homepage retains one pinned daily trio and shows the newest three source updates separately. #7 adds owner-visible provider health and controls; #8 adds editorial review and archive management. The runtime importer does not require those admin screens.
