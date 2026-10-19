# NFL Situational Play Explorer

Status: planning; implementation has not started.

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
| Database access | Explicit SQL through `database/sql` with a PostgreSQL driver | Keep queries visible and explainable; driver selection remains open |
| Schema changes | Versioned SQL migrations | Reproducible database setup and evolution; migration tool remains open |
| Frontend | React, TypeScript, Vite | A focused interactive client for the Go API |
| Local database | Docker Compose | Reproducible local PostgreSQL setup |
| CI | GitHub Actions | Run meaningful tests and build checks |

Use one repository, one database, and one API process. Provide separate `import` and `serve` commands, sharing code where appropriate. The exact command syntax and package layout remain implementation decisions.

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

Before implementing the importer, inspect a real season file, confirm field semantics and identifiers, and review the source's license and attribution requirements. Preserve source attribution in documentation and the interface. Do not imply that upstream data or models were created by this project.

## Data contract and metric rules

This is the next design task. Finalize these rules before implementing aggregates.

### Record identity and retained fields

- Confirm a stable play identifier from the source, likely a combination of game and play identifiers; verify uniqueness before adopting it.
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

The exact inclusion rules remain open. Each metric must have a named numerator, denominator, and exclusion policy. Use source definitions where appropriate and test those choices explicitly. Do not assume every metric uses the same denominator.

### Initial metrics

| Metric | Required definition |
| --- | --- |
| Play count | Count of records qualifying under the documented population and filters |
| Yards per play | Yardage sum divided by the explicitly defined eligible count; decide missing-yardage behavior |
| Third/fourth-down conversion rate | Successful conversions divided by eligible attempts; settle penalty and touchdown handling |
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
- Decide how records removed from a corrected source file are handled; simple upserts alone may be insufficient.
- Publish a season update atomically so failed imports preserve the previous usable dataset.
- Avoid exposing partially imported data to API requests.
- Record provenance and import results, including season, source reference, time, row counts, and status. Decide whether a source checksum is useful.
- Define behavior for concurrent imports of the same season.

The choice between staging and transactional season replacement remains open. Select the simplest approach that meets these guarantees.

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

Conceptual API capabilities: list available seasons/teams, retrieve situational summaries, and retrieve matching plays. Exact routes and response shapes will be designed after the data contract. Avoid building a general-purpose query language.

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

- Initial season and eventual first-release season coverage.
- Verified play identity and team-code normalization.
- Exact metric eligibility and missing-value policies.
- Database schema and atomic reimport strategy.
- PostgreSQL driver and migration tooling.
- API routes, response contracts, pagination strategy, and timeout values.
- Hosting and deployment budget.

Resolve these incrementally, beginning with source inspection and metric definitions. Avoid choosing infrastructure merely to expand the technology list.
