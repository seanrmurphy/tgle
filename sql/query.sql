-- name: GetLinks :many
SELECT * FROM links
ORDER BY created_at DESC
LIMIT 50;

-- name: GetMessages :many
SELECT * FROM messages
LIMIT 20;

-- name: GetMessageByID :one
SELECT * FROM messages
WHERE id = ?
LIMIT 1;

-- name: GetLastUpdateByUser :one
SELECT last_update FROM dialogs
WHERE id = ?
LIMIT 1;

-- name: InsertLink :one
INSERT INTO links (
    id,
    url,
    site,
    page_title,
    tags,
    created_at
) VALUES (
  ?, ?, ?, ?, ?, ?
)
RETURNING *;

-- name: InsertMessage :one
INSERT INTO messages (
    id,
    sent_at,
    sent_by,
    message
) VALUES (
  ?, ?, ?, ?
)
RETURNING *;

-- name: InsertDialog :one
INSERT INTO dialogs (
    id,
    last_update,
    name,
    type

) VALUES (
  ?, ?, ?, ?
)
ON CONFLICT (id) DO UPDATE SET last_update=excluded.last_update
RETURNING *;

-- name: GetLastTGSync :one
SELECT * FROM tgle_sync
ORDER BY sync_time DESC
LIMIT 1;

-- name: GetGSyncById :one
SELECT * FROM tgle_sync
WHERE id = ?
LIMIT 1;

-- name: AddTGSync :one
INSERT INTO tgle_sync (
    sync_time,
    messages_added,
    links_added
) VALUES (
  ?, ?, ?
)
RETURNING *;

-- name: GetLinksWithSender :many
SELECT l.id, l.url, l.page_title, m.sent_at, m.sent_by
FROM links l
INNER JOIN messages m
ON l.id = m.id
ORDER BY m.sent_at DESC
LIMIT 50;
