WITH filtered AS (
    SELECT yards_gained, epa, air_yards, cpoe,
           pass_attempt IS TRUE AND sack IS FALSE AND receiver_player_id IS NOT NULL AS is_target,
           CASE $3::integer
               WHEN 3 THEN third_down_converted IS TRUE AND third_down_failed IS FALSE
               WHEN 4 THEN fourth_down_converted IS TRUE AND fourth_down_failed IS FALSE
               ELSE FALSE
           END AS converted,
           CASE $3::integer
               WHEN 3 THEN third_down_failed IS TRUE AND third_down_converted IS FALSE
               WHEN 4 THEN fourth_down_failed IS TRUE AND fourth_down_converted IS FALSE
               ELSE FALSE
           END AS failed
    FROM plays
    WHERE season = $1 AND posteam = $2 AND down = $3 AND is_eligible
      AND ydstogo BETWEEN $4 AND $5
), stats AS (
    SELECT count(*) AS play_count,
           count(yards_gained) AS yard_count, sum(yards_gained)::double precision AS yard_sum,
           count(epa) AS epa_count, sum(epa) AS epa_sum,
           count(*) FILTER (WHERE epa > 0) AS successes,
           count(*) FILTER (WHERE is_target) AS targeted_attempts,
           count(air_yards) FILTER (WHERE is_target) AS air_count,
           sum(air_yards) FILTER (WHERE is_target) AS air_sum,
           count(cpoe) FILTER (WHERE is_target) AS cpoe_count,
           sum(cpoe) FILTER (WHERE is_target) AS cpoe_sum,
           count(*) FILTER (WHERE converted) AS converted_count,
           count(*) FILTER (WHERE failed) AS failed_count
    FROM filtered
)
SELECT publication.published_at, publication.source_sha256, publication.contract_version,
       EXISTS (SELECT 1 FROM teams WHERE code = $2) AS known_team,
       stats.*
FROM stats
LEFT JOIN season_imports AS publication ON publication.season = $1;
