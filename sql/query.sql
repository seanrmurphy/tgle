-- name: GetLinks :many
SELECT * FROM links
LIMIT 20;

-- name: GetMessages :many
SELECT * FROM messages
LIMIT 20;

-- name: GetMessageByID :one
SELECT * FROM messages
WHERE id = ?
LIMIT 1;

-- name: GetLastUpdateByUser :one
SELECT last_update FROM user_chats
WHERE id = ?
LIMIT 1;

-- name: InsertLink :one
INSERT INTO links (
    id,
    url,
    site,
    page_title,
    tags
) VALUES (
  ?, ?, ?, ?, ?
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

-- name: InsertUserChat :one
INSERT INTO user_chats (
    id,
    last_update
) VALUES (
  ?, ?
)
ON CONFLICT (id) DO UPDATE SET last_update=excluded.last_update
RETURNING *;

