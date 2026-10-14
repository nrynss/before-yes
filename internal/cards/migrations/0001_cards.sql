CREATE TABLE cards (
    id         TEXT    PRIMARY KEY,
    title_key  TEXT    NOT NULL,
    age        INTEGER NOT NULL,
    checklist  TEXT    NOT NULL,
    verdict    TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    body       BLOB    NOT NULL
);

CREATE INDEX cards_lookup ON cards (title_key, age, checklist, created_at);
