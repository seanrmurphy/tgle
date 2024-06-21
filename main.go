package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"database/sql"

	"github.com/adrg/xdg"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pterm/pterm"

	"github.com/go-faster/errors"
	boltstor "github.com/gotd/contrib/bbolt"
	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/contrib/middleware/ratelimit"
	"github.com/gotd/contrib/pebble"
	"github.com/gotd/contrib/storage"
	"go.etcd.io/bbolt"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/gotd/td/examples"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
)

//go:embed sql/schema.sql
var ddl string

var dbFilename = "sqlite.db"

var lg *zap.Logger

var arg struct {
	FillPeerStorage bool
	ServerMode      bool
	JSONReport      bool
	MarkdownReport  bool
	RemoveState     bool
}

func init() {
	flag.BoolVar(&arg.FillPeerStorage, "fill-peer-storage", false, "fill peer storage")
	flag.BoolVar(&arg.ServerMode, "server", false, "enable server mode")
	flag.BoolVar(&arg.JSONReport, "json", false, "generate link report in JSON format")
	flag.BoolVar(&arg.MarkdownReport, "markdown", false, "generate link report in markdown format")
	flag.BoolVar(&arg.RemoveState, "remove-all-state", false, "remove all tgle state")
	flag.Parse()
}

func createClient(s *Storage, c *Config, peerDB *pebble.PeerStorage) (client *telegram.Client, handlers *Handlers, err error) {
	handlers = &Handlers{}
	// Setting up client.
	//
	// Dispatcher is used to register handlers for events.
	handlers.Dispatcher = tg.NewUpdateDispatcher()
	// Setting up update handler that will fill peer storage before
	// calling dispatcher handlers.
	updateHandler := storage.UpdateHook(handlers.Dispatcher, peerDB)

	// Setting up persistent storage for qts/pts to be able to
	// recover after restart.
	boltdb, err := bbolt.Open(filepath.Join(s.SessionDir, "updates.bolt.db"), 0o666, nil)
	if err != nil {
		return nil, nil, errors.Wrap(err, "create bolt storage")
	}
	handlers.UpdatesRecovery = updates.New(updates.Config{
		Handler: updateHandler, // using previous handler with peerDB
		Logger:  lg.Named("updates.recovery"),
		Storage: boltstor.NewStateStorage(boltdb),
	})

	// Handler of FLOOD_WAIT that will automatically retry request.
	handlers.Waiter = floodwait.NewWaiter().WithCallback(func(ctx context.Context, wait floodwait.FloodWait) {
		// Notifying about flood wait.
		lg.Warn("Flood wait", zap.Duration("wait", wait.Duration))
		fmt.Println("Got FLOOD_WAIT. Will retry after", wait.Duration)
	})

	// Filling client options.
	options := telegram.Options{
		Logger:         lg,                       // Passing logger for observability.
		SessionStorage: s.SessionStorage,         // Setting up session sessionStorage to store auth data.
		UpdateHandler:  handlers.UpdatesRecovery, // Setting up handler for updates from server.
		Middlewares: []telegram.Middleware{
			// Setting up FLOOD_WAIT handler to automatically wait and retry request.
			handlers.Waiter,
			// Setting up general rate limits to less likely get flood wait errors.
			ratelimit.New(rate.Every(time.Millisecond*100), 5),
		},
		// dcs.Prod() is the default here but dcs.Test() can be used when in test
		// mode and it is necessary to test something with the test deployment...
		DCList: dcs.Prod(),
	}
	appID, err := strconv.Atoi(c.TelegramAppID)
	if err != nil {
		return nil, nil, errors.Wrap(err, "error parsing app id")
	}
	client = telegram.NewClient(appID, c.TelegramAppHash, options)
	return
}

func run(ctx context.Context, c *Config, s *Storage) error {

	peerDB := pebble.NewPeerStorage(s.DB)
	lg.Info("Storage", zap.String("path", s.SessionDir))

	client, handlers, err := createClient(s, c, peerDB)
	if err != nil {
		return errors.Wrap(err, "create client")
	}
	api := client.API()

	// Setting up resolver cache that will use peer storage.
	resolver := storage.NewResolverCache(peer.Plain(api), peerDB)
	// Usage:
	//   if _, err := resolver.ResolveDomain(ctx, "tdlibchat"); err != nil {
	//	   return errors.Wrap(err, "resolve")
	//   }
	_ = resolver

	// Authentication flow handles authentication process, like prompting for code and 2FA password.
	flow := auth.NewFlow(examples.Terminal{PhoneNumber: c.TelegramPhoneNumber}, auth.SendCodeOptions{})

	return handlers.Waiter.Run(ctx, func(ctx context.Context) error {

		// Spawning main goroutine.
		if err := client.Run(ctx, func(ctx context.Context) error {
			// Perform auth if no session is available.
			if err := client.Auth().IfNecessary(ctx, flow); err != nil {
				return errors.Wrap(err, "auth")
			}

			// Getting info about current user.
			self, err := client.Self(ctx)
			if err != nil {
				return errors.Wrap(err, "call self")
			}

			name := self.FirstName
			if self.Username != "" {
				// Username is optional.
				name = fmt.Sprintf("%s (@%s), %v", name, self.Username, self.ID)
			}
			fmt.Println("Current user:", name)

			lg.Info("Login",
				zap.String("first_name", self.FirstName),
				zap.String("last_name", self.LastName),
				zap.String("username", self.Username),
				zap.Int64("id", self.ID),
			)

			dbFullFilename := filepath.Join(c.TgleStateDirectory, dbFilename)
			db, err := sql.Open("sqlite3", dbFullFilename)
			if err != nil {
				lg.Sugar().Errorf("error opening database: %v", err)
				return err
			}

			// create tables if necessary
			if _, err = db.ExecContext(ctx, ddl); err != nil {
				lg.Sugar().Errorf("error creating db tables: %v", err)
				return err
			}
			defer db.Close()

			err = getMessages(ctx, client, db, self)
			if err != nil {
				lg.Sugar().Errorf("error getting messages: %v", err)
				return err
			}

			return nil
		}); err != nil {
			return errors.Wrap(err, "run")
		}
		return nil
	})
}

// removeState removes all tgle state from the filesystem
func removeState() error {
	result, _ := pterm.DefaultInteractiveConfirm.Show(("Are you sure you want to remove all tgle state?"))

	if result {
		removeString, _ := pterm.DefaultInteractiveTextInput.Show(("Please confirm by typing REMOVE-ALL-STATE..."))
		if removeString != "REMOVE-ALL-STATE" {
			pterm.Println("")
			pterm.Print("String entered does not match REMOVE-ALL-STATE - exiting...")
			return errors.New("String entered does not match REMOVE-ALL-STATE")
		}
	}

	// remove XDG_CONFIG_HOME/tgle
	configFileDirectory := path.Join(xdg.ConfigHome, ApplicationName)
	err := os.RemoveAll(configFileDirectory)
	if err != nil {
		pterm.Println("")
		pterm.Print("Error removing content from config directory - exiting...")
		return errors.New("Error removing content from config directory")
	}

	// remove XDG_STATE_HOME/tgle
	stateDirectory := path.Join(xdg.StateHome, ApplicationName)
	err = os.RemoveAll(stateDirectory)
	if err != nil {
		// it's not ideal if we exit here as we have not remove all state but we have no
		// specific way to rollback so we just live with it
		pterm.Println("")
		pterm.Print("Error removing content from state directory - exiting...")
		return errors.New("Error removing content from state directory")
	}

	pterm.Println("")
	pterm.Print("All tgle state has been removed...")
	return nil
}

func validateArgs() (bool, error) {
	// currently only allow one of server, jsonreport and markdownreport to be set
	// it's also ok if none are set and then we just scrape the links
	settings := 0
	if arg.ServerMode {
		settings++
	}
	if arg.JSONReport {
		settings++
	}
	if arg.MarkdownReport {
		settings++
	}
	if arg.RemoveState {
		settings++
	}
	if settings > 1 {
		return false, errors.New("only one of server, json, markdown, remove-all-state can be set")
	}
	return true, nil
}

func main() {
	validArgs, err := validateArgs()
	if !validArgs {
		log.Fatalf("invalid arguments: %v - exiting...", err.Error())
		os.Exit(1)
	}

	c, err := readConfig()
	if err != nil {
		log.Fatalf("error reading config file: %v - exiting...", err.Error())
		os.Exit(1)
	}

	s, err := initializeStorage(c)
	if err != nil {
		log.Fatalf("error initializing storage %v - exiting...", err.Error())
	}
	lg := createLogger(s.LogFilePath)
	defer func() { _ = lg.Sync() }()

	switch {
	case arg.ServerMode:
		runServer(c)
		return
	case arg.JSONReport:
		_ = generateJSONReport(c)
		return
	case arg.MarkdownReport:
		_ = generateMarkdownReport(c)
		return
	case arg.RemoveState:
		_ = removeState()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// TODO: Add logic to handle the case in which this record does not exist...
	lastSyncRecord, _ := getLastSyncTime(ctx, c)
	lastSync := time.UnixMilli(0)
	if lastSyncRecord != nil {
		lastSync = time.UnixMilli(lastSyncRecord.SyncTime.Int64)
	}
	fmt.Printf("last sync time: %v\n", lastSync)

	if err := run(ctx, c, s); err != nil {
		if errors.Is(err, context.Canceled) && ctx.Err() == context.Canceled {
			fmt.Println("\rClosed")
			defer func() {
				os.Exit(1)
			}()
			return
		}

		_, _ = fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
		defer func() {
			os.Exit(1)
		}()
		return
	}

	_ = writeSyncRecord(ctx, c)
	fmt.Println("Done")
}
