-- Guest editions use profile_id 0; signed-in editions use the user's ID.
CREATE TABLE daily_editions (
 profile_id INTEGER NOT NULL,
 day TEXT NOT NULL,
 items TEXT NOT NULL,
 PRIMARY KEY(profile_id,day)
);
