-- One row per stored card. seq is the insert order, so a tie on created_at
-- breaks to the card stored last and stays stable across a VACUUM. id is the
-- public Keel id (32 lowercase hex). title is the card's own title as shown;
-- title_key is the folded title every lookup uses. created_at is the card's
-- CheckedAt in unix milliseconds. body is the card's JSON, whose id field is
-- the row's id.
CREATE TABLE cards (
    seq        INTEGER PRIMARY KEY AUTOINCREMENT,
    id         TEXT    NOT NULL UNIQUE,
    title      TEXT    NOT NULL,
    title_key  TEXT    NOT NULL,
    age        INTEGER NOT NULL,
    checklist  TEXT    NOT NULL,
    verdict    TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    body       BLOB    NOT NULL
);

-- Latest: a card by title key, age and checklist version, newest first.
CREATE INDEX cards_lookup ON cards (title_key, age, checklist, created_at);

-- Lookup by verdict and timestamp, and the recent list's order.
CREATE INDEX cards_verdict ON cards (verdict, created_at);
CREATE INDEX cards_recent ON cards (created_at);
