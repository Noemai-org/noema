CREATE TABLE transfer_line (
    id text PRIMARY KEY,
    amount_minor NUMERIC NOT NULL,
    fee_minor double precision NOT NULL,
    balance money
);
