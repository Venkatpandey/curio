# Delivery roadmap

1. **Foundation (implemented):** Go HTTP server, SQLite migrations, household username profiles/sessions, account settings, guest browsing, responsive UI, PWA manifest, Docker, and CI. Include a small attributed starter collection.
2. **Places (implemented):** Wikimedia place catalogue, real licensed photographs, 1–2 minute stories, fact panels, maps, distance, caching, provider timeouts/backoff, and persisted per-user seen tracking.
3. **Facts (initial provider implemented):** categorized editorial facts, source/review metadata, category filters, and saved reactions. Expand the three-story fact collection and add external providers next.
4. **Personalization (implemented):** favorites, paginated history, interest/reaction category weights with a 25% exploration branch, a visible topic mix, and preference reset.
5. **Play (first slice brought forward):** photo quizzes, answer reveals, Curiosity Points, three milestone badges, and a daily three-discovery challenge are implemented. More quiz formats and comparisons remain.
6. **Operations:** admin user/provider tools, provider health, content management, backup/export/import, and additional providers.

Consider a finite Rabbit Hole mode after the discovery and recommendation models stabilize. Each phase must keep tests and Docker support working. Do not reward time spent or introduce an infinite feed.

## Tracked backlog

The remaining work is tracked in [GitHub milestones](https://github.com/Venkatpandey/curio/milestones). Each issue defines its scope, acceptance criteria, and dependencies. Optional features are not release prerequisites.

### Phase 3: Facts completion

- [#1: Expand the sourced fact catalogue and discover categories from content](https://github.com/Venkatpandey/curio/issues/1)
- [#9: Add an optional external fact provider with a reviewed import pipeline](https://github.com/Venkatpandey/curio/issues/9)

### Phase 5: Play completion

- [#2: Add true-or-false and comparison rounds to Surprise Me](https://github.com/Venkatpandey/curio/issues/2)
- [#3: Add a guess-the-distance round using the saved home location](https://github.com/Venkatpandey/curio/issues/3)
- [#4: Add category achievements and a badge collection view](https://github.com/Venkatpandey/curio/issues/4)
- [#5: Keep the daily trio useful after a user exhausts the catalogue](https://github.com/Venkatpandey/curio/issues/5)

### Phase 6: Operations

- [#6: Add a protected admin area for users, sessions, and app status](https://github.com/Venkatpandey/curio/issues/6)
- [#7: Show provider health and add safe refresh, enable, and cache controls](https://github.com/Venkatpandey/curio/issues/7)
- [#8: Add content review, category management, and archive controls](https://github.com/Venkatpandey/curio/issues/8)
- [#10: Add verified backup/export and guarded full-instance restore](https://github.com/Venkatpandey/curio/issues/10)
- [#11: Add versioned profile export/import without exposing other profiles](https://github.com/Venkatpandey/curio/issues/11)
- [#12: Verify physical iPhone PWA behavior and accessibility before stable release](https://github.com/Venkatpandey/curio/issues/12)
- [#13: Prepare the first stable release and verify NAS install/upgrade/restore](https://github.com/Venkatpandey/curio/issues/13)

### Optional features

- [#14: Optional: add a finite Rabbit Hole exploration mode](https://github.com/Venkatpandey/curio/issues/14)
- [#15: Optional: add country and weather context through replaceable providers](https://github.com/Venkatpandey/curio/issues/15)
- [#16: Optional: add small offline discovery packs with explicit download controls](https://github.com/Venkatpandey/curio/issues/16)
- [#17: Optional: share attributed discovery cards without exposing profile data](https://github.com/Venkatpandey/curio/issues/17)
- [#18: Optional: add a gentle activity calendar and Seven Day Explorer badge](https://github.com/Venkatpandey/curio/issues/18)
