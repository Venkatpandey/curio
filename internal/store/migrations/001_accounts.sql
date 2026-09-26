CREATE TABLE users (
 id INTEGER PRIMARY KEY,
 username TEXT NOT NULL COLLATE NOCASE UNIQUE,
 created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE TABLE user_settings (
 user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 display_name TEXT NOT NULL,
 theme TEXT NOT NULL DEFAULT 'dark' CHECK (theme IN ('system','light','dark')),
 home_label TEXT NOT NULL DEFAULT '',
 latitude REAL CHECK (latitude BETWEEN -90 AND 90),
 longitude REAL CHECK (longitude BETWEEN -180 AND 180),
 updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
 CHECK ((latitude IS NULL) = (longitude IS NULL))
);
CREATE TABLE user_interests (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 category TEXT NOT NULL,
 created_at INTEGER NOT NULL DEFAULT (unixepoch()),
 PRIMARY KEY(user_id,category)
);
CREATE TABLE application_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE sessions (
 token_hash TEXT PRIMARY KEY,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 csrf_token TEXT NOT NULL,
 created_at INTEGER NOT NULL DEFAULT (unixepoch()),
 expires_at INTEGER NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);
CREATE INDEX sessions_expiry ON sessions(expires_at);
