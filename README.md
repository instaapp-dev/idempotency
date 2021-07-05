# Idempotency

An API is idempotency means same requests sending to the API gets same response and side effect.

For example, `POST /songs/create` creates a new song. For any reason, if client sends multiple requests to create same song, only one song should be created, and client should get same response of that created song.

This package provides a simple middleware to achieve this. On client side, generate a high entropy random string like this:

```shell
openssl rand -hex 32
```

And sending request with HTTP header:

```
Idempotency-Key: 18c691197e040223aa72bf63c85fa65441d2ac6772f0d04f6cde9619a7c1b50e
```

On server side, wrap API handler with middleware like this:

```go
mux := http.NewServeMux()
mux.Handle("/songs/create", idempotency.API(createSongHandler))
http.ListenAndServe(":4000", mux)
```

It creates memory cache ([go-cache](https://github.com/patrickmn/go-cache)) for this API, which map from idempotency key (IK) to response (including status code, headers, and response body).

For any incoming request, the middleware checks if `ik` exists in cache. If found, return cached `response`. If not, the API handler is called. When handler returns, `response` is cached under `ik`.

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

# More Details

- IK lifetime: the ik cache has default expiration time of 30 seconds, you can customize this by calling idempotency.APIWithConfig.
- Minimum IK length is also configurabel with idempotency.APIWithConfig.
- Goroutine safe: only one request (may not necessarily be first request) can add ik to cache, other requests must wait for response to be ready.

Idempotency can be more sophisticated, as presented in this excellent article: [Implementing Stripe-like Idempotency Keys in Postgres](https://brandur.org/idempotency-keys). This package just provides minimum idempotency support.

# TODO

- gRPC middleware (intercepter): how to intercept single API (like rest API example above) instead of ALL APIs?
- For multiple instances of server, we need ik cache cross instances, where redis should be a good choice.
