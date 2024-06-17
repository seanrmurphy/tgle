-- somewhat decoupled from the migrations folder - has to be kept in sync...
CREATE TABLE IF NOT EXISTS links (
    id INTEGER PRIMARY KEY,
    url TEXT NOT NULL,
    site TEXT,
    page_title TEXT,
    tags TEXT
);

CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY,
    sent_at INTEGER NOT NULL,
    sent_by TEXT,
    message TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS user_chats (
    id INTEGER PRIMARY KEY,
    last_update INTEGER
);

-- this performs an update on the last time a sync
-- is performed
CREATE TABLE IF NOT EXISTS tgle_sync (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    sync_time INTEGER,
    messages_added INTEGER,
    links_added INTEGER
);
