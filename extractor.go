package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/pterm/pterm"

	"github.com/seanrmurphy/tgle/dbqueries"
	"github.com/seanrmurphy/tgle/utils"
)

func getMessages(ctx context.Context, client *telegram.Client, db *sql.DB, self *tg.User) error {
	// first get Saved Messages - messages a user sends to themselves
	spinnerType := createSpinnerType()
	gettimgMessagesString := "Getting Saved Messages"
	spinner, _ := spinnerType.Start(gettimgMessagesString)

	messagesFromSaveMessages, err := getSavedMessages(ctx, client, db, self.ID)
	if err != nil {
		return errors.Wrap(err, "error getting saved messages")
	}
	var messages []tg.MessageClass
	switch messagesWrapper := messagesFromSaveMessages.(type) {
	case *tg.MessagesMessages:
		messages = messagesWrapper.Messages
	case *tg.MessagesMessagesSlice:
		messages = messagesWrapper.Messages
	default:
		lg.Sugar().Errorf("unexpected type: %T", messagesWrapper)
		return errors.Errorf("unexpected type: %T", messagesWrapper)
	}
	numMessages := len(messages)
	finalMessagesString := fmt.Sprintf("%v...(%v messages received)", gettimgMessagesString, numMessages)
	spinner.Success(finalMessagesString)
	if messagesFromSaveMessages != nil {
		utils.PrintMessages(messagesFromSaveMessages, false, lg)
		_ = storeSavedMessages(ctx, messagesFromSaveMessages, db)
	} else {
		lg.Sugar().Info("no messages received...")
	}

	// next, get messages from user dialogs...
	// uncomment below when it seems to work...
	_, err = getMessagesFromDialogs(ctx, client, self.ID, db)
	if err != nil {
		return errors.Wrap(err, "error getting messages from user dialogs")
	}
	return nil
}

func getMessagesFromDialogs(ctx context.Context, client *telegram.Client, userID int64, db *sql.DB) (messages tg.MessagesMessagesClass, err error) {
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
		lg.Sugar().Errorf("Error: %+v\n", err)
		return nil, errors.Wrap(err, "error getting dialogs")
	}
	// TODO: consolidate the logic below to reduce duplication
	switch dialogs.TypeID() {
	case tg.MessagesDialogsTypeID:
		darray := dialogs.(*tg.MessagesDialogs)
		for _, d := range darray.Dialogs {
			messagesPerDialog, err := getMessagesPerDialog(ctx, client, d, db, userID)
			if err != nil {
				lg.Sugar().Errorf("error getting messages: %v", err)
				return nil, err
			}
			if messagesPerDialog != nil {
				utils.PrintMessages(messages, false, lg)
				_ = storeMessages(ctx, messagesPerDialog, db)
			}
		}
	case tg.MessagesDialogsSliceTypeID:
		lg.Sugar().Info("messagesdialogsslice")
		darray := dialogs.(*tg.MessagesDialogsSlice)
		for _, d := range darray.Dialogs {
			messagesPerDialog, err := getMessagesPerDialog(ctx, client, d, db, userID)
			if err != nil {
				lg.Sugar().Errorf("error getting messages: %v", err)
				return nil, err
			}
			if messagesPerDialog != nil {
				utils.PrintMessages(messagesPerDialog, false, lg)
				_ = storeMessages(ctx, messagesPerDialog, db)
			}
		}
	case tg.MessagesDialogsNotModifiedTypeID:
		lg.Sugar().Warn("not modified")
	default:
		lg.Sugar().Warn("no clue")
	}
	return
}

func createSpinnerType() *pterm.SpinnerPrinter {
	blueNormalStyle := pterm.NewStyle(pterm.FgBlue)
	blueBoldStyle := pterm.NewStyle(pterm.FgBlue, pterm.Bold)
	greenStyle := pterm.NewStyle(pterm.FgGreen, pterm.Bold)
	yellowStyle := pterm.NewStyle(pterm.FgYellow, pterm.Bold)
	Info := pterm.PrefixPrinter{
		MessageStyle: blueNormalStyle,
		Prefix: pterm.Prefix{
			Style: yellowStyle,
			Text:  "🛈",
		},
	}

	Success := pterm.PrefixPrinter{
		MessageStyle: blueBoldStyle,
		Prefix: pterm.Prefix{
			Style: greenStyle,
			Text:  "✔",
		},
	}

	spinner := pterm.DefaultSpinner
	spinner.InfoPrinter = &Info
	spinner.SuccessPrinter = &Success
	return &spinner
}

func getUserDialogMessages(ctx context.Context, client *telegram.Client, userID int64,
	db *sql.DB) (messages tg.MessagesMessagesClass, err error) {
	lg.Sugar().Infof("Dialog type User: getting user info for user = %v", userID)
	userRequest := tg.InputUser{UserID: userID}
	userInfo, err := client.API().UsersGetFullUser(ctx, &userRequest)
	if err != nil {
		lg.Sugar().Errorf("error getting info for user: %v", err)
		// this should really be an error but we ignore it for now...
		return nil, nil
	}
	user := userInfo.Users[0].(*tg.User)
	name := user.FirstName
	if name == "" {
		lg.Sugar().Warnf("error specifying username - user has no first name")
		name = "Unknown"
	}
	if user.LastName != "" {
		name = name + " " + user.LastName
	}
	peerInfo := PeerInfo{
		ID:   userID,
		Name: name,
		Type: tg.PeerUserTypeID,
	}
	spinnerType := createSpinnerType()
	gettimgMessagesString := fmt.Sprintf("Getting messages from user %v", name)
	spinner, _ := spinnerType.Start(gettimgMessagesString)

	queries := dbqueries.New(db)
	lastUpdatePerUser, err := queries.GetLastUpdateByUser(ctx, peerInfo.ID)
	if err != nil && err != sql.ErrNoRows {
		lg.Sugar().Error("error getting last update from db - ignoring...")
	}
	// assumes we get info on a single user (as this is what we asked for)
	lg.Sugar().Info("Getting messages from dialog...")
	peerUser := tg.InputPeerUser{UserID: userID}
	searchRequest := tg.MessagesSearchRequest{
		Peer:   &peerUser,
		Limit:  100,
		Filter: &tg.InputMessagesFilterURL{},
		MinID:  int(lastUpdatePerUser.Int64),
	}
	messages, err = client.API().MessagesSearch(ctx, &searchRequest)
	highestOffset, numMessages := getMaxOffset(messages)
	finalMessagesString := fmt.Sprintf("%v...(%v messages received)", gettimgMessagesString, numMessages)
	spinner.Success(finalMessagesString)
	if numMessages != 0 {
		insertDialogParams := dbqueries.InsertDialogParams{
			ID:         peerInfo.ID,
			LastUpdate: sql.NullInt64{Int64: highestOffset, Valid: true},
			Name:       sql.NullString{String: peerInfo.Name, Valid: true},
			Type:       sql.NullInt64{Int64: peerInfo.Type, Valid: true},
		}
		_, err = queries.InsertDialog(ctx, insertDialogParams)
		if err != nil {
			lg.Sugar().Errorf("error inserting user chat information: %v", err)
			return
		}
	}
	return
}

// getSavedMessages gets Saved Messages which are messages a user sends to themselves
func getSavedMessages(ctx context.Context, client *telegram.Client, db *sql.DB, userID int64) (messages tg.MessagesMessagesClass, err error) {
	queries := dbqueries.New(db)
	lastUpdatePerUser, err := queries.GetLastUpdateByUser(ctx, userID)
	if err != nil && err != sql.ErrNoRows {
		lg.Sugar().Error("error getting last update from db - ignoring...")
	}

	peerUser := tg.InputPeerUser{UserID: userID}
	// log.Printf("min date int: %v", minDateInt)
	searchRequest := tg.MessagesSearchRequest{
		Peer:   &peerUser,
		Limit:  100,
		Filter: &tg.InputMessagesFilterURL{},
		MinID:  int(lastUpdatePerUser.Int64),
	}
	messages, err = client.API().MessagesSearch(ctx, &searchRequest)
	if err != nil {
		lg.Sugar().Errorf("error getting messages: %v", err)
		return nil, err
	}
	highestOffset, numMessages := getMaxOffset(messages)
	if numMessages != 0 {
		insertDialogParams := dbqueries.InsertDialogParams{
			ID:         userID,
			LastUpdate: sql.NullInt64{Int64: highestOffset, Valid: true},
			Name:       sql.NullString{String: "some name", Valid: true},
		}
		_, err = queries.InsertDialog(ctx, insertDialogParams)
		if err != nil {
			lg.Sugar().Errorf("error inserting user chat information: %v", err)
			return
		}
	}
	return
}

func getMessagesPerDialog(ctx context.Context, client *telegram.Client, d tg.DialogClass,
	db *sql.DB, userID int64) (messages tg.MessagesMessagesClass, err error) {
	peer := d.GetPeer()
	switch p := peer.(type) {
	case *tg.PeerUser:
		if p.UserID != userID { // this is the Saved Message case which is handled separately
			messages, err = getUserDialogMessages(ctx, client, p.UserID, db)
		}
	case *tg.PeerChat:
		messages, err = getChatDialogMessages(ctx, client, p.ChatID, db)
	case *tg.PeerChannel:
		// currently, we don't extract links sent to channels
		lg.Sugar().Infof("Dialog type channel = %v - ignoring...", p)
	default:
		lg.Sugar().Warnf("unknown peer type = %v - ignoring...", peer)
	}
	return
}

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
		lg.Sugar().Infof("URL: %s", url)
	}
	return urls[0], nil
}

// twitter does not make it so easy to obtain the page title -
// need to load it in a headless browser
func getPageTitleUsingBrowser(uri string) (string, error) {
	ctx, _ := chromedp.NewContext(context.Background())
	var title string
	err := chromedp.Run(ctx,
		chromedp.Navigate(uri),
		chromedp.Sleep(10*time.Second),
		chromedp.Title(&title),
	)
	return title, err
}

func getPageTitle(uri string) (pageTitle string, err error) {

	// twitter is a special case...
	parsedURL, err := url.Parse(uri)
	if err != nil {
		return "", err
	}

	// Remove the "www." prefix if present
	hostname := strings.TrimPrefix(parsedURL.Hostname(), "www.")

	if hostname == "twitter.com" || hostname == "mobile.twitter.com" || hostname == "x.com" || hostname == "t.co" {
		pageTitle, err = getPageTitleUsingBrowser(uri)
		return
	}

	pageTitle, err = getPageTitleWithGet(uri)
	return
}

func getPageTitleWithGet(uri string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return "", err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		lg.Warn(err.Error())
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		lg.Sugar().Errorf("status code error: %d %s", res.StatusCode, res.Status)
		return "", errors.New("invalid status code")
	}

	// Load the HTML document
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		lg.Sugar().Errorf("error loading document: %v", err)
		defer func() {
			os.Exit(1)
		}()
	}

	title := doc.Find("title").Text()
	return title, nil
}

func storeMessage(ctx context.Context, db *sql.DB, message tg.Message, savedMessage bool) error {
	queries := dbqueries.New(db)

	_, err := queries.GetMessageByID(ctx, int64(message.ID))
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil {
		return ErrRowExists
	}

	var sentBy int64
	if message.FromID == nil {
		// this should only happen when this is a saved message
		if !savedMessage {
			lg.Sugar().Warn("warning: message FromID not specified and not in Saved Messages...")
		}
		sentBy = int64(message.PeerID.(*tg.PeerUser).UserID)
	} else {
		sentBy = int64(message.FromID.(*tg.PeerUser).UserID)

	}
	insertMessageParams := dbqueries.InsertMessageParams{
		ID:      int64(message.ID),
		SentAt:  int64(message.Date),
		SentBy:  sql.NullString{String: fmt.Sprintf("%d", sentBy), Valid: true},
		Message: message.Message,
	}
	_, err = queries.InsertMessage(ctx, insertMessageParams)
	if err != nil {
		lg.Sugar().Errorf("Error inserting message: %v", err)
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
				CreatedAt: int64(time.Now().UnixMilli()),
			}
			_, err = queries.InsertLink(ctx, insertLinkParams)
			if err != nil {
				lg.Sugar().Warnf("Error inserting link: %v", err)
			}
		} else {
			lg.Sugar().Warn("unable to extract url from message")
		}
	}
	return nil
}

// storeSavedMessages stores saved messages; what is particular about Saved Messages is that they don't
func storeSavedMessages(ctx context.Context, messagesClass tg.MessagesMessagesClass, db *sql.DB) error {
	lg.Sugar().Infof("Storing messages in database %s", dbFilename)

	newMessagesStored := 0
	// TODO: consolidate the fbllowing to reduce duplication
	switch messages := messagesClass.(type) {
	case *tg.MessagesMessages:
		for _, mc := range messages.Messages {
			switch m := mc.(type) {
			case *tg.Message:
				err := storeMessage(ctx, db, *m, true)
				if err != nil && err != ErrRowExists {
					return err
				}
				if err == nil {
					newMessagesStored++
				}
				_ = storeLink(ctx, db, *m)

			default:
				lg.Sugar().Warnf("unknown message class: %T", m)
			}
		}
	case *tg.MessagesMessagesSlice:
		for _, mc := range messages.Messages {
			switch m := mc.(type) {
			case *tg.Message:
				err := storeMessage(ctx, db, *m, true)
				if err != nil && err != ErrRowExists {
					return err
				}
				if err == nil {
					newMessagesStored++
				}
				_ = storeLink(ctx, db, *m)
			default:
				lg.Sugar().Warnf("unknown message class: %T", m)
			}
		}
	default:
		lg.Sugar().Warnf("unknown messagesmessages class: %T", messages)
	}
	lg.Sugar().Infof("New messages stored: %v", newMessagesStored)
	return nil
}

func storeMessages(ctx context.Context, messagesClass tg.MessagesMessagesClass, db *sql.DB) error {
	lg.Sugar().Infof("Storing messages in database %s", dbFilename)

	newMessagesStored := 0
	// TODO: consolidate the fbllowing to reduce duplication
	switch messages := messagesClass.(type) {
	case *tg.MessagesMessages:
		for _, mc := range messages.Messages {
			switch m := mc.(type) {
			case *tg.Message:
				err := storeMessage(ctx, db, *m, false)
				if err != nil && err != ErrRowExists {
					return err
				}
				if err == nil {
					newMessagesStored++
				}
				_ = storeLink(ctx, db, *m)

			default:
				lg.Sugar().Warnf("unknown message class: %T", m)
			}
		}
	case *tg.MessagesMessagesSlice:
		for _, mc := range messages.Messages {
			switch m := mc.(type) {
			case *tg.Message:
				err := storeMessage(ctx, db, *m, false)
				if err != nil && err != ErrRowExists {
					return err
				}
				if err == nil {
					newMessagesStored++
				}
				_ = storeLink(ctx, db, *m)
			default:
				lg.Sugar().Warnf("unknown message class: %T", m)
			}
		}
	default:
		lg.Sugar().Warnf("unknown messagesmessages class: %T", messages)
	}
	lg.Sugar().Infof("New messages stored: %v", newMessagesStored)
	return nil
}

// getMaxOffset ranges over a set of messages and gets the max offset
// in the set...
func getMaxOffset(messagesClass tg.MessagesMessagesClass) (maxOffset int64, numMessages int) {
	switch messages := messagesClass.(type) {
	case *tg.MessagesMessages:
		numMessages = len(messages.Messages)
		for _, mc := range messages.Messages {
			switch m := mc.(type) {
			case *tg.Message:
				if int64(m.ID) > maxOffset {
					maxOffset = int64(m.ID)
				}
			default:
				lg.Sugar().Warnf("unknown message class: %T", m)
			}
		}
	case *tg.MessagesMessagesSlice:
		numMessages = len(messages.Messages)
		for _, mc := range messages.Messages {
			switch m := mc.(type) {
			case *tg.Message:
				if int64(m.ID) > maxOffset {
					maxOffset = int64(m.ID)
				}
			default:
				lg.Sugar().Warnf("unknown message class: %T", m)
			}
		}
	}
	return
}

func getChatDialogMessages(ctx context.Context, client *telegram.Client, chatID int64,
	db *sql.DB) (messages tg.MessagesMessagesClass, err error) {
	lg.Sugar().Infof("Dialog type chat: getting chat info for user = %v", chatID)
	chatInfo, err := client.API().MessagesGetFullChat(ctx, chatID)
	if err != nil {
		lg.Sugar().Errorf("error getting info for chat: %v", err)
		// this should really be an error but we ignore it for now...
		return nil, nil
	}
	chat := chatInfo.Chats[0].(*tg.Chat)
	peerInfo := PeerInfo{
		ID:   chatID,
		Name: chat.Title,
		Type: tg.PeerChatTypeID,
	}
	spinnerType := createSpinnerType()
	gettimgMessagesString := fmt.Sprintf("Getting messages for chat %v", chat.Title)
	spinner, _ := spinnerType.Start(gettimgMessagesString)

	queries := dbqueries.New(db)
	getLastUpdatebyPeerParams := dbqueries.GetLastUpdateByPeerParams{
		ID:   peerInfo.ID,
		Type: sql.NullInt64{Int64: tg.PeerChatTypeID, Valid: true},
	}
	lastUpdatePerChat, err := queries.GetLastUpdateByPeer(ctx, getLastUpdatebyPeerParams)
	if err != nil && err != sql.ErrNoRows {
		lg.Sugar().Error("error getting last update from db - ignoring...")
	}
	// assumes we get info on a single user (as this is what we asked for)
	user := chatInfo.Users[0].(*tg.User)
	lg.Sugar().Infof("userinfo: (firstname, lastname) - (%v, %v)", user.FirstName, user.LastName)
	lg.Sugar().Info("Getting messages from dialog...")
	peerUser := tg.InputPeerUser{UserID: user.ID}
	searchRequest := tg.MessagesSearchRequest{
		Peer:   &peerUser,
		Limit:  100,
		Filter: &tg.InputMessagesFilterURL{},
		MinID:  int(lastUpdatePerChat.Int64),
	}
	messages, err = client.API().MessagesSearch(ctx, &searchRequest)
	highestOffset, numMessages := getMaxOffset(messages)
	finalMessagesString := fmt.Sprintf("%v...(%v messages received)", gettimgMessagesString, numMessages)
	spinner.Success(finalMessagesString)
	if numMessages != 0 {
		insertDialogParams := dbqueries.InsertDialogParams{
			ID:         peerInfo.ID,
			LastUpdate: sql.NullInt64{Int64: highestOffset, Valid: true},
			Name:       sql.NullString{String: peerInfo.Name, Valid: true},
			Type:       sql.NullInt64{Int64: peerInfo.Type, Valid: true},
		}
		_, err = queries.InsertDialog(ctx, insertDialogParams)
		if err != nil {
			lg.Sugar().Errorf("error inserting user chat information: %v", err)
			return
		}
	}
	return
}

func writeSyncRecord(ctx context.Context, c *Config) error {
	db, err := sql.Open("sqlite3", filepath.Join(c.TgleStateDirectory, dbFilename))
	if err != nil {
		lg.Sugar().Errorf("error opening database: %v", err)
		return err
	}

	// create tables
	if _, err = db.ExecContext(ctx, ddl); err != nil {
		lg.Sugar().Errorf("error creating db tables: %v", err)
		return err
	}
	defer db.Close()

	queries := dbqueries.New(db)

	addTGSyncParams := dbqueries.AddTGSyncParams{
		SyncTime: sql.NullInt64{Int64: time.Now().UnixMilli(), Valid: true},
	}
	_, err = queries.AddTGSync(ctx, addTGSyncParams)
	if err != nil {
		lg.Sugar().Errorf("error adding sync record: %v", err)
		return err
	}
	return nil
}
