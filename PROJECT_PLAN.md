# NFL Situational Play Explorer

Status: source inspection, the data contract, PostgreSQL migrations/imports, verified summary metrics, the first HTTP endpoint, and the Docker development workflow are merged. Matching-play pagination is implemented and awaiting PR review. See [API documentation](docs/API.md) for implemented behavior.

## Progress

Update this checklist in each implementation PR. Mark milestones complete after merge; describe unmerged work as implemented and awaiting review.

- [x] Source inspection, Go profiler, and initial data contract — [PR #1](https://github.com/caleflat/nfl/pull/1).
- [x] PostgreSQL schema, migrations, atomic season replacement, and import tests — [PR #2](https://github.com/caleflat/nfl/pull/2).
- [x] Verified summary metrics, HTTP validation/timeouts, and Docker startup — [PR #3](https://github.com/caleflat/nfl/pull/3).
- [ ] Paginated matching plays with shared filters, metric/context fields, and reimport detection — implemented, awaiting review.
- [ ] Team comparison under identical filters and a consistent dataset snapshot.
- [ ] Available-season/team discovery for interface controls.
- [ ] React interface with filters, summary, matching plays, comparisons, and shareable URLs.
- [ ] Verify additional completed seasons and document compatibility.
- [ ] Measure representative queries and document justified optimizations.
- [ ] Portfolio packaging: screenshots, deployment decision, and evidence-based resume bullets.

Current local coverage is the 2025 regular season. Go CI includes PostgreSQL integration tests and a fresh-database Docker startup check. Frontend checks and browser verification will be added with frontend work.

The sections below describe release requirements and rationale; the checklist above is the current completion status.

## Purpose

Build a useful NFL play-by-play explorer that demonstrates backend engineering: repeatable data ingestion, relational data modeling, correct calculations, API design, failure handling, and measured query performance.

The primary audience for the project portfolio is backend employers, while a small, polished frontend also demonstrates full-stack ability. The project should remain small enough to finish and explain confidently in an interview. Resume claims must describe implemented, verified behavior; performance claims must have measurements.

## Core user workflow

Select a completed season, offensive team, down, and yards-to-go range. See how that offense performed, inspect the underlying plays, and optionally compare another offense using identical filters.

Example: “How did New England perform on third down with 5–10 yards to go, compared with Buffalo?”

The interface will show sample sizes alongside results and explain which records each statistic includes. Matching plays make the results inspectable rather than presenting unexplained aggregates.

## Stack

| Component | Decision | Rationale |
| --- | --- | --- |
| API and importer | Go | Practice backend development in Go; share types and database logic |
| HTTP server | Standard library `net/http` | Sufficient for the planned JSON API |
| Database | PostgreSQL | Fits filtered queries, aggregates, constraints, and transactions |
| Database access | Explicit SQL through `database/sql` and pgx's compatibility driver | Keep queries visible and explainable |
| Schema changes | Embedded, numbered SQL migrations with checksums and a transactional runner | Reproducible database setup and evolution |
| Frontend | React, TypeScript, Vite | A focused interactive client for the Go API |
| Local services | Docker Compose | Start PostgreSQL, apply migrations, and run the API together; run imports explicitly |
| CI | GitHub Actions | Run meaningful tests and build checks |

Use one repository, one database, and one API process. Provide separate `import` and `serve` commands, sharing code where appropriate. Implemented commands are `inspect`, `migrate`, `import`, and `serve`; the README documents their host and Docker workflows.

```text
nflverse season files -> Go import command -> PostgreSQL
                                                  ^
                                           Go API <- React UI
```

Imports run independently of user requests. A search must never download source datasets.

## First complete release

### Data coverage

- Start development with one completed season.
- After the import and query workflow is verified, support a small set of completed seasons.
- Display available seasons and imported-data freshness/provenance.
- Keep imports manually triggered for this release.

### Filters

- Season.
- Offensive team.
- Down.
- Inclusive minimum and maximum yards to go.
- Optional comparison team, evaluated with the same season and situation filters.

### Results

- Number of qualifying plays.
- Average yards gained per qualifying play.
- Average depth of target (aDOT), with the number of qualifying throws.
- EPA per play and total EPA, with the number of plays containing usable EPA.
- EPA-based success rate and source CPOE, with metric-specific counts and coverage rules defined in the data contract.
- Conversion rate for third- or fourth-down selections, with the qualifying denominator shown.
- Paginated matching plays with descriptions, teams, and game context.
- Side-by-side team results under identical filters.
- Shareable URLs containing filter state.
- Clear empty, loading, and error states.
- Plain-language metric definitions and exclusions.

Conversion rate should be unavailable when its definition does not apply, rather than displaying a misleading zero. Empty samples and missing metric values also need explicit representations.

## Source data

Use published nflverse play-by-play season files directly. CSV allows ingestion in Go without Python or R dependencies.

- Dataset access: https://nflreadr.nflverse.com/reference/load_pbp.html
- Field dictionary: https://nflreadr.nflverse.com/articles/dictionary_pbp.html
- Published files: https://github.com/nflverse/nflverse-data/releases/tag/pbp

The initial season and fields were inspected in PR #1; findings, hash, and attribution are recorded in `docs/SOURCE_INSPECTION.md`. Preserve source attribution in documentation and the interface. Do not imply that upstream data or models were created by this project.

## Data contract and metric rules

Version 1 rules are defined in [the data contract](docs/DATA_CONTRACT.md) and exercised by PostgreSQL summary fixtures. Future changes require explicit documentation and compatibility review.

### Record identity and retained fields

- Use `(game_id, play_id)` as the validated initial key; reject duplicate identities on every import.
- Retain fields needed for identity, season, game context, offense/defense, down, distance, yards gained, descriptions, and metric eligibility.
- Retain enough source flags to explain exclusions and distinguish relevant play outcomes.
- Separate legitimate missing values from zero.
- Establish team-code handling across seasons.
- Do not copy every source column into the application schema without a reason.

### Eligible play population

Define and document how the application handles:

- Penalties and records marked as no play.
- Sacks and quarterback scrambles.
- Kneels and spikes.
- Punts, field goals, kickoffs, and conversion attempts.
- Missing offense, down, distance, or yardage.
- Touchdowns, first downs, turnovers, and accepted penalties affecting conversions.

Version 1 includes qualifying run/pass records, including sacks, scrambles, and qualifying penalty records; it excludes no-play records, kneels, spikes, special teams, two-point attempts, deleted records, and aborted plays. The data contract defines metric-specific denominators, identified targets, null handling, and classified conversions.

### Initial metrics

| Metric | Required definition |
| --- | --- |
| Play count | Count of records qualifying under the documented population and filters |
| Yards per play | Sum of non-null yardage divided by its valid count; show missing count |
| Third/fourth-down conversion rate | Converted divided by converted plus failed, using source flags; show unclassified count separately |
| Average depth of target (aDOT) | Sum of source `air_yards` divided by qualifying targeted pass attempts with non-null air yards; includes incomplete passes and interceptions with recorded air yards, and preserves negative depths |
| EPA per play | Mean source `epa` across eligible offensive plays with non-null EPA; publish the eligible count |
| Total EPA | Sum source `epa` over that same EPA population; label as volume-dependent |

### Advanced statistics

aDOT and EPA are part of the first complete release. Add them after the basic query slice is verified, using the same situation filters and team-comparison workflow.

- **aDOT:** Measures target depth relative to the line of scrimmage, rather than completed-pass distance or yards gained after the catch. Exclude sacks, scrambles, spikes, and throwaways from the targeted-attempt population. Confirm how source flags identify these cases before finalizing SQL. Include valid incomplete/intercepted targets; never restrict this metric to completions. Null air yards are excluded and reported as missing coverage, while zero and negative air yards remain valid.
- **EPA:** Expected points added measures how a play changes expected scoring value. Import nflverse/nflfastR's published `epa` values and aggregate them; building or training an expected-points model is outside this project's scope. Attribute the upstream model and preserve provenance. Do not substitute `qb_epa`, `air_epa`, or another EPA variant without an explicitly different metric definition.
- **EPA eligibility:** Start from the documented offensive-play population. Resolve penalty, no-play, kneel, spike, sack, scramble, and turnover handling explicitly. An eligible sack or scramble can have EPA even though it cannot contribute to aDOT. EPA is descriptive and model-dependent; do not present it as an opponent-adjusted ranking or causal measure of individual ability.
- **Coverage and comparisons:** Return each metric's denominator and missing-value count. Show unavailable values for empty metric populations, not zero. Aggregate underlying sums/counts when combining samples rather than averaging group averages. Both teams use identical eligibility rules.
- **Storage and API:** Retain nullable `air_yards` and `epa`, plus the source flags needed to reproduce eligibility. Include these values in matching-play details when available. Expose advanced metrics and their counts in summary responses; preserve precision in storage/calculation and round only for display.
- **Tests:** Cover negative/zero/null air yards; completed, incomplete, and intercepted targets; excluded throws; sacks and scrambles; positive/negative/zero/null EPA; empty samples; unequal group sizes; and comparison consistency. Verify expected aggregates with hand-calculated fixtures.

Source references: [nflfastR overview](https://nflfastr.com/articles/nflfastR.html), [statistics variable definitions](https://nflfastr.com/reference/nfl_stats_variables.html), and [QB EPA distinction](https://nflfastr.com/reference/add_qb_epa.html). Verify exact source-field semantics against the real input file and dictionary during data-contract work.

Pass/run frequency is deferred until sacks and scrambles have a deliberate classification policy.

## Import requirements

- Download and parse a selected completed season.
- Validate expected headers, identifiers, required values, and duplicate records.
- Handle legitimate nulls explicitly; never silently coerce them to zero.
- Report malformed data and failure context clearly.
- Reimporting a season must not create duplicates and must apply source corrections.
- Remove records absent from a corrected source file during transactional season replacement.
- Publish a season update atomically so failed imports preserve the previous usable dataset.
- Avoid exposing partially imported data to API requests.
- Record current successful publication provenance, including season, source reference, acquisition/publication time, row counts, contract version, and SHA-256. Report failed attempts to stderr.
- Serialize concurrent imports of the same season with a transaction-scoped advisory lock.

Implemented: a single replacement transaction with a season-specific advisory lock. Removed source records are removed on replacement; failure preserves the previous data and metadata. SHA-256 and current successful publication metadata are stored. Downloads remain manual; failures currently go to stderr rather than a durable history table. See [import design](docs/IMPORTING.md).

## Database and API requirements

- Use database constraints to enforce record identity and appropriate invariants.
- Apply schema changes through committed migrations.
- Use parameterized SQL.
- Validate team, season, down, distance bounds, and pagination parameters.
- Reject invalid ranges and unsupported values with useful client errors.
- Cap page sizes and use deterministic ordering for matching plays.
- Propagate request cancellation and enforce suitable database/request timeouts.
- Return consistent JSON errors without exposing database internals.
- Handle empty datasets and unavailable seasons explicitly.
- Use connection pooling with documented configuration.
- Keep comparison filters identical except for offensive team.
- Use the same documented eligibility logic for aggregates and matching plays, exposing any metric-specific differences.

Implemented: `GET /api/v1/summary` and `GET /healthz`. Current work adds `GET /api/v1/plays`. Season/team discovery and team comparison remain planned. See API documentation for response shapes and timeout values. Avoid building a general-purpose query language.

## Diagnostics and configuration

- Log actionable import errors and outcomes.
- Log request duration, response status, and useful error context.
- Configure database connection, server address, and relevant timeouts through environment/configuration.
- Commit an example configuration without secrets.
- Provide a basic health endpoint; decide whether database readiness needs a separate check.
- Document startup, migration, import, test, and build commands.

## Verification

Focus tests on behavior that could corrupt data or mislead users.

- Small fixtures with known answers for counts, yardage, conversions, nulls, and play exclusions.
- Import integration tests against real PostgreSQL for duplicate prevention, corrected data, rollback on failure, and season isolation.
- Query/API integration tests for filters, boundary values, comparison consistency, pagination, empty results, and invalid inputs.
- Confirm cancellation behavior where practical.
- CI runs Go tests, relevant integration tests, frontend type/build checks, and backend builds.
- Manually verify the core browser workflow and shareable filter URLs.

Keep fixtures small and reproducible. Automated checks should not depend on downloading the full upstream dataset on every run.

## Performance work

- Load the actual supported dataset before making optimization claims.
- Choose representative broad and selective queries.
- Record dataset size, hardware/environment, query parameters, and measurement method.
- Inspect PostgreSQL query plans and measure timings before choosing indexes.
- Compare results after changes, including whether an index actually helped.
- Document limitations such as warm versus cold caches and local versus deployed timings.

Do not promise a speedup, add an index to every filter, or introduce caching before measurements justify it.

Reference: https://www.postgresql.org/docs/current/indexes-multicolumn.html

## Milestones and acceptance criteria

1. **Finalize the data contract.** Inspect source fields and settle identifiers, eligibility, and metric definitions with small examples.
2. **Build the import foundation.** Migrations and one-season import work; repeated and failed imports satisfy the documented guarantees.
3. **Build one vertical slice.** A validated API query returns verified counts and yards per play for a selected situation.
4. **Complete the explorer.** Add conversion rate, aDOT, EPA per play and total EPA with metric-specific counts, matching-play pagination, team comparison, and the React interface with shareable URLs.
5. **Verify and measure.** Complete meaningful tests, CI, a few seasons of data, and documented query measurements.
6. **Package for review.** Provide a clear README, architecture and metric notes, reproducible setup, screenshots, and a public demo if practical.

Choose hosting after the local application works. Hosting provider, budget, deployment topology, and operational expectations remain open. A public demo is desirable for the portfolio, but no provider or paid service is committed.

## Later additions

These are candidates after the first release, not promises:

- Field-position filters.
- Score-differential filters.
- Time-remaining filters.
- Pass/run frequency with documented sack/scramble handling.
- More historical seasons.

## Deferred infrastructure and features

Reconsider only when a demonstrated use case warrants the additional work:

- Replay video playback, embedded clips, and paid video-provider integrations. No replay subscription is needed for the planned application. Keep game identifiers, quarter, clock, and play descriptions for context; direct links to external film can be investigated later if supported.
- Accounts, authentication, and saved searches; shareable URLs cover the initial need.
- Caching.
- Scheduled refresh and background job infrastructure.
- Live scores or live play ingestion.
- Predictions, machine learning, and fourth-down recommendations.
- Player rankings and fantasy features.
- Microservices, message brokers, and orchestration platforms.

## Portfolio deliverables

- Working source code with reproducible setup and a useful README.
- Documented architecture, data provenance, metric definitions, and tradeoffs.
- Evidence for import reliability and calculation correctness.
- Reproducible query measurements and explanations of any optimization.
- A small, usable frontend demonstrating the backend.
- Resume bullets written after completion, reflecting actual scope and measured results without invented scale, users, or impact.

## Open decisions

- Additional first-release seasons beyond 2025 and any historical team-code compatibility changes.
- Comparison and discovery endpoint contracts.
- Frontend layout and presentation choices.
- Query-specific indexes or other optimizations after measurement.
- Hosting and deployment budget.

Resolve these incrementally after the matching-play workflow. Avoid choosing infrastructure merely to expand the technology list.
