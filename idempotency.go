/* This package provides simple middleware for any http.Handler to maintain idempotency.
 * Call API to use default config.
 * Call APIWithConfig to configure for IK lifetime, cache cleanup interval, and minimum IK length.
 */
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

const (
	DefExpiration      = 30 * time.Second
	DefCleanupInterval = 1 * time.Minute
	DefMinIKLength     = 32
)

// API returns middleware for http.ServeMux, and ensure idempotency for handler.
func API(handler http.HandlerFunc) http.Handler {
	return APIWithConfig(handler, DefExpiration, DefCleanupInterval, DefMinIKLength)
}

// APIWithConfig works just like API, with configuration: expiration, cleanupInterval, and minIKLen.
func APIWithConfig(handler http.HandlerFunc, expiration, cleanupInterval time.Duration, minIKLen int) http.Handler {
	return &idempotencyAPI{
		ikCache:  cache.New(expiration, cleanupInterval),
		handler:  handler,
		minIKLen: minIKLen,
	}
}

// idempotencyAPI holds info in order to achieve idempotency for handler.
type idempotencyAPI struct {
	ikCache  *cache.Cache // map ik to response.
	handler  http.HandlerFunc
	minIKLen int
}

// ServeHTTP handles request to target API, checking if ik exists in ikCache, if yes returns cached response.
// If not, call handler, and saves response to cache under key: ik.
func (i *idempotencyAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ik := r.Header.Get("Idempotency-Key")
	if ik == "" {
		i.responseError(w, r, "missing header: Idempotency-Key", http.StatusBadRequest)
		return
	}
	if len(ik) < i.minIKLen {
		i.responseError(w, r, fmt.Sprintf("Minimum idempotency key length: %d", i.minIKLen), http.StatusBadRequest)
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

		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.Status)
		if resp.Body != nil {
			w.Write(resp.Body)
		}
		return
	}

	respWriter := &respCatcher{w, &bytes.Buffer{}, http.StatusOK}
	i.handler(respWriter, r)

	i.ikCache.Replace(ik,
		&response{Ready: true, Header: respWriter.Header(), Status: respWriter.statusCode, Body: respWriter.body.Bytes()},
		cache.DefaultExpiration,
	)
}

// API response, including status code, header, and body.
type response struct {
	Ready  bool // if response ready to be used
	Header http.Header
	Status int
	Body   []byte
}

func (i *idempotencyAPI) getResponse(ik string) (resp *response, err error) {
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

// Extension to http.ResponseWriter for caching status code and response body.
// Notice we can get header with http.ResponseWriter.Header().
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

func (i *idempotencyAPI) responseError(w http.ResponseWriter, r *http.Request, msg string, status int) {
	if msg == "" {
		http.StatusText(status)
	}
	http.Error(w, msg, status)
}

func (i *idempotencyAPI) dump(prefix string) {
	bb, _ := json.MarshalIndent(i.ikCache.Items(), "", "  ")
	log.Printf("ikCache %s:\n%s\n", prefix, bb)
}
