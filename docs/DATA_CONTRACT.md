# Proposed data contract — version 1

Status: proposed for review in the first PR. These rules describe application behavior, not official league statistics. The inspection command profiles the proposed population; import, SQL aggregates, and API behavior will be implemented in later PRs.

## Coverage and identity

- Begin with the 2025 regular season (`season_type = REG`). The downloaded file also includes postseason records; never mix them silently. Retain season type in the eventual database so later support is explicit.
- Use `(game_id, play_id)` as the proposed unique key. It is unique in the inspected snapshot. The upstream dictionary mentions drive in its identity description, so validate this key on every import and revisit it if a future source conflicts; do not silently deduplicate.
- Preserve upstream team codes as canonical values for this release. For example, the Rams use `LA` and Jacksonville uses `JAX`. Display labels may differ; historical franchise normalization is deferred until adding seasons that require it.
- Season is the football season, not the calendar year of each game.

## Base offensive-play population

A record qualifies only when all of these hold:

1. Regular season, offensive team present, and down is 1–4.
2. Source `play_type` is `run` or `pass`.
3. `qb_kneel`, `qb_spike`, `two_point_attempt`, `play_deleted`, and `aborted_play` are all explicitly zero.

This includes sacks and scrambles. It excludes special teams, administrative/no-play records, kneels, spikes, two-point attempts, deleted records, and aborted plays. Penalties on otherwise qualifying run/pass records remain included; `no_play` penalties do not. This is a reproducible analytical choice and must be visible in metric help text. Results may differ from league box scores or another site's population.

The importer must reject invalid required classification flags rather than quietly applying the policy to malformed data. Null down/offense may be legitimate on excluded records. Down and distance are pre-play values; a distance filter uses inclusive `ydstogo` bounds. Null distance cannot match a distance range and is reported in import validation/coverage.

## Metrics

All metrics start from the filtered base population but use their own valid observations. Return numerator or sum, valid count, and missing/unclassified count as appropriate. Means and percentages are null when their count is zero. Preserve valid zeros and negatives. Compare teams using identical filters and policies.

| Metric | Calculation | Coverage policy |
| --- | --- | --- |
| Play count | Number of filtered base plays | Includes plays missing an individual metric |
| Yards/play | Sum of non-null `yards_gained` / yardage count | Null yardage excluded from this metric only |
| EPA/play | Sum of non-null source `epa` / EPA count | Attribute the upstream model; do not substitute `qb_epa` |
| Total EPA | Sum over the same EPA observations | Null if no observations; depends on volume |
| Success rate | Count of EPA observations strictly greater than zero / EPA count | EPA of zero is not a success; missing EPA is not a failure |
| aDOT | Sum of non-null `air_yards` / air-yard count on the targeted-attempt subset | Includes negative and zero depth, incompletions, and intercepted targets |
| CPOE | Mean of non-null source `cpoe` on the targeted-attempt subset | Display in percentage points; do not multiply upstream values by 100 |
| Third-/fourth-down conversion | Converted / (converted + failed), for the chosen down | Use the corresponding source conversion flags; show unclassified count separately |

Success rate and CPOE extend the initial metric set discussed in the project plan. Their SQL calculations and hand-checked fixtures are part of the later metric implementation, not completed by this inspection PR.

### Targeted-attempt subset

Require a base play with `pass_attempt = 1`, `sack = 0`, and non-null `receiver_player_id`. The base policy already excludes spikes; scramble records do not qualify as targeted pass attempts. Do not identify a target merely from non-null air yards: the snapshot contains air-yard values without an identified receiver.

This operational definition excludes throws without an identified target, including throwaways, but can also exclude genuinely targeted passes with missing receiver attribution. Expose it as “identified-target attempts,” document that limitation, and do not claim exhaustive throwaway classification. A targeted attempt can have null air yards or CPOE; exclude it only from the affected metric and count that missing observation. Do not infer missing model values or restrict the subset to completed passes.

The observed CPOE scale is percentage points. Validate a sample against `(complete_pass - cp) * 100` before implementing CPOE aggregates; dictionary wording alone is insufficient to determine units.

### Conversions and penalties

For third down, use `third_down_converted` and `third_down_failed`; fourth down uses the analogous fields. Exactly one flag set to one is classified. Both zero, nulls, or conflicting flags are unclassified for this metric and must be exposed. Do not infer conversions from yards gained, since penalties and goal-to-go plays complicate that shortcut.

The snapshot has eight eligible third-down records with neither flag set. Excluding these from the conversion denominator must be explicit. The metric label should say “classified attempt conversion rate,” with counts available. Third- and fourth-down rates are separate; do not blend them. If the selected down is 1 or 2, conversion rate is unavailable.

### Interpretation

EPA and CPOE use upstream model outputs, not models trained by this project. They are descriptive, model-dependent, and not opponent-adjusted or causal estimates of individual ability. This release summarizes offenses, not individual quarterback rankings. Averages across multiple groups must be computed from underlying sums/counts, not unweighted averages of averages.

## Proposed retained fields

Finalize column types during the schema PR. Retain these source fields or explicit equivalents:

- Identity/provenance: `game_id`, `play_id`, `season`, `season_type`, and a reference to the successful import.
- Context: `week`, `game_date`, `home_team`, `away_team`, `posteam`, `defteam`, `drive`, `qtr`, `time`, `down`, `ydstogo`, `yardline_100`, `score_differential`, `game_seconds_remaining`, `desc`.
- Eligibility: `play_type`, `qb_kneel`, `qb_spike`, `qb_scramble`, `qb_dropback`, `two_point_attempt`, `play_deleted`, `aborted_play`, `penalty`, `pass_attempt`, `sack`, `receiver_player_id`.
- Measurements/outcomes: `yards_gained`, `epa`, `air_yards`, `cp`, `cpoe`, `complete_pass`, `interception`, `touchdown`, `first_down`, and third-/fourth-down converted/failed flags.

Store numeric missing values as SQL NULL, not zero. Preserve sufficient precision for upstream model outputs; choose database types explicitly in the schema review. Round only for presentation. Retaining field position, score, and clock supports context now and later filters without another source change.

## Import behavior for the next PR

- Validate required headers, scalar types, flags, season consistency, unique keys, and team references before publication. Additional upstream columns may be ignored; missing required columns fail clearly.
- Record source URL, download/import times, SHA-256, row counts, eligible counts, and contract version. Preserve the prior successful import's provenance when a replacement fails.
- Replace an imported season atomically, including removal of records deleted from the new source. Do not implement insert-only deduplication.
- Serialize imports for the same season. Concurrent requests should see a committed dataset; comparisons and summaries must use a consistent snapshot if they require multiple SQL statements.
- Separate import-wide failures from legitimate metric nulls. Report coverage rather than imputing values.
- Keep raw downloads outside Git and keep the public application usable without upstream network requests.

## Review questions before schema work

1. Is a regular-season-only initial dataset appropriate?
2. Are the exclusion policy and identified-target definition useful for the intended analysis?
3. Is a classified-attempt conversion rate with explicit unclassified counts preferable to a separately researched rule for penalty outcomes?

These are proposed defaults, not blockers to reviewing this PR. Resolve substantive changes before building dependent import and metric code.

## Source references

- [Published CSV release](https://github.com/nflverse/nflverse-data/releases/tag/pbp).
- [Machine-readable field dictionary](https://github.com/nflverse/nflreadr/blob/main/data-raw/dictionary_pbp.csv).
- [nflfastR documentation](https://www.nflfastr.com/articles/nflfastR.html).
- [QB EPA distinction](https://www.nflfastr.com/reference/add_qb_epa.html).

See [source inspection](SOURCE_INSPECTION.md) for the exact file hash and observed evidence supporting these defaults.
