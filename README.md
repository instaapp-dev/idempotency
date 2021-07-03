# idempotency

Idempotency 在後端開發指的是同一個 request 執行多次，會得到同樣的結果。

Idempotency 可以簡單達成，也可以很複雜，要看這個 request 會驅動多少事情。若只改動到 local instance DB 則容易。若牽涉到外部 (例如其它 microservice 甚至 3rd party service) 狀態變化，則較為複雜。

我在這邊先實做較簡單的情境：呼叫多次 `POST /songs/create` 只能新增一首歌。

## Prerequests

- PostgreSQL
- [Migration](https://github.com/golang-migrate/migrate/tree/master/cmd/migrate)

## Usage

1. Create postgres database: `createdb test`
2. Migration: `migrate -path=./migrations -database=postgres://michael:michael@localhost/test up` 
3. Run server: `go run ./cmd/server --dsn=postgres://michael:michael@localhost/test`
4. Run test script: `./test.sh`
5. Check if only one song created from client output.

Note: replace 'michael:michael' above with your postgres user and password.
