# 2025 source inspection

Inspected on October 19, 2026. These counts describe a downloaded snapshot, not an immutable upstream release or official league totals.

## Reproduction

Follow the commands in [README.md](../README.md). The checked-in [JSON profile](source-profile-2025.json) was produced by `go run ./cmd/inspect -file data/raw/play_by_play_2025.csv` using the policy in [DATA_CONTRACT.md](DATA_CONTRACT.md). A changed upstream file can legitimately produce different counts; compare hashes first.

- Source: https://github.com/nflverse/nflverse-data/releases/download/pbp/play_by_play_2025.csv
- SHA-256: `8ce0001826f0f7b895b7a1068e4db7f43696c699db7064ccb1201855768fd06c`
- 48,771 records, 372 columns, 285 games.
- All rows identify season 2025.
- 46,452 regular-season records and 2,319 postseason records.
- No duplicate `(game_id, play_id)` pairs in this snapshot.

## Findings that affect the design

### Record types differ from qualifying offensive plays

The file contains kicks, administrative records, no-play penalties, kneels, spikes, and missing play types as well as run/pass records. A row count of the raw file is not an offensive snap count.

The proposed regular-season population contains **32,726 plays across 272 games and 32 teams**. It excludes aborted plays as well as kneels, spikes, two-point attempts, deleted rows, and non-run/pass records. Qualifying penalty records remain included. This is our analytical policy, not a claim that it matches every other statistics provider.

### Metric coverage needs separate denominators

| Observation under the proposed population | Count |
| --- | ---: |
| Plays with yardage | 32,726 |
| Plays with EPA | 32,726 |
| Identified-target attempts | 16,609 |
| Identified-target attempts with air yards | 16,609 |
| Identified-target attempts with CPOE | 16,608 |

Many offensive plays legitimately lack air yards or CPOE. These missing values must not become zeros. Some records have air-yard values without a receiver ID, so non-null air yards alone does not establish an identified target. Receiver attribution can itself be incomplete; the metric definition must disclose that limitation.

### Conversion flags leave some attempts unclassified

| Down | Converted | Failed | Neither classified |
| --- | ---: | ---: | ---: |
| Third | 2,709 | 4,066 | 8 |
| Fourth | 488 | 385 | 0 |

The eight third-down records have penalty context. Examples include `(2025_03_GB_CLE, 1550)` and `(2025_03_LA_PHI, 1293)`. Do not turn both-zero flags into a failed conversion. The proposed contract reports these separately and uses classified attempts for the conversion denominator.

### CPOE units were checked against the data

An independent exploratory calculation checked all 17,490 records in the full file with populated `cpoe`, `cp`, and `complete_pass`. The maximum absolute difference between source CPOE and `(complete_pass - cp) * 100` was about `1.07e-13`. Use percentage points directly; do not multiply the stored CPOE by 100 again. This check was exploratory and is not currently part of the inspection command.

### Identity and team codes need explicit policies

The observed game/play pair is unique, but future imports must enforce uniqueness rather than assume it. The dictionary's identity description also mentions drive; retain that field and revisit conflicting keys instead of silently dropping records.

The snapshot uses `LA` for the Rams and `JAX` for Jacksonville. Preserve source codes for now and separate presentation labels from canonical identifiers.

## Attribution

The [upstream repository metadata](https://api.github.com/repos/nflverse/nflverse-data) identifies CC BY 4.0. Attribute nflverse and nflfastR, link to the source and license, and identify our filtering/aggregation changes. The upstream expected-points and completion-probability models are not authored by this project. No raw season file is committed here.

## Limits and next work

- One season is inspected; this does not establish historical compatibility.
- The profiler validates required headers, identity presence, row shape, and finite measured values. It reports duplicate keys but does not act as a production import validator.
- Metric formulas are proposed and documented; the profiler does not implement aggregate metric calculations.
- Decide the review questions in the contract before implementing migrations, atomic imports, and SQL metric fixtures.
