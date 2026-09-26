# Delivery roadmap

1. **Foundation (implemented):** Go HTTP server, SQLite migrations, household username profiles/sessions, account settings, guest browsing, responsive UI, PWA manifest, Docker, and CI. Include a small attributed starter collection.
2. **Places (implemented):** Wikimedia place catalogue, real licensed photographs, 1–2 minute stories, fact panels, maps, distance, caching, provider timeouts/backoff, and persisted per-user seen tracking.
3. **Facts (initial provider implemented):** categorized editorial facts, source/review metadata, category filters, and saved reactions. Expand the three-story fact collection and add external providers next.
4. **Personalization (implemented):** favorites, paginated history, interest/reaction category weights with a 25% exploration branch, a visible topic mix, and preference reset.
5. **Play (first slice brought forward):** photo quizzes, answer reveals, Curiosity Points, three milestone badges, and a daily three-discovery challenge are implemented. More quiz formats and comparisons remain.
6. **Operations:** admin user/provider tools, provider health, content management, backup/export/import, and additional providers.

Consider a finite Rabbit Hole mode after the discovery and recommendation models stabilize. Each phase must keep tests and Docker support working. Do not reward time spent or introduce an infinite feed.
