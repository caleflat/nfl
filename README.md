# NFL Situational Play Explorer

A Go/PostgreSQL project for exploring how NFL offenses perform in selected game situations. The backend is the primary focus; a small React interface is planned.

**Current state:** source inspection, PostgreSQL schema/migrations, and a local CSV importer. There is no API or frontend yet. See [the project plan](PROJECT_PLAN.md), [data contract](docs/DATA_CONTRACT.md), [source findings](docs/SOURCE_INSPECTION.md), and [database/import design](docs/IMPORTING.md).

## Run the source inspection

Prerequisites: Go 1.26 or newer and `curl`. No Python, database, credentials, or paid subscription is required for this step.

```sh
mkdir -p data/raw
curl -fL --retry 2 -o data/raw/play_by_play_2025.csv \
  https://github.com/nflverse/nflverse-data/releases/download/pbp/play_by_play_2025.csv
go run ./cmd/inspect -file data/raw/play_by_play_2025.csv
go test ./...
go vet ./...
```

The inspection command streams the CSV and outputs JSON containing its SHA-256, dimensions, duplicate identifiers, season/play types, and coverage under the proposed regular-season offensive-play policy. It validates required headers and measured numeric fields. It is a profiling tool, not the production importer; complete domain validation and database constraints belong to the next milestone. Large raw files are ignored by Git. Keep the emitted hash when comparing runs because upstream releases can change.

## Review workflow

- Keep `main` as the reviewed baseline.
- Use focused feature branches and pull requests for implementation milestones.
- Commit coherent changes with test evidence in the PR description.
- Review and merge each milestone before starting work that depends on it.
- Do not commit downloaded datasets, local configuration secrets, or generated binaries.

The first PR established source findings and metric rules. The second adds database setup and atomic imports. API and aggregate calculations follow separately.

## Start PostgreSQL and import a season

Prerequisites: Go 1.26+, Docker with Compose, and the CSV downloaded above. The importer reads a local file; it does not download data. Local credentials are development defaults, and PostgreSQL is bound to loopback on port 5433.

```sh
cp .env.example .env
set -a
. ./.env
set +a
docker compose up -d --wait
go run ./cmd/migrate
go run ./cmd/import -season 2025 -file data/raw/play_by_play_2025.csv \
  -source https://github.com/nflverse/nflverse-data/releases/download/pbp/play_by_play_2025.csv
```

Rerun the import to replace the season, including corrections and removed plays. Only regular-season records are published. Import failure preserves existing data and provenance. The command prints a JSON publication summary on success; errors and elapsed time go to stderr. `-timeout` defaults to five minutes and includes waiting for another import. Ctrl-C cancels the database transaction.

Pass `-source-obtained-at 2026-10-19T12:00:00Z` with the actual acquisition time if known. Otherwise, the source acquisition timestamp defaults to the file modification time, which is only a proxy. `.env` is loaded by the shell commands above, not automatically by the Go programs. If changing `POSTGRES_PORT`, update `DATABASE_URL` to match.

Check the publication:

```sh
docker compose exec -T db psql -U nfl -d nfl -c \
  'SELECT season, source_rows, stored_rows, eligible_rows, source_sha256 FROM season_imports;'
```

Stop the database with `docker compose stop`; its named volume retains the data.

## Tests

```sh
go test ./...
TEST_DATABASE_URL="$DATABASE_URL" go test -race -count=1 ./...
go vet ./...
go build ./...
```

Without `TEST_DATABASE_URL`, integration tests skip explicitly. With it, tests create isolated temporary schemas and remove only those schemas afterward. Use a local/test database and a role allowed to create schemas. CI runs these tests against PostgreSQL 18; it does not download an NFL dataset.

## Data attribution

Play-by-play data and published model outputs come from [nflverse](https://github.com/nflverse/nflverse-data) and [nflfastR](https://www.nflfastr.com/). The upstream data repository identifies its license as [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/). This project filters and aggregates those records; it does not create the upstream expected-points or completion-probability models. See the inspection notes for the exact source and snapshot hash. No raw season data is redistributed in this repository.
