# Devices API

[![CI](https://github.com/gtalha07/api-device-management/actions/workflows/ci.yml/badge.svg?branch=mainline)](https://github.com/gtalha07/api-device-management/actions/workflows/ci.yml)

A REST API in Go for managing devices and their lifecycle state, backed by
PostgreSQL. Subscribers are notified of every state change once it has been
saved.

- Create, fully or partially update, fetch, list (filter by brand and/or
  state) and delete devices.
- Business rules: creation time never changes; name and brand can't change
  while a device is `in-use`; `in-use` devices can't be deleted.
- Documented with [OpenAPI 3.1](api/openapi.yaml), containerized, and checked
  in CI.

## Quick start

Requires Docker.

```sh
docker compose up -d --build
```

The API listens on `http://localhost:8080` and migrates the database on
startup.

```sh
curl -i -X POST localhost:8080/devices -d '{"name":"Phone X","brand":"Acme"}'
curl -s "localhost:8080/devices?state=available"
curl -s -X PATCH localhost:8080/devices/<id> -d '{"state":"in-use"}'
docker compose logs api      # shows the "device state changed" event
docker compose down          # add -v to also delete the database volume
```

## API

| Method | Path | Description | Success |
| --- | --- | --- | --- |
| `POST` | `/devices` | Create a device (`state` defaults to `available`) | `201` + `Location` |
| `GET` | `/devices?brand=&state=` | List devices, oldest first; filters are optional | `200` |
| `GET` | `/devices/{id}` | Get one device | `200` |
| `PUT` | `/devices/{id}` | Replace name, brand and state (all required) | `200` |
| `PATCH` | `/devices/{id}` | Change only the fields sent | `200` |
| `DELETE` | `/devices/{id}` | Delete a device | `204` |
| `GET` | `/healthz` | Readiness: can the service reach the database | `200` / `503` |

A device:

```json
{
  "id": "3e3d6681-72d4-4654-a3a8-64eaba8d1c1d",
  "name": "Phone X",
  "brand": "Acme",
  "state": "available",
  "createdAt": "2026-09-26T16:00:58.652868Z"
}
```

`state` is one of `available`, `in-use`, `inactive`. `id` and `createdAt` are
set by the server; sending them is rejected, as is any unknown field.

Errors always look like `{"error": "<message>"}`:

| Status | When |
| --- | --- |
| `400` | Malformed JSON, unknown or missing field, blank name/brand, unknown state, malformed id |
| `404` | No device with that id |
| `409` | The change conflicts with the device being `in-use` |
| `500` | Unexpected error; details are logged, never returned |

The full contract, with every schema and example, is in
[`api/openapi.yaml`](api/openapi.yaml). To browse it:

```sh
npx @redocly/cli preview-docs api/openapi.yaml
```

## Local development

Requirements: Go 1.25+, Docker. Optional: [golang-migrate CLI][migrate]
(`brew install golang-migrate`) for the `migrate-*` and `db-reset` targets,
and [golangci-lint][golangci] (`brew install golangci-lint`) for `make lint`.

| Command | What it does |
| --- | --- |
| `make run` | Start Postgres, then run the API on `:8080` |
| `make test` | Unit tests (database tests are skipped) |
| `make test-integration` | All tests, including against Postgres |
| `make cover` | All tests with coverage; writes `coverage.html` |
| `make lint` | golangci-lint (formatting, vet, static analysis, security) |
| `make db-up` / `make db-down` | Start / stop the Postgres container |
| `make db-reset` | Drop the local schema and re-apply all migrations |
| `make migrate-up` / `make migrate-down` | Apply all / roll back one migration |

Postgres is published on host port **5433** (not 5432) so it doesn't clash
with a locally installed Postgres. Inside Compose, the API reaches it at
`db:5432`.

### Configuration

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `DATABASE_URL` | yes | – | Postgres URL, e.g. `postgres://devices:devices@localhost:5433/devices?sslmode=disable` |
| `ADDR` | no | `:8080` | Listen address |
| `TEST_DATABASE_URL` | tests only | – | Enables the Postgres integration tests |

The credentials in `compose.yaml` and the Makefile are for local development
only; in production they come from a secret store, with `sslmode=require` or
stricter.

## Testing

- **Unit tests** cover the service rules and every HTTP handler with a fake
  repository and notifier.
- **Integration tests** run the repository against real Postgres, including a
  test that races two concurrent updates to prove the row lock works. They
  run only when `TEST_DATABASE_URL` is set and migrate the schema themselves.
- **CI** ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) runs on
  every push and pull request: `go mod tidy` check, golangci-lint, OpenAPI
  lint, govulncheck, tests with `-race` against a Postgres service container
  (coverage summary and HTML report), and a Docker build.

Coverage is about 76% overall (`internal/device` 92%); `cmd/api` is wiring
and not unit-tested.

## Project structure

```
cmd/api/            main: config, wiring, HTTP server, graceful shutdown
internal/device/    model, business rules (service), Postgres repository, HTTP handlers
internal/database/  connection pool with startup ping; embedded migrations runner
internal/notify/    Notifier implementation (structured log events)
migrations/         SQL migrations (golang-migrate), embedded into the binary
api/openapi.yaml    API specification
```

## Design decisions

- **Rules are checked under a row lock.** Update and delete run inside a
  transaction after `SELECT ... FOR UPDATE`, so concurrent requests can't slip
  a change between the in-use check and the write.
- **Notifications are sent only after commit,** so subscribers never hear
  about a change that was rolled back.
- **The database enforces the rules too:** `CHECK` constraints for non-blank
  name/brand and valid state, and `created_at` is never part of an `UPDATE`.
- **Standard library HTTP and manual wiring:** Go 1.22+ routing, domain errors
  mapped to status codes in one place, and `main` as the only place that knows
  the concrete implementations.
- **Production basics:** fail fast if the database is unreachable, server
  timeouts, a request size limit, JSON logs, graceful shutdown, and a small
  distroless non-root image.

## Limitations and next steps

Each item is also a `TODO` next to the relevant code.

- **Notifications are at-most-once.** A crash between commit and notify loses
  the event; a transactional outbox would make delivery at-least-once. The
  notifier writes structured logs and would be replaced by a real transport
  (webhook, message broker).
- **No pagination** on `GET /devices`; keyset pagination on
  `(created_at, id)` would bound responses.
- **No authentication, authorization or rate limiting.**
- **Lost updates on `PUT`** when two clients edit from the same stale read;
  `ETag` + `If-Match` would prevent it.
- **Brand filter is exact and case-sensitive.**
- **Oversized bodies return 400**, not 413.
- **Migrations run at startup**, which suits a single service; with many
  replicas they'd move to a separate deploy step.
- **`/healthz` is readiness only**; a separate liveness probe would avoid
  restarts during a database outage.
- **Integration tests share the local development database** and truncate
  it; a dedicated test database or testcontainers would isolate them.

[migrate]: https://github.com/golang-migrate/migrate
[golangci]: https://golangci-lint.run
