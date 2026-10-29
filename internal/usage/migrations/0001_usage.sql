-- One row per (UTC day, event) with the number of times the event happened
-- that day. day is YYYY-MM-DD in UTC; event is one of the names in
-- usage.Events. Nothing else is stored: no addresses, agents, cookies, titles
-- or identifiers. The primary key is the upsert target.
CREATE TABLE usage_counts (
    day   TEXT    NOT NULL,
    event TEXT    NOT NULL,
    n     INTEGER NOT NULL,
    PRIMARY KEY (day, event)
);
