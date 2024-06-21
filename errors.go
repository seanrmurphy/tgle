package main

import "github.com/go-faster/errors"

var (
	// ErrRowExists is returned when trying to create a record for a row which already exists
	ErrRowExists = errors.New("row exists")
)
