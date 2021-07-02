package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	_ "github.com/lib/pq"
	"github.com/patrickmn/go-cache"
)

var (
	port uint
	dsn  string
)

type application struct {
	db      *sql.DB
	ikCache *cache.Cache
}

func main() {
	flag.UintVar(&port, "port", 8080, "server port")
	flag.StringVar(&dsn, "dsn", "", "postgres data source name")
	flag.Parse()

	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		panic(err)
	}
	if err = db.Ping(); err != nil {
		panic(err)
	}
	defer db.Close()

	app := &application{
		db:      db,
		ikCache: cache.New(10*time.Second, time.Minute),
	}

	mux := http.NewServeMux()
	mux.Handle("/songs", http.HandlerFunc(app.songsHandler))

	addr := fmt.Sprintf(":%d", port)
	fmt.Println("server listen on", addr)
	if err := http.ListenAndServe(addr, logger(mux)); err != nil {
		panic(err)
	}
}

func (app *application) songsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		app.getSongs(w, r)
	case http.MethodPost:
		app.createSong(w, r)
	default:
		app.responseError(w, r, "", http.StatusMethodNotAllowed, false)
	}
}

type song struct {
	ID     int64
	Title  string
	Artist string
	Year   int
}

func (app *application) getSongs(w http.ResponseWriter, r *http.Request) {
	rows, err := app.db.Query("SELECT * FROM songs")
	if err != nil {
		panic(err)
	}

	ss := []*song{}
	for rows.Next() {
		s := &song{}
		err = rows.Scan(&s.ID, &s.Title, &s.Artist, &s.Year)
		if err != nil {
			fmt.Println(err)
			app.responseError(w, r, "", http.StatusInternalServerError, false)
			return
		}
		ss = append(ss, s)
	}

	if err = rows.Err(); err != nil {
		panic(err)
	}

	err = app.writeJson(w, r, http.StatusOK, ss, false)
	if err != nil {
		app.responseError(w, r, "", http.StatusInternalServerError, false)
		return
	}
}

func (app *application) createSong(w http.ResponseWriter, r *http.Request) {
	done, err := app.beforeIdempotencyAPI(w, r)
	if err != nil {
		app.responseError(w, r, err.Error(), http.StatusInternalServerError, false)
		return
	}
	if done {
		return
	}

	var input struct {
		Title  string
		Artist string
		Year   int
	}

	err = json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		fmt.Println(err)
		app.responseError(w, r, "", http.StatusBadRequest, true)
		return
	}

	stmt := "INSERT INTO songs (title, artist, year) VALUES ($1, $2, $3) RETURNING id"
	var id int64
	err = app.db.QueryRow(stmt, input.Title, input.Artist, input.Year).Scan(&id)
	if err != nil {
		fmt.Println(err)
		app.responseError(w, r, "", http.StatusInternalServerError, true)
		return
	}

	stmt = "SELECT * FROM songs WHERE id=$1"
	s := &song{}
	err = app.db.QueryRow(stmt, id).Scan(&s.ID, &s.Title, &s.Artist, &s.Year)
	if err != nil {
		fmt.Println(err)
		app.responseError(w, r, "", http.StatusInternalServerError, true)
		return
	}

	err = app.writeJson(w, r, http.StatusCreated, s, true)
	if err != nil {
		fmt.Println(err)
		app.responseError(w, r, "", http.StatusInternalServerError, true)
		return
	}
}

func (app *application) writeJson(w http.ResponseWriter, r *http.Request, status int, data interface{}, idempotency bool) error {
	js, err := json.Marshal(data)
	if err != nil {
		return err
	}

	js = append(js, '\n')

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(js)

	if idempotency {
		app.afterIdempotencyAPI(r, w.Header(), status, js)
	}
	return nil
}

type Response struct {
	Ready  bool // if response ready to be used
	Header http.Header
	Status int
	Body   []byte
}

func (app *application) beforeIdempotencyAPI(w http.ResponseWriter, r *http.Request) (bool, error) {
	ik := r.Header.Get("Idempotency-Key")
	if ik == "" {
		app.responseError(w, r, "missing header: Idempotency-Key", http.StatusBadRequest, false)
		return true, nil
	}

	v, ok := app.ikCache.Get(ik)
	if !ok {
		app.ikCache.Set(ik, &Response{}, cache.DefaultExpiration)
		dump(app.ikCache)
		return false, nil
	}

	resp := v.(*Response)
	for !resp.Ready {
		v, ok := app.ikCache.Get(ik)
		if !ok {
			app.responseError(w, r, "", http.StatusInternalServerError, false)
			return true, nil
		}
		resp = v.(*Response)
		time.Sleep(45 * time.Millisecond)
	}

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.Status)
	if resp.Body != nil {
		w.Write(resp.Body)
	}
	return true, nil
}

func (app *application) afterIdempotencyAPI(r *http.Request, header http.Header, status int, body []byte) error {
	ik := r.Header.Get("Idempotency-Key")
	if ik == "" {
		return fmt.Errorf(http.StatusText(http.StatusInternalServerError))
	}

	app.ikCache.Replace(ik,
		&Response{Ready: true, Header: header, Status: status, Body: body},
		cache.DefaultExpiration,
	)

	dump(app.ikCache)
	return nil
}

func dump(c *cache.Cache) {
	bb, _ := json.MarshalIndent(c.Items(), "", "  ")
	log.Printf("ikCache: %s\n", bb)
}

func logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s\n", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func (app *application) responseError(w http.ResponseWriter, r *http.Request, msg string, status int, idempotency bool) {
	if msg == "" {
		http.StatusText(status)
	}
	http.Error(w, msg, status)

	if idempotency {
		app.afterIdempotencyAPI(r, w.Header(), status, nil)
	}
}
