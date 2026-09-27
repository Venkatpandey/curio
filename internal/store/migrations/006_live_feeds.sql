ALTER TABLE content_items ADD COLUMN expires_at INTEGER NOT NULL DEFAULT 0;
CREATE INDEX content_expiry ON content_items(expires_at);
CREATE TABLE feed_state (
 provider TEXT PRIMARY KEY,
 payload TEXT NOT NULL CHECK(json_valid(payload))
);

-- Keep only the small award ledger when a source payload expires. This prevents
-- both lost points and repeat awards without retaining old reading behavior.
CREATE TABLE completion_ledger (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 content_id TEXT NOT NULL,
 day TEXT NOT NULL,
 answer INTEGER NOT NULL,
 correct INTEGER NOT NULL CHECK(correct IN (0,1)),
 points INTEGER NOT NULL CHECK(points IN (1,3)),
 PRIMARY KEY(user_id,content_id)
);
INSERT INTO completion_ledger SELECT * FROM discoveries;
DROP TABLE discoveries;
ALTER TABLE completion_ledger RENAME TO discoveries;
CREATE INDEX discoveries_day ON discoveries(user_id,day);

-- Retain a reaction's category for its seven-day learning window even when the
-- source article expires sooner. No article body is needed for this signal.
CREATE TABLE recent_reactions (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 content_id TEXT NOT NULL,
 value TEXT NOT NULL CHECK(value IN ('interesting','not-for-me')),
 updated_at INTEGER NOT NULL,
 category TEXT NOT NULL,
 PRIMARY KEY(user_id,content_id)
);
INSERT INTO recent_reactions SELECT r.user_id,r.content_id,r.value,r.updated_at,json_extract(c.payload,'$.category') FROM reactions r JOIN content_items c ON c.id=r.content_id;
DROP TABLE reactions;
ALTER TABLE recent_reactions RENAME TO reactions;
CREATE INDEX reactions_recent ON reactions(user_id,updated_at);
