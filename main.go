package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"database/sql"

	_ "github.com/mattn/go-sqlite3"

	"github.com/PuerkitoBio/goquery"
	pebbledb "github.com/cockroachdb/pebble"
	"github.com/go-faster/errors"
	boltstor "github.com/gotd/contrib/bbolt"
	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/contrib/middleware/ratelimit"
	"github.com/gotd/contrib/pebble"
	"github.com/gotd/contrib/storage"
	"go.etcd.io/bbolt"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/time/rate"
	lj "gopkg.in/natefinch/lumberjack.v2"

	"github.com/seanrmurphy/telegram-bot/dbqueries"

	"github.com/gotd/td/examples"
	"github.com/gotd/td/session"
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

var (
	// ErrRowExists is returned when trying to create a record for a row which already exists
	ErrRowExists = errors.New("row exists")
)

var lg *zap.Logger

var arg struct {
	FillPeerStorage bool
	ServerMode      bool
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

func sessionFolder(phone string) string {
	var out []rune
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			out = append(out, r)
		}
	}
	return "phone-" + string(out)
}

// func getEnvironmentVariables() (c *Config, err error) {
// 	c = &Config{}
// 	// Using ".env" file to load environment variables.
// 	if err = godotenv.Load(); err != nil && !os.IsNotExist(err) {
// 		return nil, errors.Wrap(err, "load env")
// 	}
//
// 	// TG_PHONE is phone number in international format.
// 	// Like +4123456789.
// 	c.TelegramPhoneNumber = os.Getenv("TG_PHONE")
// 	if c.TelegramPhoneNumber == "" {
// 		return nil, errors.New("no phone")
// 	}
// 	// APP_HASH, APP_ID is from https://my.telegram.org/.
// 	c.TelegramAppID = os.Getenv("APP_ID")
// 	if err != nil {
// 		return nil, errors.Wrap(err, " parse app id")
// 	}
// 	c.TelegramAppHash = os.Getenv("APP_HASH")
// 	if c.TelegramAppHash == "" {
// 		return nil, errors.New("no app hash")
// 	}
//
// 	return
// }

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

func init() {
	flag.BoolVar(&arg.FillPeerStorage, "fill-peer-storage", false, "fill peer storage")
	flag.BoolVar(&arg.ServerMode, "server", false, "enable server mode")
	flag.Parse()
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
		// dcs.Prod() is the default here...
		DCList: dcs.Prod(),
		// DCList: dcs.Test(),
	}
	appID, err := strconv.Atoi(c.TelegramAppID)
	if err != nil {
		return nil, nil, errors.Wrap(err, "error parsing app id")
	}
	client = telegram.NewClient(appID, c.TelegramAppHash, options)
	return
}

// func printMessages(messagesClass tg.MessagesMessagesClass) {
// 	switch messages := messagesClass.(type) {
// 	case *tg.MessagesMessages:
// 		for _, mc := range messages.Messages {
// 			switch m := mc.(type) {
// 			case *tg.Message:
// 				log.Printf("message: Date %v, FromID %v, MessageID %v, Message %v", time.Unix(int64(m.Date), 0), m.FromID, m.ID, m.Message)
// 			default:
// 				log.Printf("unknown message class: %T", m)
// 			}
// 		}
// 	case *tg.MessagesMessagesSlice:
// 		for _, mc := range messages.Messages {
// 			switch m := mc.(type) {
// 			case *tg.Message:
// 				log.Printf("message: Date %v, FromID %v, MessageID %v, Message %v", time.Unix(int64(m.Date), 0), m.FromID, m.ID, m.Message)
// 			default:
// 				log.Printf("unknown message class: %T", m)
// 			}
// 		}
// 	default:
// 		log.Printf("unknown messagesmessages class: %T", messages)
// 	}
// }

func containsURL(message string) bool {
	return strings.Contains(message, "http") || strings.Contains(message, "https")
}

func extractURL(message string) (string, error) {
	re := regexp.MustCompile(`(https?|ftp|file)://[-A-Za-z0-9+&@#/%?=~_|!:,.;]+[-A-Za-z0-9+&@#/%=~_|]`)
	urls := re.FindAllString(message, -1)
	if len(urls) == 0 {
		return "", errors.New("no urls found")
	}
	for _, url := range urls {
		log.Printf("URL: %s", url)
	}
	return urls[0], nil
}

func getPageTitle(uri string) (string, error) {

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return "", err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}

	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		log.Printf("status code error: %d %s", res.StatusCode, res.Status)
		return "", errors.New("invalid status code")
	}

	// Load the HTML document
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Printf("error loading document: %v", err)
		defer func() {
			os.Exit(1)
		}()
	}

	title := doc.Find("title").Text()
	return title, nil

}

// func printMessagesToFile(filename string, messagesClass tg.MessagesMessagesClass) error {
// 	log.Printf("Writing messages to %s", filename)
// 	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
// 	if err != nil {
// 		log.Printf("error opening file: %v", err)
// 		return err
// 	}
// 	defer f.Close()
// 	switch messages := messagesClass.(type) {
// 	case *tg.MessagesMessages:
// 		for _, mc := range messages.Messages {
// 			switch m := mc.(type) {
// 			case *tg.Message:
// 				if containsURL(m.Message) {
// 					messageURl, err := extractURL(m.Message)
// 					if err != nil {
// 						log.Printf("unable to extract url from message")
// 					} else {
// 						title, _ := getPageTitle(messageUrl)
// 						fmt.Fprintf(f, "Date %v, FromID %v, MessageID %v, Url: %v, PageTitle: %v, Message %v\n\n", time.Unix(int64(m.Date), 0), m.FromID, m.ID, messageUrl, title, m.Message)
// 					}
// 				}
// 			default:
// 				log.Printf("unknown message class: %T", m)
// 			}
// 		}
// 	case *tg.MessagesMessagesSlice:
// 		for _, mc := range messages.Messages {
// 			switch m := mc.(type) {
// 			case *tg.Message:
// 				if containsURL(m.Message) {
// 					messageURL, err := extractURL(m.Message)
// 					if err != nil {
// 						log.Printf("unable to extract url from message")
// 					} else {
// 						title, _ := getPageTitle(messageUrl)
// 						fmt.Fprintf(f, "Date %v, FromID %v, MessageID %v, Url: %v, PageTitle: %v, Message %v\n\n", time.Unix(int64(m.Date), 0), m.FromID, m.ID, messageUrl, title, m.Message)
// 					}
// 				}
// 			default:
// 				log.Printf("unknown message class: %T", m)
// 			}
// 		}
// 	default:
// 		log.Printf("unknown messagesmessages class: %T", messages)
// 	}
// 	return nil
// }

func storeMessage(ctx context.Context, db *sql.DB, message tg.Message) error {
	queries := dbqueries.New(db)

	_, err := queries.GetMessageByID(ctx, int64(message.ID))
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil {
		return ErrRowExists
	}

	insertMessageParams := dbqueries.InsertMessageParams{
		ID:      int64(message.ID),
		SentAt:  int64(message.Date),
		SentBy:  sql.NullString{},
		Message: message.Message,
	}
	_, err = queries.InsertMessage(ctx, insertMessageParams)
	if err != nil {
		log.Printf("Error inserting message: %v", err)
		return err

	}
	return err
}

func storeLink(ctx context.Context, db *sql.DB, m tg.Message) error {
	queries := dbqueries.New(db)
	if containsURL(m.Message) {
		messageURL, err := extractURL(m.Message)
		if err == nil {
			title, _ := getPageTitle(messageURL)
			insertLinkParams := dbqueries.InsertLinkParams{
				ID:        int64(m.ID),
				Url:       messageURL,
				Site:      sql.NullString{},
				PageTitle: sql.NullString{String: title, Valid: true},
				Tags:      sql.NullString{},
			}
			_, err = queries.InsertLink(ctx, insertLinkParams)
			if err != nil {
				log.Printf("Error inserting link: %v", err)
			}
		} else {
			log.Printf("unable to extract url from message")
		}
	}
	return nil
}
func storeMessages(ctx context.Context, messagesClass tg.MessagesMessagesClass, db *sql.DB) error {
	log.Printf("Storing messages in database %s", dbFilename)
	// TODO: modify this to assume db is in the XDG_CONFIG_HOME directory

	newMessagesStored := 0
	switch messages := messagesClass.(type) {
	case *tg.MessagesMessages:
		for _, mc := range messages.Messages {
			switch m := mc.(type) {
			case *tg.Message:
				err := storeMessage(ctx, db, *m)
				if err != nil && err != ErrRowExists {
					return err
				}
				if err == nil {
					newMessagesStored++
				}
				_ = storeLink(ctx, db, *m)

			default:
				log.Printf("unknown message class: %T", m)
			}
		}
	case *tg.MessagesMessagesSlice:
		for _, mc := range messages.Messages {
			switch m := mc.(type) {
			case *tg.Message:
				err := storeMessage(ctx, db, *m)
				if err != nil && err != ErrRowExists {
					return err
				}
				if err == nil {
					newMessagesStored++
				}
				_ = storeLink(ctx, db, *m)
			default:
				log.Printf("unknown message class: %T", m)
			}
		}
	default:
		log.Printf("unknown messagesmessages class: %T", messages)
	}
	log.Printf("New messages stored: %v", newMessagesStored)
	return nil
}

// getMaxOffset ranges over a set of messages and gets the max offset
// in the set...
func getMaxOffset(messagesClass tg.MessagesMessagesClass) (maxOffset int64) {
	switch messages := messagesClass.(type) {
	case *tg.MessagesMessages:
		for _, mc := range messages.Messages {
			switch m := mc.(type) {
			case *tg.Message:
				if int64(m.ID) > maxOffset {
					maxOffset = int64(m.ID)
				}
			default:
				log.Printf("unknown message class: %T", m)
			}
		}
	case *tg.MessagesMessagesSlice:
		for _, mc := range messages.Messages {
			switch m := mc.(type) {
			case *tg.Message:
				if int64(m.ID) > maxOffset {
					maxOffset = int64(m.ID)
				}
			default:
				log.Printf("unknown message class: %T", m)
			}
		}
	}
	return
}

func getUserDialogMessages(ctx context.Context, client *telegram.Client, p *tg.PeerUser, db *sql.DB) (messages tg.MessagesMessagesClass, err error) {
	log.Printf("Dialog type User: getting user info for user = %v", p)
	userRequest := tg.InputUser{UserID: p.UserID}
	userInfo, err := client.API().UsersGetFullUser(ctx, &userRequest)
	if err != nil {
		log.Printf("error getting info for user: %v", err)
		// this should really be an error but we ignore it for now...
		return nil, nil
	}
	queries := dbqueries.New(db)
	lastUpdatePerUser, err := queries.GetLastUpdateByUser(ctx, p.UserID)
	if err != nil && err != sql.ErrNoRows {
		log.Printf("error getting last update from db - ignoring...")
	}
	// assumes we get info on a single user (as this is what we asked for)
	user := userInfo.Users[0].(*tg.User)
	log.Printf("userinfo: (firstname, lastname) - (%v, %v)", user.FirstName, user.LastName)
	log.Printf("Getting messages from dialog...")
	peerUser := tg.InputPeerUser{UserID: user.ID}
	searchRequest := tg.MessagesSearchRequest{
		Peer:     &peerUser,
		Limit:    100,
		Filter:   &tg.InputMessagesFilterURL{},
		OffsetID: int(lastUpdatePerUser.Int64),
	}
	messages, err = client.API().MessagesSearch(ctx, &searchRequest)
	highestOffset := getMaxOffset(messages)
	insertUserChatParams := dbqueries.InsertUserChatParams{
		ID:         p.UserID,
		LastUpdate: sql.NullInt64{Int64: highestOffset, Valid: true},
	}
	_, err = queries.InsertUserChat(ctx, insertUserChatParams)
	if err != nil {
		log.Printf("error inserting user chat information: %v", err)
		return
	}
	return
}

// getSavedMessages gets Saved Messages which are messages a user sends to themselves
func getSavedMessages(ctx context.Context, client *telegram.Client, userID int64) (messages tg.MessagesMessagesClass, err error) {
	peerUser := tg.InputPeerUser{UserID: userID}
	searchRequest := tg.MessagesSearchRequest{
		Peer:   &peerUser,
		Limit:  50,
		Filter: &tg.InputMessagesFilterURL{},
		//Filter: &tg.InputMessagesFilterEmpty{},
	}
	messages, err = client.API().MessagesSearch(ctx, &searchRequest)
	if err != nil {
		log.Printf("error getting messages: %v", err)
		return nil, err
	}
	return
}

func getMessagesFromUserDialogs(ctx context.Context, client *telegram.Client, userID int64, db *sql.DB) (messages tg.MessagesMessagesClass, err error) {
	api := client.API()
	request := tg.MessagesGetDialogsRequest{
		Flags:         0,
		ExcludePinned: false,
		FolderID:      0,
		OffsetDate:    0,
		OffsetID:      0,
		OffsetPeer:    &tg.InputPeerEmpty{},
		Limit:         0,
		Hash:          0,
	}
	dialogs, err := api.MessagesGetDialogs(ctx, &request)
	if err != nil {
		fmt.Printf("Error: %+v\n", err)
		return nil, errors.Wrap(err, "error getting dialogs")
	}
	switch dialogs.TypeID() {
	case tg.MessagesDialogsTypeID:
		log.Println()
		darray := dialogs.(*tg.MessagesDialogs)
		for _, d := range darray.Dialogs {
			messagesPerDialog, err := getMessagesPerDialog(ctx, client, d, db)
			if err != nil {
				log.Printf("error getting messages: %v", err)
				return nil, err
			}
			if messagesPerDialog != nil {
				_ = storeMessages(ctx, messagesPerDialog, db)
			}
		}
	case tg.MessagesDialogsSliceTypeID:
		log.Printf("messagesdialogsslice")
		darray := dialogs.(*tg.MessagesDialogsSlice)
		for _, d := range darray.Dialogs {
			messagesPerDialog, err := getMessagesPerDialog(ctx, client, d, db)
			if err != nil {
				log.Printf("error getting messages: %v", err)
				return nil, err
			}
			if messagesPerDialog != nil {
				_ = storeMessages(ctx, messagesPerDialog, db)
			}
		}
	case tg.MessagesDialogsNotModifiedTypeID:
		log.Printf("not modified")
	default:
		log.Printf("no clue")
	}
	return
}

// func getDialogs(ctx context.Context, client *telegram.Client) error {
// 	api := client.API()
// 	request := tg.MessagesGetDialogsRequest{
// 		Flags:         0,
// 		ExcludePinned: false,
// 		FolderID:      0,
// 		OffsetDate:    0,
// 		OffsetID:      0,
// 		OffsetPeer:    &tg.InputPeerEmpty{},
// 		Limit:         0,
// 		Hash:          0,
// 	}
// 	dialogs, err := api.MessagesGetDialogs(ctx, &request)
// 	if err != nil {
// 		fmt.Printf("Error: %+v\n", err)
// 		return errors.Wrap(err, "get dialogs")
// 	}
// 	switch dialogs.TypeID() {
// 	case tg.MessagesDialogsTypeID:
// 		log.Println()
// 		darray := dialogs.(*tg.MessagesDialogs)
// 		for _, d := range darray.Dialogs {
// 			printDialog(ctx, client, d)
// 		}
// 	case tg.MessagesDialogsSliceTypeID:
// 		log.Printf("messagesdialogsslice")
// 		darray := dialogs.(*tg.MessagesDialogsSlice)
// 		for _, d := range darray.Dialogs {
// 			printDialog(ctx, client, d)
// 		}
// 	case tg.MessagesDialogsNotModifiedTypeID:
// 		log.Printf("not modified")
// 	default:
// 		log.Printf("no clue")
// 	}
// 	return nil
// }

func getMessagesPerDialog(ctx context.Context, client *telegram.Client, d tg.DialogClass, db *sql.DB) (messages tg.MessagesMessagesClass, err error) {
	peer := d.GetPeer()
	switch p := peer.(type) {
	case *tg.PeerUser:
		messages, err = getUserDialogMessages(ctx, client, p, db)
	case *tg.PeerChat:
		log.Printf("Dialog type chat = %v - ignoring...", p)
	case *tg.PeerChannel:
		log.Printf("Dialog type channel = %v - ignoring...", p)
		// channelRequest := tg.InputChannel{ChannelID: p.ChannelID}
		// channelInfo, err := client.API().ChannelsGetFullChannel(ctx, &channelRequest)
		// if err != nil {
		// 	log.Printf("error getting info for channel: %v", err)
		// 	return
		// }
		// assumes we get info on a single user (as this is what we asked for)
		// channel := channelInfo.[0].(*tg.User)
		// log.Printf("channel info -  %v", channelInfo)
	default:
		log.Printf("unknown peer type = %v - ignoring...", peer)
	}
	return
}

func run(ctx context.Context, c *Config) error {
	s, err := initializeStorage(c)
	if err != nil {
		return errors.Wrap(err, "initialize storage")
	}
	lg := createLogger(s.LogFilePath)
	defer func() { _ = lg.Sync() }()

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
				log.Printf("error opening database: %v", err)
				return err
			}

			// create tables
			if _, err = db.ExecContext(ctx, ddl); err != nil {
				log.Printf("error creating db tables: %v", err)
				return err
			}
			defer db.Close()

			// first get Saved Messages - messages a user sends to themselves
			messages, err := getSavedMessages(ctx, client, self.ID)
			if err != nil {
				return errors.Wrap(err, "error getting saved messages")
			}
			if messages != nil {
				_ = storeMessages(ctx, messages, db)
			}

			// next, get messages from user dialogs...
			messages, err = getMessagesFromUserDialogs(ctx, client, self.ID, db)
			if err != nil {
				return errors.Wrap(err, "error getting messages from user dialogs")
			}
			// storeMessages(ctx, messages)

			// if arg.FillPeerStorage {
			// 	fmt.Println("Filling peer storage from dialogs to cache entities")
			// 	collector := storage.CollectPeers(peerDB)
			// 	if err := collector.Dialogs(ctx, query.GetDialogs(api).Iter()); err != nil {
			// 		return errors.Wrap(err, "collect peers")
			// 	}
			// 	fmt.Println("Filled")
			// }

			return nil
			// Waiting until context is done.
			// fmt.Println("Listening for updates. Interrupt (Ctrl+C) to stop.")
			// return handlers.UpdatesRecovery.Run(ctx, api, self.ID, updates.AuthOptions{
			// 	IsBot: self.Bot,
			// 	OnStart: func(ctx context.Context) {
			// 		fmt.Println("Update recovery initialized and started, listening for events")
			// 	},
			// })
		}); err != nil {
			return errors.Wrap(err, "run")
		}
		return nil
	})
}

func main() {
	c, err := readConfig()
	if err != nil {
		log.Fatal(err)
	}

	if arg.ServerMode {
		runServer(c)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := run(ctx, c); err != nil {
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

	fmt.Println("Done")
}
