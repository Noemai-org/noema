-- amount_minor NUMERIC in a comment is not a column
CREATE TABLE transfer_line (
    id text PRIMARY KEY,
    amount_minor BIGINT NOT NULL CHECK (amount_minor BETWEEN 0 AND 9007199254740991),
    attempt_ratio numeric,
    elapsed real
);
SELECT (r.json->>'attempt')::numeric FROM r;
