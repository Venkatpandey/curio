CREATE TABLE favorites (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 content_id TEXT NOT NULL REFERENCES content_items(id) ON DELETE CASCADE,
 saved_at INTEGER NOT NULL,
 PRIMARY KEY(user_id,content_id)
);
CREATE INDEX favorites_recent ON favorites(user_id,saved_at DESC);
ALTER TABLE reactions ADD COLUMN updated_at INTEGER NOT NULL DEFAULT 0;
CREATE TABLE recommendation_resets (
 user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 reset_at INTEGER NOT NULL
);
