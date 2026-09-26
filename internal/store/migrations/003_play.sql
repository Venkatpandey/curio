CREATE TABLE discoveries (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 content_id TEXT NOT NULL REFERENCES content_items(id) ON DELETE CASCADE,
 day TEXT NOT NULL,
 answer INTEGER NOT NULL,
 correct INTEGER NOT NULL CHECK(correct IN (0,1)),
 points INTEGER NOT NULL CHECK(points IN (1,3)),
 PRIMARY KEY(user_id,content_id)
);
CREATE INDEX discoveries_day ON discoveries(user_id,day);
CREATE TABLE reactions (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 content_id TEXT NOT NULL REFERENCES content_items(id) ON DELETE CASCADE,
 value TEXT NOT NULL CHECK(value IN ('interesting','not-for-me')),
 PRIMARY KEY(user_id,content_id)
);
