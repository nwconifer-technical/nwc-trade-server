CREATE TYPE accountType as ENUM ('region', 'nation');

CREATE TABLE IF NOT EXISTS accounts (
    account_name TEXT UNIQUE NOT NULL PRIMARY KEY ON DELETE CASCADE,
    account_pass_hash TEXT,
    account_type accountType NOT NULL DEFAULT 'nation',
    cash_in_hand NUMERIC(100,2) NOT NULL DEFAULT 0.0 CHECK(cash_in_hand >= 0.0),
    cash_in_escrow NUMERIC(100,2) NOT NULL DEFAULT 0.0 CHECK(cash_in_escrow >= 0.0)
);

CREATE TYPE perm as ENUM ('admin', 'trader', 'citizen');

CREATE TABLE IF NOT EXISTS nation_permissions (
    region_name TEXT NOT NULL REFERENCES accounts(account_name),
    nation_name TEXT NOT NULL REFERENCES accounts(account_name),
    permission perm NOT NULL,
    PRIMARY KEY(region_name, nation_name),
    CONSTRAINT separateThings CHECK(region_name != nation_name)
);

CREATE TABLE IF NOT EXISTS loans (
    loan_id bigint UNIQUE NOT NULL PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    lendee TEXT NOT NULL REFERENCES accounts(account_name),
    lender TEXT NOT NULL REFERENCES accounts(account_name),
    lent_value NUMERIC(100,2) NOT NULL CHECK(lent_value >= 0.0),
    rate NUMERIC(100,2) NOT NULL,
    current_value NUMERIC NOT NULL
);

CREATE TABLE IF NOT EXISTS stocks (
    ticker TEXT UNIQUE NOT NULL PRIMARY KEY,
    region TEXT REFERENCES accounts(account_name),
    market_cap NUMERIC(100,2) NOT NULL DEFAULT 0.0 CHECK(market_cap >= 0.0),
    total_share_volume INT NOT NULL DEFAULT 0,
    share_price NUMERIC(100,2)
);

CREATE TABLE IF NOT EXISTS stock_holdings (
    ticker TEXT NOT NULL REFERENCES stocks(ticker),
    account_name TEXT NOT NULL REFERENCES accounts(account_name),
    share_quant INT NOT NULL DEFAULT 0 CHECK(share_quant >= 0),
    avg_price NUMERIC(100,2) DEFAULT 0.0 CHECK(avg_price >= 0.0),
    PRIMARY KEY(ticker, account_name)
);

CREATE TYPE direction as ENUM ('buy', 'sell');
CREATE TYPE priceType as ENUM ('market', 'limit');

CREATE TABLE IF NOT EXISTS open_orders (
    trade_id bigint UNIQUE NOT NULL PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    ticker TEXT NOT NULL REFERENCES stocks(ticker),
    trader TEXT NOT NULL REFERENCES accounts(account_name),
    quant INT NOT NULL CHECK(quant >= 0),
    -- remaining_quant INT NOT NULL CHECK(quant >= 0),
    order_direction direction NOT NULL,
    price_type priceType NOT NULL,
    order_price NUMERIC(100,2) CHECK(order_price >= 0.0)
)

INSERT INTO accounts (account_name, account_type, cash_in_hand) VALUES ('New West Conifer', 'region', 1000000);