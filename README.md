# NFL Situational Play Explorer

A Go/PostgreSQL project for exploring how NFL offenses perform in selected game situations. The backend is the primary focus; a small React interface is planned.

**Current state:** source inspection, PostgreSQL schema/migrations, a local CSV importer, summary metrics, and paginated matching plays. There is no frontend yet. See [the project plan](PROJECT_PLAN.md), [data contract](docs/DATA_CONTRACT.md), [source findings](docs/SOURCE_INSPECTION.md), [database/import design](docs/IMPORTING.md), and [API documentation](docs/API.md).

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

The first PR established source findings and metric rules. The second added database setup and atomic imports. The third adds verified summary calculations and their first HTTP endpoint. The fourth adds matching-play inspection and pagination. Team comparison and frontend work remain upcoming. See the progress checklist in PROJECT_PLAN.md for merge status.

## Run with Docker Compose

Prerequisites: a running Docker engine (e.g. OrbStack or Docker Desktop), Docker Compose, and `curl` to download data. Host Go and exported environment variables are not required for this workflow. Run commands from the repository root.

```sh
docker compose up -d --build --wait
```

Compose starts PostgreSQL, waits for it to be healthy, runs migrations, then starts the API. The API is exposed on loopback port 8080 and PostgreSQL on loopback port 5433. Containers use an internal database URL automatically. Credentials are local development defaults. Existing data persists in the named database volume.

On a fresh setup, download and import a season explicitly:

```sh
mkdir -p data/raw
curl -fL --retry 2 -o data/raw/play_by_play_2025.csv \
  https://github.com/nflverse/nflverse-data/releases/download/pbp/play_by_play_2025.csv
docker compose run --rm importer -season 2025 -file /data/play_by_play_2025.csv \
  -source https://github.com/nflverse/nflverse-data/releases/download/pbp/play_by_play_2025.csv
curl -sS 'http://127.0.0.1:8080/api/v1/summary?season=2025&team=NE&down=3&min_distance=5&max_distance=10'
```

The importer mounts `data/raw` read-only as `/data`; downloads are not baked into images. It runs only when explicitly requested. Reimporting replaces the season, including corrections and removed plays. Only regular-season records are published. Failure preserves existing data and provenance. Success produces JSON; errors and elapsed time go to stderr. `-timeout` defaults to five minutes and includes lock wait.

Pass `-source-obtained-at` with the actual RFC3339 acquisition time if known; otherwise file modification time is used as a proxy. An unimported season returns 404 even when services are healthy. The root URL `/` has no frontend yet.

Useful commands:

```sh
docker compose ps -a
docker compose logs -f api
docker compose logs migrate
docker compose exec -T db psql -U nfl -d nfl -c \
  'SELECT season, source_rows, stored_rows, eligible_rows, source_sha256 FROM season_imports;'
docker compose stop
```

The migration container exits successfully; it is not a long-running service. Stopping retains data. Rebuild after source changes with `docker compose up -d --build --wait`. Optionally copy `.env.example` to `.env` to edit `API_PORT` / `POSTGRES_PORT`. No file or variable export is needed for default ports. Container `DATABASE_URL` is explicitly set in Compose; the example's localhost URL is for host Go development.

## Host Go development

For a faster edit/run loop, use Go 1.26+ on the host and PostgreSQL in Docker. Stop the container API first to free port 8080:

```sh
docker compose stop api
docker compose up -d --wait db
export DATABASE_URL='postgres://nfl:local_nfl@localhost:5433/nfl?sslmode=disable'
go run ./cmd/migrate
go run ./cmd/serve
```

If changing `POSTGRES_PORT`, adjust the host URL to match. Go commands do not load `.env` automatically. The equivalent host import is:

```sh
go run ./cmd/import -season 2025 -file data/raw/play_by_play_2025.csv \
  -source https://github.com/nflverse/nflverse-data/releases/download/pbp/play_by_play_2025.csv
```

Results include play count, yards/play, EPA, success rate, aDOT, CPOE, classified-attempt conversion rate, metric-specific coverage, and source provenance. See [API documentation](docs/API.md) for response semantics, configuration, and error behavior.

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
