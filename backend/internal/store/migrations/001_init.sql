-- MarkTrace MVP schema (SPEC §4).

CREATE TABLE users (
    username      text PRIMARY KEY,
    password_hash text NOT NULL,
    role          text NOT NULL CHECK (role IN ('A1', 'A2', 'A3', 'A4', 'admin')),
    display_name  text NOT NULL
);

CREATE TABLE sessions (
    token      text PRIMARY KEY,
    username   text NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);

CREATE TABLE items (
    item_code text PRIMARY KEY,
    name      text NOT NULL,
    gtin      text NOT NULL
);

CREATE TABLE code_registry (
    code            text PRIMARY KEY,
    item_code       text NOT NULL REFERENCES items (item_code),
    expected_status text NULL,
    expected_ts     timestamptz NULL
);

CREATE TABLE codes (
    code       text PRIMARY KEY REFERENCES code_registry (code),
    item_code  text NOT NULL,
    status     text NOT NULL CHECK (status IN ('issued', 'introduced', 'sold', 'returned', 'disposed')),
    ts         timestamptz NOT NULL,
    last_actor text NOT NULL
);
CREATE INDEX codes_item_idx ON codes (item_code);
CREATE INDEX codes_status_idx ON codes (status);

CREATE TABLE operations (
    op_id      text PRIMARY KEY,
    code       text NOT NULL REFERENCES code_registry (code),
    op_type    text NOT NULL,
    ts         timestamptz NOT NULL,
    actor      text NOT NULL,
    attempt_id uuid NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (code, ts)
);

CREATE TABLE import_sessions (
    session_id     uuid PRIMARY KEY,
    started_by     text NOT NULL,
    role           text NOT NULL,
    started_at     timestamptz NOT NULL DEFAULT now(),
    finished_at    timestamptz NULL,
    status         text NOT NULL CHECK (status IN ('running', 'completed', 'structure_rejected', 'failed')),
    failure_reason text NULL,
    files          jsonb NOT NULL DEFAULT '[]'::jsonb,
    counters       jsonb NULL,
    rows_created   integer NULL
);

CREATE TABLE import_rows (
    row_id            uuid PRIMARY KEY,
    session_id        uuid NOT NULL REFERENCES import_sessions (session_id),
    entity            text NOT NULL CHECK (entity IN ('items', 'codes', 'operations')),
    line_no           integer NOT NULL,
    key               text NOT NULL,
    raw               jsonb NOT NULL,
    processing_status text NOT NULL CHECK (processing_status IN ('accepted', 'duplicate', 'rejected')),
    reason            text NULL
);
CREATE INDEX import_rows_session_idx ON import_rows (session_id);
CREATE INDEX import_rows_key_idx ON import_rows (entity, key);

CREATE TABLE operation_attempts (
    attempt_id    uuid PRIMARY KEY,
    source        text NOT NULL CHECK (source IN ('form', 'import')),
    import_row_id uuid NULL REFERENCES import_rows (row_id),
    op_id         text NOT NULL,
    code          text NOT NULL,
    op_type       text NOT NULL,
    ts_raw        text NOT NULL,
    actor         text NOT NULL,
    initiator     text NOT NULL,
    role          text NOT NULL,
    decision      text NOT NULL CHECK (decision IN ('accepted', 'rejected', 'duplicate')),
    reason        text NULL,
    mock_result   jsonb NULL,
    rules_version text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX operation_attempts_op_idx ON operation_attempts (op_id);
CREATE INDEX operation_attempts_code_idx ON operation_attempts (code);

CREATE TABLE violations (
    violation_id  uuid PRIMARY KEY,
    source        text NOT NULL CHECK (source IN ('items', 'codes', 'operations')),
    ref           text NOT NULL,
    code          text NULL,
    item_code     text NULL,
    reason        text NOT NULL,
    message       text NOT NULL,
    attempt_id    uuid NULL,
    import_row_id uuid NULL,
    rules_version text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT clock_timestamp(),
    op_ts         timestamptz NULL,
    actor         text NULL
);
CREATE INDEX violations_code_idx ON violations (code);
CREATE INDEX violations_reason_idx ON violations (reason);
CREATE INDEX violations_created_idx ON violations (created_at);

CREATE TABLE violation_resolutions (
    id           uuid PRIMARY KEY,
    violation_id uuid NOT NULL REFERENCES violations (violation_id),
    action       text NOT NULL CHECK (action IN ('comment', 'close')),
    comment      text NOT NULL CHECK (comment <> ''),
    author       text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX violation_resolutions_vid_idx ON violation_resolutions (violation_id);
-- At most one close per violation.
CREATE UNIQUE INDEX violation_resolutions_one_close ON violation_resolutions (violation_id) WHERE action = 'close';
