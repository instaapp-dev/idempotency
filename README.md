# Idempotency

An API is idempotency means same requests to the API gets same response and side effect.

For example, `POST /songs/create` creates a new song. For any reason, if client sends multiple requests to create same song, only one song should be created, and client should get same response.

This package provides a simple middleware to achieve this:

```go
	mux := http.NewServeMux()
	mux.Handle("/songs/create", idempotency.API(createSongHandler))
	http.ListenAndServe(":4000", mux)
```

It creates memory cache ([go-cache](https://github.com/patrickmn/go-cache)) for this API, which map from idempotency key (IK) to response (including status code, headers, and response body).

For any request, the middleware checks if `ik` exists in cache. If found, return cached `respons`. If not, the API handler is called. When handler returns, `response` is cached under `ik`.

# To Run Sample Server

## Prerequest

- Go
- PostgreSQL
- [Migration](https://github.com/golang-migrate/migrate/tree/master/cmd/migrate)

## Steps

1. Create postgres database: `createdb test`
2. Migration: `migrate -path=./migrations -database=postgres://user:pass@localhost/test up` 
3. Run server: `go run ./cmd/server --dsn=postgres://user:pass@localhost/test`
4. Run test script: `./test.sh`
5. Check if only two songs are created.

Note: replace 'user:pass' above with your postgres user and password.

# More

- Customize: you can set ik life duration by calling `APIWithConfig` and set ik duration and cache clean interval.
- Goroutine safe: only one request (may not necessarily be first request) can add ik to cache, other requests must wait for response to be ready.

Idempotency can be more sophisticated, as presented in this excellent article: [Implementing Stripe-like Idempotency Keys in Postgres](https://brandur.org/idempotency-keys). This package just provides minimum idempotency support.

# TODO

- gRPC middleware (intercepter): how to intercept single API (like rest API example above) instead of ALL APIs?

