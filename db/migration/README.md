# GO-BINDATA

please use this version of go-bindata

```sh
go install github.com/kevinburke/go-bindata/v4/...@latest
```

Each migration set keeps its SQL files in `db/migration/<name>/sql/`
(numbered `NNN_<desc>.up.sql` / `NNN_<desc>.down.sql`), and they are
embedded into `db/migration/<name>/bindata.go`.

- `make bind-data` regenerates every set, including the notification
  store migrations under `server/notifications/repository/sqlite/migrations/`.
- `make bindata-db DB=<name>` regenerates just one set.

`dummy/` is a test fixture used by the `db/sqlite` package tests.
`make bind-data` does not include it; regenerate it with
`make bindata-db DB=dummy`.
