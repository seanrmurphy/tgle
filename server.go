package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"

	"github.com/donseba/go-htmx"
	"github.com/donseba/go-htmx/middleware"

	"github.com/seanrmurphy/telegram-bot/dbqueries"
)

type App struct {
	htmx   *htmx.HTMX
	config *Config
}

func runServer(c *Config) {
	// new app with htmx instance
	app := &App{
		htmx:   htmx.New(),
		config: c,
	}

	mux := http.NewServeMux()
	// wrap the htmx example middleware around the http handler
	mux.Handle("/", middleware.MiddleWare(http.HandlerFunc(app.Home)))
	mux.Handle("/links", middleware.MiddleWare(http.HandlerFunc(app.Links)))

	log.Printf("running server on port 3000...")
	err := http.ListenAndServe(":3000", mux)
	log.Fatal(err)
}

func (a *App) Links(w http.ResponseWriter, r *http.Request) {
	// initiate a new htmx handler
	h := a.htmx.NewHandler(w, r)

	dbFullFilename := filepath.Join(a.config.TgleStateDirectory, dbFilename)
	db, err := sql.Open("sqlite3", dbFullFilename)
	if err != nil {
		log.Printf("error opening database: %v", err)
		content := elem.Div(attrs.Props{}, elem.P(attrs.Props{}, elem.Text("unable to open database")))
		// _, _ = h.Write(byte[](returnString))
		_, _ = h.Write([]byte(content.Render()))
		return

	}
	defer db.Close()

	queries := dbqueries.New(db)

	links, err := queries.GetLinks(context.TODO())
	if err != nil {
		log.Printf("error getting links: %v", err)

		swap := htmx.NewSwap().Swap(time.Second * 2).ScrollBottom()

		h.ReSwapWithObject(swap)

		// write the output like you normally do.
		// check the inspector tool in the browser to see that the headers are set.

		content := elem.Div(attrs.Props{}, elem.P(attrs.Props{}, elem.Text("no links found")))
		// _, _ = h.Write(byte[](returnString))
		_, _ = h.Write([]byte(content.Render()))
	}

	// check if the request is a htmx request
	if h.IsHxRequest() {
		// do something
		log.Printf("htmx request - %v", h.Request())
	}

	// check if the request is boosted
	if h.IsHxBoosted() {
		// do something
	}

	// check if the request is a history restore request
	if h.IsHxHistoryRestoreRequest() {
		// do something
	}

	// check if the request is a prompt request
	if h.RenderPartial() {
		// do something
	}

	// set the headers for the response, see docs for more options
	// h.PushURL("http://push.url")
	// h.ReTarget("#ReTarged")
	swap := htmx.NewSwap().Swap(time.Second * 2).ScrollBottom()

	h.ReSwapWithObject(swap)

	// write the output like you normally do.
	// check the inspector tool in the browser to see that the headers are set.
	liElements := elem.TransformEach(links, func(link dbqueries.Link) elem.Node {
		return elem.Li(nil, elem.Text(link.Url))
	})

	ulElement := elem.Ul(nil, liElements...)

	content := elem.Div(attrs.Props{}, ulElement)

	// _, _ = h.Write(byte[](returnString))
	_, _ = h.Write([]byte(content.Render()))
}

func (a *App) Home(w http.ResponseWriter, r *http.Request) {
	// initiate a new htmx handler
	h := a.htmx.NewHandler(w, r)

	// check if the request is a htmx request
	if h.IsHxRequest() {
		// do something
		log.Printf("htmx request - %v", h.Request())
	}

	// check if the request is boosted
	if h.IsHxBoosted() {
		// do something
	}

	// check if the request is a history restore request
	if h.IsHxHistoryRestoreRequest() {
		// do something
	}

	// check if the request is a prompt request
	if h.RenderPartial() {
		// do something
	}

	// set the headers for the response, see docs for more options
	h.PushURL("http://push.url")
	h.ReTarget("#ReTarged")

	// write the output like you normally do.
	// check the inspector tool in the browser to see that the headers are set.
	file, _ := os.ReadFile("index.html")
	_, _ = h.Write(file)
}
