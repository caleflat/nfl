CREATE TABLE teams (
    code text PRIMARY KEY
);
INSERT INTO teams(code) VALUES
    ('ARI'),('ATL'),('BAL'),('BUF'),('CAR'),('CHI'),('CIN'),('CLE'),
    ('DAL'),('DEN'),('DET'),('GB'),('HOU'),('IND'),('JAX'),('KC'),
    ('LA'),('LAC'),('LV'),('MIA'),('MIN'),('NE'),('NO'),('NYG'),
    ('NYJ'),('PHI'),('PIT'),('SEA'),('SF'),('TB'),('TEN'),('WAS');

-- One current publication per regular season; replacing it and its plays is
-- transactional. Prior successful metadata survives any failed replacement.
CREATE TABLE season_imports (
    season integer PRIMARY KEY CHECK (season BETWEEN 1999 AND 2100),
    source text NOT NULL CHECK (length(source) > 0),
    source_sha256 text NOT NULL CHECK (source_sha256 ~ '^[0-9a-f]{64}$'),
    source_obtained_at timestamptz NOT NULL,
    published_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    contract_version integer NOT NULL CHECK (contract_version > 0),
    source_rows integer NOT NULL CHECK (source_rows > 0),
    stored_rows integer NOT NULL CHECK (stored_rows > 0),
    eligible_rows integer NOT NULL CHECK (eligible_rows >= 0),
    missing_distance_rows integer NOT NULL CHECK (missing_distance_rows >= 0),
    CHECK (eligible_rows <= stored_rows AND stored_rows <= source_rows)
);

CREATE TABLE plays (
    game_id text NOT NULL,
    play_id integer NOT NULL CHECK (play_id >= 0),
    season integer NOT NULL REFERENCES season_imports(season) ON DELETE CASCADE,
    season_type text NOT NULL CHECK (season_type = 'REG'),
    week integer NOT NULL CHECK (week BETWEEN 1 AND 22),
    game_date date NOT NULL,
    home_team text NOT NULL REFERENCES teams(code),
    away_team text NOT NULL REFERENCES teams(code),
    posteam text REFERENCES teams(code),
    defteam text REFERENCES teams(code),
    drive integer,
    qtr integer CHECK (qtr BETWEEN 1 AND 10),
    time text,
    down integer CHECK (down BETWEEN 1 AND 4),
    ydstogo integer CHECK (ydstogo >= 0),
    yardline_100 integer CHECK (yardline_100 BETWEEN 0 AND 100),
    score_differential integer,
    game_seconds_remaining integer CHECK (game_seconds_remaining >= 0),
    description text,
    play_type text,
    qb_kneel boolean,
    qb_spike boolean,
    qb_scramble boolean,
    qb_dropback boolean,
    two_point_attempt boolean,
    play_deleted boolean,
    aborted_play boolean,
    penalty boolean,
    pass_attempt boolean,
    sack boolean,
    receiver_player_id text,
    yards_gained integer,
    epa double precision,
    air_yards double precision,
    cp double precision CHECK (cp BETWEEN 0 AND 1),
    cpoe double precision CHECK (cpoe BETWEEN -100 AND 100),
    complete_pass boolean,
    interception boolean,
    touchdown boolean,
    first_down boolean,
    third_down_converted boolean,
    third_down_failed boolean,
    fourth_down_converted boolean,
    fourth_down_failed boolean,
    is_eligible boolean NOT NULL,
    PRIMARY KEY (game_id, play_id),
    CHECK (home_team <> away_team),
    CHECK (posteam IS NULL OR posteam IN (home_team, away_team)),
    CHECK (defteam IS NULL OR defteam IN (home_team, away_team))
);

-- Supports season replacement and its FK checks. Query-specific indexes will
-- follow actual workload measurements in a later milestone.
CREATE INDEX plays_season_idx ON plays(season);
