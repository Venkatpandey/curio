CREATE TABLE content_items (
 id TEXT PRIMARY KEY,
 kind TEXT NOT NULL CHECK (kind IN ('place','fact')),
 article_title TEXT NOT NULL DEFAULT '',
 payload TEXT NOT NULL CHECK (json_valid(payload)),
 cached_at INTEGER NOT NULL
);
CREATE INDEX content_kind ON content_items(kind);
CREATE INDEX content_article ON content_items(article_title);
CREATE TABLE media (
 key TEXT PRIMARY KEY,
 content_type TEXT NOT NULL,
 body BLOB NOT NULL,
 created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE TABLE user_history (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 content_id TEXT NOT NULL REFERENCES content_items(id) ON DELETE CASCADE,
 last_seen INTEGER NOT NULL,
 views INTEGER NOT NULL DEFAULT 1,
 PRIMARY KEY(user_id,content_id)
);
CREATE INDEX history_recent ON user_history(user_id,last_seen DESC);
