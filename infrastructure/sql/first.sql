CREATE TYPE accountType as ENUM ('region', 'nation');

CREATE TABLE IF NOT EXISTS accounts (
    account_name TEXT UNIQUE NOT NULL PRIMARY KEY ON DELETE CASCADE,
    account_pass_hash TEXT,
    account_type accountType NOT NULL DEFAULT 'nation',
    cash_in_hand NUMERIC NOT NULL DEFAULT 0.0 CHECK(cash_in_hand >= 0.0),
    cash_in_escrow NUMERIC NOT NULL DEFAULT 0.0 CHECK(cash_in_escrow >= 0.0)
);

CREATE TYPE perm as ENUM ('admin', 'trader', 'citizen');

CREATE TABLE IF NOT EXISTS nation_permissions (
    region_name TEXT NOT NULL REFERENCES accounts(account_name),
    nation_name TEXT NOT NULL REFERENCES accounts(account_name),
    permission perm NOT NULL,
    PRIMARY KEY(region_name, nation_name),
    CONSTRAINT seperateThings CHECK(region_name != nation_name)
);

CREATE TABLE IF NOT EXISTS loans (
    loan_id bigint UNIQUE NOT NULL PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    lendee TEXT NOT NULL REFERENCES accounts(account_name),
    lender TEXT NOT NULL REFERENCES accounts(account_name),
    lent_value NUMERIC NOT NULL CHECK(lent_value >= 0.0),
    rate NUMERIC NOT NULL,
    current_value NUMERIC NOT NULL
);

INSERT INTO accounts (account_name, account_type, cash_in_hand) VALUES ('New West Conifer', 'region', 1000000);