# Database and season imports

## Scope

This document covers local-file ingestion for the contract version 1 regular-season dataset. It does not implement automatic downloads. PostgreSQL 18, the migration command, and the API now run through Compose locally; see the [README](../README.md#run-with-docker-compose) for the default workflow and [API documentation](API.md) for query behavior. The optional `importer` service runs on demand with `docker compose run --rm importer ...` and reads from the host `data/raw` directory mounted read-only at `/data`.

## Schema decisions

- `teams` contains the 32 current source team codes. Foreign keys reject unknown codes. Supporting historical aliases requires an explicit future schema/data change.
- `season_imports` holds one current successful publication per season: declared source, acquisition timestamp, SHA-256 of the entire input file, publication timestamp, contract version, and coverage counts.
- `plays` stores all regular-season records, including excluded administrative/special-teams records, with `(game_id, play_id)` as its primary key. It retains the typed fields from the contract and a derived `is_eligible` flag. Import and inspection use the same Go eligibility function.
- Optional measurements and flags remain SQL NULL when missing. Integer-valued source quantities use integers. Model outputs use PostgreSQL `double precision`, preserving float64 precision without display rounding. CPOE remains in percentage points.
- Constraints cover identifiers, season/type, teams, basic ranges, and publication counts. Parser validation additionally rejects non-finite measurements, malformed dates/integers, nonbinary flags, and missing classification flags on run/pass records.
- The only additional play index is on season to support replacement/FK operations. API-specific indexes await measured query patterns.

Source row count includes both REG and POST rows in the file. Stored count is REG only; eligible count applies the analytical policy. Missing-distance count refers only to eligible plays. Metric-specific coverage is available in the original profiler and will be added to query responses in the metrics milestone.

## Migrations

`go run ./cmd/migrate` applies embedded numbered SQL files in filename order. The small runner stores checksums and applies pending files in a transaction under a migration advisory lock. Rerunning is safe; editing an already applied file fails. Schema changes require a new forward migration. Down migrations and automatic migration during import are intentionally not provided.

Migration files are embedded in binaries, so commands work independently of the current directory. Application startup checks connectivity with a five-second timeout. Migrations have a one-minute deadline. The connection pool allows four open/idle connections and recycles connections after 30 minutes; these are initial CLI defaults, not tuned production settings.

## Atomic replacement

1. Validate command options and CSV headers.
2. Begin a transaction and take an exclusive transaction-scoped advisory lock for the selected season.
3. Delete the prior season publication and its plays via cascading FK; insert provisional metadata inside the transaction.
4. Stream and validate every source record. Reject duplicate game/play pairs, a mixed season, unsupported season type/team, malformed values, and database constraint failures. POST records are validated but not stored.
5. Insert REG records through a parameterized prepared statement. Count qualifying plays with the shared eligibility policy. Compute the input hash as the original bytes are read.
6. Reject a source without any REG records. Finalize publication metadata and commit.

Any error rolls the transaction back, including its provisional metadata. Reader queries see the previous committed season until commit. Same-season importers serialize; different seasons use different advisory locks. Read-committed API code making several queries must still choose a consistent snapshot when it needs a single publication across those queries.

The whole import runs in one transaction, including parsing. This is simple and sufficient for a single season at the current scale. It holds locks and generates replacement writes even when the input is unchanged. Staging/COPY, checksum no-op optimization, and retaining a full import history are possible later changes if measurements justify them. The current implementation does not hold the full CSV in memory, but keeps a set of identifiers to detect duplicates.

## Provenance and failure reporting

The source is caller-declared metadata; the importer does not fetch or verify that URL. The hash identifies the actual input bytes, including skipped POST records. Supply the actual acquisition timestamp when available; file modification time is the documented fallback. A fresh publication timestamp is recorded even on identical reimports.

Only successful current publication metadata is stored. Failures are reported to stderr with record context and a nonzero exit code; there is no durable failed-attempt history yet. The five-minute import timeout includes lock wait and database work. The local-file command responds to interrupt/termination signals. An incomplete file ending at a valid record boundary cannot be distinguished from an intentionally smaller corrected dataset; successful validation does not prove upstream completeness.

## Verification performed

- Unit tests preserve nulls, zero EPA, and negative air yards and reject malformed scalar/classification data.
- Real PostgreSQL tests verify repeated imports, corrections, removed plays, untouched neighboring seasons, rollback after late failures, provenance preservation, empty-source rejection, advisory-lock cancellation, reader visibility, repeated migrations, and migration checksum checks.
- A full local 2025 import published 46,452 regular-season rows from 48,771 source rows; 32,726 qualify for analysis, with no missing distance among eligible plays. Its hash matches the source-inspection snapshot.
- An identical full-season reimport preserved the same hash and 46,452 stored rows with no duplicates. The local imports took roughly 13–15 seconds; this is an operational observation, not a benchmark or a project performance claim.
