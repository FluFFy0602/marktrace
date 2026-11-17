-- Batch issue of new codes from the «Выпуск кодов» screen.
-- batch_id is the client's idempotency key: repeating the request returns the same batch.

CREATE TABLE issue_batches (
    batch_id   uuid PRIMARY KEY,
    item_code  text NOT NULL REFERENCES items (item_code),
    quantity   integer NOT NULL CHECK (quantity > 0),
    ts         timestamptz NOT NULL,
    actor      text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE issue_batch_codes (
    batch_id uuid NOT NULL REFERENCES issue_batches (batch_id),
    pos      integer NOT NULL,
    code     text NOT NULL UNIQUE REFERENCES code_registry (code),
    PRIMARY KEY (batch_id, pos)
);
