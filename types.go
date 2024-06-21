package main

import (
	pebbledb "github.com/cockroachdb/pebble"
	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
)

// PeerInfo captures the information about a telegram peer
type PeerInfo struct {
	ID   int64
	Name string
	Type int64
}

// Storage is a wrapper aound the different storage types; these storage types were defined
// in the original userbot all of this is based on but I'm still not sure if they are really
// necessary here...
type Storage struct {
	SessionDir     string
	LogFilePath    string
	SessionStorage *session.FileStorage
	DB             *pebbledb.DB
}

// Handlers is a wrapper around the different handlers; these handlers were defined in the
// original userbot all of this is based on but I'm still not sure if they are really
// required here - they seem to be a basic mechanism of the telegram API when the objective
// is to create an interactive client.
type Handlers struct {
	Dispatcher      tg.UpdateDispatcher
	Waiter          *floodwait.Waiter
	UpdatesRecovery *updates.Manager
}
