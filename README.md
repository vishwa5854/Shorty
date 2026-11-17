# Shorty

A small URL shortener. One Go binary, one SQLite file, a static UI embedded in
the binary. No cgo, no runtime dependencies beyond the pure-Go SQLite driver.

```
shorty.com/abc12  ->  302  ->  https://example.com/whatever/it/was
```

## Run

```sh
go build -o shorty .
./shorty -addr :8080 -db shorty.db -base-url https://shorty.com
```

Then open http://localhost:8080 and paste a URL.

## API

### POST /api/condense

```sh
curl -s -X POST http://localhost:8080/api/condense \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://google.com"}'
# {"url":"https://shorty.com/l7MTp"}
```

Validation: must parse as a URL, scheme must be `http` or `https` (a bare
`example.com/x` is treated as `https://example.com/x`), max 2048 characters.
Errors come back as `{"error":"..."}` with 400.

Shortening the same URL twice returns the same slug; there is a unique index on
`url`.

### GET /{slug}

`302 Found` to the stored target with `Cache-Control: no-store`. Unknown slug
returns 404. Each redirect bumps the `hits` column (no UI for it yet — read it
with `sqlite3 shorty.db 'select * from links order by created_at desc'`).

### GET /api/health

`200 ok` when the database is reachable, `503` otherwise. Useful for a compose
healthcheck.

## Configuration

Flags win over environment variables.

| Flag         | Env                | Default      | Meaning                                            |
|--------------|--------------------|--------------|----------------------------------------------------|
| `-addr`      | `SHORTY_ADDR`      | `:8080`      | listen address                                      |
| `-db`        | `SHORTY_DB`        | `shorty.db`  | SQLite file (WAL mode)                              |
| `-base-url`  | `SHORTY_BASE_URL`  | empty        | public origin used for short links                  |

When `-base-url` is empty the origin is derived from the request, preferring
`X-Forwarded-Proto` / `X-Forwarded-Host` and falling back to `Host`. Setting it
explicitly is still the safest behind a proxy.

## Behind nginx

The app handles plain HTTP; nginx terminates TLS:

```nginx
server {
    listen 443 ssl http2;
    server_name shorty.com;

    # ssl_certificate ... (certbot / your CA)

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host              $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host  $host;
        proxy_set_header X-Real-IP         $remote_addr;
    }
}
```

## Docker

```sh
docker build -t shorty .
docker run -d --name shorty -p 8080:8080 -v shorty-data:/data shorty
```

The image is Alpine plus the static binary; the database lives on the `/data`
volume.

## Notes

- Slug: 5 characters of base62 (62^5 ≈ 916 million). Insert collisions are
  retried; the unique index on `url` makes concurrent duplicates resolve to one
  row.
- SQLite is deliberate: single-writer load, one file to back up, no service to
  babysit. Postgres would add a daemon, credentials and migrations for no
  benefit at homelab scale — revisit only if this is ever run as more than one
  instance.
- The one dependency is `modernc.org/sqlite` (pure Go, no cgo), which keeps the
  binary static and the build free of gcc.

## Layout

```
main.go     server, routing, handlers
store.go    schema, slug generation, queries
web/        index.html, app.js, style.css (embedded via //go:embed)
```
