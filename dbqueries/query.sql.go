// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.26.0
// source: query.sql

package dbqueries

import (
	"context"
	"database/sql"
)

const addTGSync = `-- name: AddTGSync :one
INSERT INTO tgle_sync (
    sync_time,
    messages_added,
    links_added
) VALUES (
  ?, ?, ?
)
RETURNING id, sync_time, messages_added, links_added
`

type AddTGSyncParams struct {
	SyncTime      sql.NullInt64
	MessagesAdded sql.NullInt64
	LinksAdded    sql.NullInt64
}

func (q *Queries) AddTGSync(ctx context.Context, arg AddTGSyncParams) (TgleSync, error) {
	row := q.db.QueryRowContext(ctx, addTGSync, arg.SyncTime, arg.MessagesAdded, arg.LinksAdded)
	var i TgleSync
	err := row.Scan(
		&i.ID,
		&i.SyncTime,
		&i.MessagesAdded,
		&i.LinksAdded,
	)
	return i, err
}

const getGSyncById = `-- name: GetGSyncById :one
SELECT id, sync_time, messages_added, links_added FROM tgle_sync
WHERE id = ?
LIMIT 1
`

func (q *Queries) GetGSyncById(ctx context.Context, id int64) (TgleSync, error) {
	row := q.db.QueryRowContext(ctx, getGSyncById, id)
	var i TgleSync
	err := row.Scan(
		&i.ID,
		&i.SyncTime,
		&i.MessagesAdded,
		&i.LinksAdded,
	)
	return i, err
}

const getLastTGSync = `-- name: GetLastTGSync :one
SELECT id, sync_time, messages_added, links_added FROM tgle_sync
ORDER BY sync_time DESC
LIMIT 1
`

func (q *Queries) GetLastTGSync(ctx context.Context) (TgleSync, error) {
	row := q.db.QueryRowContext(ctx, getLastTGSync)
	var i TgleSync
	err := row.Scan(
		&i.ID,
		&i.SyncTime,
		&i.MessagesAdded,
		&i.LinksAdded,
	)
	return i, err
}

const getLastUpdateByUser = `-- name: GetLastUpdateByUser :one
SELECT last_update FROM user_chats
WHERE id = ?
LIMIT 1
`

func (q *Queries) GetLastUpdateByUser(ctx context.Context, id int64) (sql.NullInt64, error) {
	row := q.db.QueryRowContext(ctx, getLastUpdateByUser, id)
	var last_update sql.NullInt64
	err := row.Scan(&last_update)
	return last_update, err
}

const getLinks = `-- name: GetLinks :many
SELECT id, created_at, url, site, page_title, tags FROM links
ORDER BY created_at DESC
LIMIT 50
`

func (q *Queries) GetLinks(ctx context.Context) ([]Link, error) {
	rows, err := q.db.QueryContext(ctx, getLinks)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Link
	for rows.Next() {
		var i Link
		if err := rows.Scan(
			&i.ID,
			&i.CreatedAt,
			&i.Url,
			&i.Site,
			&i.PageTitle,
			&i.Tags,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getMessageByID = `-- name: GetMessageByID :one
SELECT id, sent_at, sent_by, message FROM messages
WHERE id = ?
LIMIT 1
`

func (q *Queries) GetMessageByID(ctx context.Context, id int64) (Message, error) {
	row := q.db.QueryRowContext(ctx, getMessageByID, id)
	var i Message
	err := row.Scan(
		&i.ID,
		&i.SentAt,
		&i.SentBy,
		&i.Message,
	)
	return i, err
}

const getMessages = `-- name: GetMessages :many
SELECT id, sent_at, sent_by, message FROM messages
LIMIT 20
`

func (q *Queries) GetMessages(ctx context.Context) ([]Message, error) {
	rows, err := q.db.QueryContext(ctx, getMessages)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Message
	for rows.Next() {
		var i Message
		if err := rows.Scan(
			&i.ID,
			&i.SentAt,
			&i.SentBy,
			&i.Message,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const insertLink = `-- name: InsertLink :one
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
RETURNING id, created_at, url, site, page_title, tags
`

type InsertLinkParams struct {
	ID        int64
	Url       string
	Site      sql.NullString
	PageTitle sql.NullString
	Tags      sql.NullString
	CreatedAt int64
}

func (q *Queries) InsertLink(ctx context.Context, arg InsertLinkParams) (Link, error) {
	row := q.db.QueryRowContext(ctx, insertLink,
		arg.ID,
		arg.Url,
		arg.Site,
		arg.PageTitle,
		arg.Tags,
		arg.CreatedAt,
	)
	var i Link
	err := row.Scan(
		&i.ID,
		&i.CreatedAt,
		&i.Url,
		&i.Site,
		&i.PageTitle,
		&i.Tags,
	)
	return i, err
}

const insertMessage = `-- name: InsertMessage :one
INSERT INTO messages (
    id,
    sent_at,
    sent_by,
    message
) VALUES (
  ?, ?, ?, ?
)
RETURNING id, sent_at, sent_by, message
`

type InsertMessageParams struct {
	ID      int64
	SentAt  int64
	SentBy  sql.NullString
	Message string
}

func (q *Queries) InsertMessage(ctx context.Context, arg InsertMessageParams) (Message, error) {
	row := q.db.QueryRowContext(ctx, insertMessage,
		arg.ID,
		arg.SentAt,
		arg.SentBy,
		arg.Message,
	)
	var i Message
	err := row.Scan(
		&i.ID,
		&i.SentAt,
		&i.SentBy,
		&i.Message,
	)
	return i, err
}

const insertUserChat = `-- name: InsertUserChat :one
INSERT INTO user_chats (
    id,
    last_update
) VALUES (
  ?, ?
)
ON CONFLICT (id) DO UPDATE SET last_update=excluded.last_update
RETURNING id, last_update
`

type InsertUserChatParams struct {
	ID         int64
	LastUpdate sql.NullInt64
}

func (q *Queries) InsertUserChat(ctx context.Context, arg InsertUserChatParams) (UserChat, error) {
	row := q.db.QueryRowContext(ctx, insertUserChat, arg.ID, arg.LastUpdate)
	var i UserChat
	err := row.Scan(&i.ID, &i.LastUpdate)
	return i, err
}
