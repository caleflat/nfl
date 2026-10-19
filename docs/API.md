# Situational analysis API

Status: summary endpoint and Docker workflow are merged. The matching-play endpoint is implemented for review. Team comparison and metadata discovery remain later milestones. Run migrations and import a season before querying it.

## Run with Docker Compose

Start Docker/OrbStack, then run from the repository root:

```sh
docker compose up -d --build --wait
curl -sS 'http://127.0.0.1:8080/healthz'
curl -sS 'http://127.0.0.1:8080/api/v1/summary?season=2025&team=NE&down=3&min_distance=5&max_distance=10'
```

Compose waits for PostgreSQL's health check, runs the migration command successfully, then starts the API and waits for its health check. No shell export of `DATABASE_URL` is needed: containers connect to `db:5432`. The existing named database volume is reused. A fresh database still needs a [season import](../README.md#run-with-docker-compose); without it, the summary returns `season_unavailable`.

The host API port defaults to 8080; set `API_PORT` in `.env` to change it. PostgreSQL's host port defaults to 5433 via `POSTGRES_PORT`. App containers use their explicit internal database URL. Source changes require rerunning `docker compose up -d --build --wait`; there is no automatic reload.

Use `docker compose logs -f api` for logs, `docker compose ps -a` for service state, and `docker compose stop` to stop services while retaining data. The migration container exiting with code 0 is expected. A migration failure prevents API startup; inspect `docker compose logs migrate`.

For host Go development, follow the separate [README instructions](../README.md#host-go-development). The Go server itself checks database connectivity but does not migrate or import automatically. It listens on `127.0.0.1:8080` by default; `-addr` changes it. `-query-timeout` defaults to `5s` and must be positive and at most `15s`. Request logs are JSON on stderr with method, path, status, and duration. SQL error details are not returned to clients. The root URL `/` has no UI and returns a JSON 404; use the routes shown here.

## GET /api/v1/summary

| Parameter | Required | Meaning |
| --- | --- | --- |
| `season` | Yes | Football season, 1999–2100; must be imported |
| `team` | Yes | Canonical uppercase offensive team code, e.g. `NE`, `LA`, `JAX` |
| `down` | Yes | One down, 1–4 |
| `min_distance` | No | Inclusive minimum yards to go; default 0 |
| `max_distance` | No | Inclusive maximum yards to go; default 100 |

Bounds must satisfy `0 <= min_distance <= max_distance <= 100`. Unknown parameters, malformed query strings, duplicate keys, empty values, invalid integers, unsupported codes, and invalid ranges return 400. A known team with no matching plays returns a valid empty result; a season without a publication returns 404. Lowercase team codes are rejected rather than silently normalized. Null distance does not match a range, including the default bounds.

Only the eligible population defined in [the data contract](DATA_CONTRACT.md) contributes. This endpoint is regular-season only. Every request uses one parameterized SQL statement to read both aggregates and publication metadata from a single PostgreSQL statement snapshot.

## Response

The top-level response contains:

- `filters`: the effective filters, including default bounds.
- `dataset`: season, publication time, source SHA-256, and contract version. These identify the imported snapshot behind the result; the upstream source may later change.
- `play_count`: matching eligible offensive plays.
- `yards_per_play`: `value`, `sum`, valid `count`, and `missing` relative to matching plays.
- `epa`: `per_play`, `total`, valid `count`, `missing`, `successful_plays`, and `success_rate`. Success means strictly positive EPA. Missing EPA never counts as a failure.
- `identified_target_attempts`: matching plays with `pass_attempt = true`, `sack = false`, and an identified receiver.
- `adot` and `cpoe`: each has `value`, `sum`, valid `count`, and `missing` relative to the identified-target subset. Non-target plays do not inflate these missing counts.
- `conversion_rate`: `value`, `converted`, `failed`, `classified`, and `unclassified` on down 3 or 4. It is null on down 1 or 2.

Rates are fractions from 0 to 1, not percentages. CPOE is already in percentage points; a value of 3 means three percentage points above expectation. Yardage and model values preserve calculation precision; display rounding belongs in the client. `epa.total` is volume-dependent. EPA and CPOE use published nflfastR model outputs with attribution, not locally trained models.

When a metric has no valid observations, its mean/rate and sum/total are JSON null. Counts remain zero. An empty third-down result still has a conversion object with a null rate and zero counts. A nonempty first-down sample missing every EPA observation has `epa.count = 0` and `epa.missing = play_count`; it differs from an empty sample.

For aDOT, retain negative and zero air yards and include identified incomplete/intercepted targets. The identified-target restriction can omit real targets with missing receiver attribution. For conversions, exactly one corresponding source flag must be true; both false, null flags, and conflicting flags are unclassified rather than failures. The API does not infer conversion from yards gained.

## GET /api/v1/plays

Uses the same required `season`, `team`, and `down` filters and inclusive optional distance bounds as the summary. Additional parameters:

| Parameter | Default | Meaning |
| --- | --- | --- |
| `limit` | 25 | Page size, 1–100 |
| `offset` | 0 | Number of matching plays to skip, 0–100000 |
| `source_sha256` | Omitted | Optional expected dataset hash, 64 lowercase hexadecimal characters |

```sh
curl -sS 'http://127.0.0.1:8080/api/v1/plays?season=2025&team=NE&down=3&min_distance=5&max_distance=10&limit=25&offset=0'
```

The response contains effective `filters`, `dataset` provenance, `pagination` (`limit`, `offset`, total matching plays, nullable `next_offset`), and a `plays` array. Use `next_offset` for subsequent pages while preserving filters and the expected hash. Order is ascending `game_id`, then numeric `play_id`. The compound key makes ordering deterministic. There is no configurable sort or cursor. Bounded offset pagination fits the current single-team, single-down season samples; this is not a bulk export API.

Records include game/play identifiers; week, date, home/away/offensive/defensive teams; quarter/clock, down/distance, field position, score differential, description, and play type; raw yardage, EPA, air yards, and CPOE; `identified_target`; sack/scramble/penalty/completion/interception/touchdown flags; and `conversion_status`. Optional values remain JSON null. Dates use `YYYY-MM-DD`. Conversion status is `converted`, `failed`, `unclassified`, or `not_applicable` under the summary rules. Raw air yards/CPOE on non-target plays do not contribute to summary passing metrics; use `identified_target` to interpret them.

Empty samples return `plays: []`, total 0, and no next offset. Beyond-end offsets return an empty array with the actual matching total. The total equals the summary play count for the same filters and dataset; individual metric counts may be smaller.

### Consistency across pages and summaries

Metadata, count, and page use one read-only repeatable-read PostgreSQL transaction, so a concurrent reimport cannot mix publications within a response.

Pass the summary's `dataset.source_sha256` with the first and every subsequent play request to browse that source snapshot. If the current hash differs, HTTP 409 `dataset_changed` instructs the client to refresh the summary and restart pagination. Old publications are not retained. Reimporting identical bytes changes publication time but not the hash and does not invalidate paging. Without the optional hash, pages can change between requests even though each response is internally consistent.

The hash binds the source, not the filters: clients must preserve filters while paging. This API uses contract version 1; retain/check the returned contract version too. Contract changes require a separate compatibility decision.

## Errors and methods

Errors use this shape:

```json
{"error":{"code":"season_unavailable","message":"season has not been imported"}}
```

| Status | Code | Meaning |
| --- | --- | --- |
| 400 | `invalid_filters` | Missing/malformed/unsupported filter syntax or range |
| 400 | `unknown_team` | Code is well formed but not in the supported team table |
| 404 | `season_unavailable` | No committed import for the requested season |
| 404 | `not_found` | Unknown route |
| 405 | `method_not_allowed` | Only GET is supported; response includes `Allow: GET` |
| 409 | `dataset_changed` | Expected play dataset hash differs; restart with a fresh summary |
| 504 | `query_timeout` | Database request exceeded its deadline |
| 500 | `internal_error` | Unexpected database failure; details remain in server logs |

JSON responses use `Cache-Control: no-store` because source publications can change. The query string is capped at 2,048 characters. Server read-header/read/write/idle timeouts are 5/10/20/60 seconds, with a 16 KiB header cap. Client disconnects propagate cancellation to the query. Interrupt/termination signals cancel active request contexts and allow up to five seconds for server shutdown.

## GET /healthz

Returns `{"status":"ok"}` while the HTTP handler is running. This is process liveness, not a database readiness check; it makes no database query. A separate readiness endpoint can be added when deployment requires one.

## Validation and current limits

Hand-calculated PostgreSQL fixtures cover denominators, null versus zero, target eligibility, conversion rules, distance bounds, and filter isolation. Play tests walk pages without gaps/duplicates, compare their total with the summary, verify numeric key order and context/nulls, and reject stale hashes. Both endpoints test blocked real SQL queries, timeout/cancellation, and pool recovery. Unit tests cover validation, pagination bounds, errors, and context propagation. Import tests use the same isolated-schema helper.

This milestone adds play inspection and bounded pagination, without query indexes, caching, team comparison, or frontend. Performance claims require a later measured workload.
