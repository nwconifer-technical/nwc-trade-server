CREATE TYPE accountType as ENUM ('region', 'nation');

CREATE TABLE IF NOT EXISTS accounts (
    account_name TEXT UNIQUE NOT NULL PRIMARY KEY,
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

INSERT INTO accounts (account_name, account_type cash_in_hand) VALUES ('The Exchange', 'region', 1000000);