package main

import (
	"fmt"
	"os"
	"path/filepath"

	pebbledb "github.com/cockroachdb/pebble"
	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	lj "gopkg.in/natefinch/lumberjack.v2"
)

func sessionFolder(phone string) string {
	var out []rune
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			out = append(out, r)
		}
	}
	return "phone-" + string(out)
}

func createLogger(logFilePath string) *zap.Logger {
	// Setting up logging to file with rotation.
	//
	// Log to file, so we don't interfere with prompts and messages to user.
	logWriter := zapcore.AddSync(&lj.Logger{
		Filename:   logFilePath,
		MaxBackups: 3,
		MaxSize:    1, // megabytes
		MaxAge:     7, // days
	})
	logCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		logWriter,
		zap.DebugLevel,
	)
	lg = zap.New(logCore)
	return lg
}

func initializeStorage(c *Config) (s *Storage, err error) {
	s = &Storage{}
	// Setting up session storage.
	// This is needed to reuse session and not login every time.
	s.SessionDir = filepath.Join(c.TgleStateDirectory, "session", sessionFolder(c.TelegramPhoneNumber))
	if err = os.MkdirAll(s.SessionDir, 0o700); err != nil {
		return nil, err
	}
	s.LogFilePath = filepath.Join(s.SessionDir, "log.jsonl")

	fmt.Printf("Storing session in %s, logs in %s\n", s.SessionDir, s.LogFilePath)

	// So, we are storing session information in current directory, under subdirectory "session/phone_hash"
	s.SessionStorage = &telegram.FileSessionStorage{
		Path: filepath.Join(s.SessionDir, "session.json"),
	}
	// Peer storage, for resolve caching and short updates handling.
	s.DB, err = pebbledb.Open(filepath.Join(s.SessionDir, "peers.pebble.db"), &pebbledb.Options{})
	if err != nil {
		return nil, errors.Wrap(err, "create pebble storage")
	}

	return
}
