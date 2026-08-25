-- Módulo Magic: The Gathering

INSERT INTO games (code, name) VALUES ('mtg', 'Magic: The Gathering')
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS mtg_card_details (
    card_id        BIGINT PRIMARY KEY REFERENCES cards(id) ON DELETE CASCADE,
    mana_cost      TEXT   NOT NULL DEFAULT '',
    mana_value     INT    NOT NULL DEFAULT 0,
    colors         TEXT[] NOT NULL DEFAULT '{}',
    color_identity TEXT[] NOT NULL DEFAULT '{}',
    power          TEXT   NOT NULL DEFAULT '',
    toughness      TEXT   NOT NULL DEFAULT '',
    type_line      TEXT   NOT NULL DEFAULT '',
    rules          JSONB,
    keywords       TEXT[] NOT NULL DEFAULT '{}',
    layout         TEXT   NOT NULL DEFAULT '',
    faces          JSONB,
    rulings        JSONB
);
