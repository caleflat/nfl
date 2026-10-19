# Situational summary API

Status: first endpoint, prepared for review. The matching-play list, team comparison, and metadata discovery endpoints are later milestones. Run migrations and import a season before querying it.

## Run locally

Load `DATABASE_URL` as described in the README, then:

```sh
go run ./cmd/serve
curl -sS 'http://127.0.0.1:8080/healthz'
curl -sS 'http://127.0.0.1:8080/api/v1/summary?season=2025&team=NE&down=3&min_distance=5&max_distance=10'
```

The server listens on `127.0.0.1:8080` by default; `-addr` changes it. `-query-timeout` defaults to `5s` and must be positive and at most `15s`. The server checks database connectivity at startup; it does not migrate or import automatically. Request logs are JSON on stderr with method, path, status, and duration. Query errors are logged internally; SQL details are not returned to clients.

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
| 504 | `query_timeout` | Database request exceeded its deadline |
| 500 | `internal_error` | Unexpected database failure; details remain in server logs |

JSON responses use `Cache-Control: no-store` because source publications can change. The query string is capped at 2,048 characters. Server read-header/read/write/idle timeouts are 5/10/20/60 seconds, with a 16 KiB header cap. Client disconnects propagate cancellation to the query. Interrupt/termination signals cancel active request contexts and allow up to five seconds for server shutdown.

## GET /healthz

Returns `{"status":"ok"}` while the HTTP handler is running. This is process liveness, not a database readiness check; it makes no database query. A separate readiness endpoint can be added when deployment requires one.

## Validation and current limits

Hand-calculated PostgreSQL fixtures cover metric denominators, null versus zero, negative air yards, target eligibility, positive-EPA success, conversion conflicts, inclusive distance bounds, and filter isolation. An end-to-end handler test uses PostgreSQL, blocks a query with a table lock, and verifies HTTP timeout, SQL cancellation, and subsequent pool usability. Unit tests cover request validation, errors, and context propagation. Existing import tests use the same isolated-schema helper.

This release does not add query indexes, caching, pagination, play listings, or cross-team comparisons. Performance claims require a later measured workload. The endpoint exposes the first usable backend slice; matching-play inspection and comparison will build on it after review.
