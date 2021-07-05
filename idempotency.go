package idempotency

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/patrickmn/go-cache"
)

func API(handler http.HandlerFunc) http.Handler {
	return APIWithExiprationConfig(handler, 30*time.Second, 1*time.Minute)
}

func APIWithExiprationConfig(handler http.HandlerFunc, ikExpiration, cleanupInterval time.Duration) http.Handler {
	return &IdempotencyAPI{
		ikCache: cache.New(ikExpiration, cleanupInterval),
		handler: handler,
	}
}

type respCatcher struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (rc *respCatcher) WriteHeader(code int) {
	rc.statusCode = code
	rc.ResponseWriter.WriteHeader(code)
}

func (rc *respCatcher) Write(p []byte) (n int, err error) {
	rc.body.Write(p)
	return rc.ResponseWriter.Write(p)
}

type IdempotencyAPI struct {
	ikCache *cache.Cache
	handler http.HandlerFunc
}

func (i *IdempotencyAPI) responseError(w http.ResponseWriter, r *http.Request, msg string, status int) {
	if msg == "" {
		http.StatusText(status)
	}
	http.Error(w, msg, status)
}

type response struct {
	Ready  bool // if response ready to be used
	Header http.Header
	Status int
	Body   []byte
}

func (i *IdempotencyAPI) getResponse(ik string) (resp *response, err error) {
	v, ok := i.ikCache.Get(ik)
	if !ok {
		err = fmt.Errorf("no valid response for ik: %s", ik)
		return
	}
	resp, ok = v.(*response)
	if !ok {
		err = fmt.Errorf("no valid response for ik: %s", ik)
		return
	}
	return
}

func (i *IdempotencyAPI) dump(prefix string) {
	bb, _ := json.MarshalIndent(i.ikCache.Items(), "", "  ")
	log.Printf("ikCache %s:\n%s\n", prefix, bb)
}

func (i *IdempotencyAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ik := r.Header.Get("Idempotency-Key")
	if ik == "" {
		i.responseError(w, r, "missing header: Idempotency-Key", http.StatusBadRequest)
		return
	}

	err := i.ikCache.Add(ik, &response{}, cache.DefaultExpiration)
	if err != nil {
		// ik existed, response with cached value.
		resp, err := i.getResponse(ik)
		if err != nil {
			i.responseError(w, r, "", http.StatusInternalServerError)
			return
		}
		for !resp.Ready {
			time.Sleep(45 * time.Millisecond)

			resp, err = i.getResponse(ik)
			if err != nil {
				i.responseError(w, r, "", http.StatusInternalServerError)
				return
			}
		}
		i.dump("sending cached response")

		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.Status)
		if resp.Body != nil {
			w.Write(resp.Body)
		}
		return
	}

	i.dump("before api")
	respWriter := &respCatcher{w, &bytes.Buffer{}, http.StatusOK}
	i.handler(respWriter, r)

	i.ikCache.Replace(ik,
		&response{Ready: true, Header: respWriter.Header(), Status: respWriter.statusCode, Body: respWriter.body.Bytes()},
		cache.DefaultExpiration,
	)
	i.dump("after api")
}
