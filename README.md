# NFL Situational Play Explorer

A Go/PostgreSQL project for exploring how NFL offenses perform in selected game situations. The backend is the primary focus; a small React interface is planned.

**Current state:** source inspection and proposed data contract. There is no database importer, API, or frontend yet. See [the project plan](PROJECT_PLAN.md), [data contract](docs/DATA_CONTRACT.md), and [source findings](docs/SOURCE_INSPECTION.md).

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

The first PR covers source findings, a reproducible Go inspection command, and proposed metric rules. Database/import work follows review of those rules.

## Data attribution

Play-by-play data and published model outputs come from [nflverse](https://github.com/nflverse/nflverse-data) and [nflfastR](https://www.nflfastr.com/). The upstream data repository identifies its license as [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/). This project filters and aggregates those records; it does not create the upstream expected-points or completion-probability models. See the inspection notes for the exact source and snapshot hash. No raw season data is redistributed in this repository.
