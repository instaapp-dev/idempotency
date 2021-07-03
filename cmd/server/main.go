package main

import (
	"bytes"
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
	mux.Handle("/songs/list", app.listSongsHandler())
	mux.Handle("/songs/create", app.idempotencyAPI(app.createSongHandler))

	addr := fmt.Sprintf(":%d", port)
	fmt.Println("server listen on", addr)
	if err := http.ListenAndServe(addr, logger(mux)); err != nil {
		panic(err)
	}
}

type song struct {
	ID     int64
	Title  string
	Artist string
	Year   int
}

func (app *application) listSongsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				app.responseError(w, r, "", http.StatusInternalServerError)
				return
			}
			ss = append(ss, s)
		}

		if err = rows.Err(); err != nil {
			panic(err)
		}

		err = app.writeJson(w, r, http.StatusOK, ss)
		if err != nil {
			app.responseError(w, r, "", http.StatusInternalServerError)
			return
		}
	})
}

func (app *application) createSongHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Title  string
		Artist string
		Year   int
	}

	err := json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		fmt.Println(err)
		app.responseError(w, r, "", http.StatusBadRequest)
		return
	}

	stmt := "INSERT INTO songs (title, artist, year) VALUES ($1, $2, $3) RETURNING id"
	var id int64
	err = app.db.QueryRow(stmt, input.Title, input.Artist, input.Year).Scan(&id)
	if err != nil {
		fmt.Println(err)
		app.responseError(w, r, "", http.StatusInternalServerError)
		return
	}

	stmt = "SELECT * FROM songs WHERE id=$1"
	s := &song{}
	err = app.db.QueryRow(stmt, id).Scan(&s.ID, &s.Title, &s.Artist, &s.Year)
	if err != nil {
		fmt.Println(err)
		app.responseError(w, r, "", http.StatusInternalServerError)
		return
	}

	err = app.writeJson(w, r, http.StatusCreated, s)
	if err != nil {
		fmt.Println(err)
		app.responseError(w, r, "", http.StatusInternalServerError)
		return
	}
}

func (app *application) writeJson(w http.ResponseWriter, r *http.Request, status int, data interface{}) error {
	js, err := json.Marshal(data)
	if err != nil {
		return err
	}

	js = append(js, '\n')

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(js)
	return nil
}

type Response struct {
	Ready  bool // if response ready to be used
	Header http.Header
	Status int
	Body   []byte
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

func (app *application) responseError(w http.ResponseWriter, r *http.Request, msg string, status int) {
	if msg == "" {
		http.StatusText(status)
	}
	http.Error(w, msg, status)
}

type idempotencyWriter struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (w *idempotencyWriter) WriteHeader(code int) {
	fmt.Printf("WriteHeader code: %d\n", code)
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *idempotencyWriter) Write(p []byte) (n int, err error) {
	fmt.Printf("Write p: %s\n", string(p))
	w.body.Write(p)
	return w.ResponseWriter.Write(p)
}

func (app *application) idempotencyAPI(api http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ik := r.Header.Get("Idempotency-Key")
		if ik == "" {
			app.responseError(w, r, "missing header: Idempotency-Key", http.StatusBadRequest)
			return
		}

		v, ok := app.ikCache.Get(ik)
		if ok {
			resp := v.(*Response)
			for !resp.Ready {
				v, ok := app.ikCache.Get(ik)
				if !ok {
					app.responseError(w, r, "", http.StatusInternalServerError)
					return
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
			return
		}

		app.ikCache.Set(ik, &Response{}, cache.DefaultExpiration)
		dump(app.ikCache)

		iw := &idempotencyWriter{w, &bytes.Buffer{}, http.StatusOK}
		api(iw, r)

		app.ikCache.Replace(ik,
			&Response{Ready: true, Header: iw.Header(), Status: iw.statusCode, Body: iw.body.Bytes()},
			cache.DefaultExpiration,
		)
		dump(app.ikCache)
	})
}
