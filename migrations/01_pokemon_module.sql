-- Módulo Pokémon

-- ============================================================
-- Juegos
-- ============================================================

CREATE TABLE IF NOT EXISTS games (
    id   BIGSERIAL PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL
);

INSERT INTO games (code, name) VALUES ('pokemon', 'Pokémon TCG')
ON CONFLICT (code) DO NOTHING;

-- ============================================================
-- Expansiones
-- ============================================================

CREATE TABLE IF NOT EXISTS expansions (
    id           BIGSERIAL PRIMARY KEY,
    game_id      BIGINT NOT NULL REFERENCES games(id),
    external_id  TEXT NOT NULL,
    name         TEXT NOT NULL,
    code         TEXT NOT NULL DEFAULT '',
    series       TEXT NOT NULL DEFAULT '',
    total        INT  NOT NULL DEFAULT 0,
    release_date TEXT NOT NULL DEFAULT '',
    logo_url     TEXT NOT NULL DEFAULT '',
    symbol_url   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (game_id, external_id)
);

-- ============================================================
-- Cartas (campos comunes a cualquier juego)
-- ============================================================

CREATE TABLE IF NOT EXISTS cards (
    id           BIGSERIAL PRIMARY KEY,
    game_id      BIGINT NOT NULL REFERENCES games(id),
    expansion_id BIGINT NOT NULL REFERENCES expansions(id),
    external_id  TEXT NOT NULL,
    name         TEXT NOT NULL,
    number       TEXT NOT NULL DEFAULT '',
    rarity       TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),   -- fecha de ingreso a la DB
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (game_id, external_id)
);

-- ============================================================
-- Detalles específicos de Pokémon (1 fila por carta)
-- ============================================================

CREATE TABLE IF NOT EXISTS pokemon_card_details (
    card_id      BIGINT PRIMARY KEY REFERENCES cards(id) ON DELETE CASCADE,
    hp           INT,
    types        TEXT[]  NOT NULL DEFAULT '{}',
    evolves_from TEXT    NOT NULL DEFAULT '',
    stage        TEXT    NOT NULL DEFAULT '',   -- Basic, Stage 1, Stage 2, etc.
    attacks      JSONB,
    weaknesses   JSONB,
    resistances  JSONB,
    retreat_cost INT     NOT NULL DEFAULT 0
);

-- ============================================================
-- Variantes de carta (Normal, Reverse Holo, Holo, etc.)
-- ============================================================

CREATE TABLE IF NOT EXISTS card_variants (
    id                  BIGSERIAL PRIMARY KEY,
    card_id             BIGINT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    external_id         TEXT   NOT NULL DEFAULT '',
    variant_name        TEXT   NOT NULL,
    last_price_check_at TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (card_id, variant_name)
);

-- ============================================================
-- Imágenes (de carta o de variante, nunca las dos a la vez)
-- ============================================================

CREATE TABLE IF NOT EXISTS card_images (
    id         BIGSERIAL PRIMARY KEY,
    card_id    BIGINT REFERENCES cards(id)         ON DELETE CASCADE,
    variant_id BIGINT REFERENCES card_variants(id) ON DELETE CASCADE,
    image_type TEXT NOT NULL DEFAULT 'front',
    small_url  TEXT NOT NULL DEFAULT '',
    medium_url TEXT NOT NULL DEFAULT '',
    large_url  TEXT NOT NULL DEFAULT '',
    CHECK (num_nonnulls(card_id, variant_id) = 1)  -- exactamente uno de los dos
);

-- ============================================================
-- Precios por variante (USD y COP, cacheados)
-- ============================================================

CREATE TABLE IF NOT EXISTS variant_prices (
    id          BIGSERIAL PRIMARY KEY,
    variant_id  BIGINT NOT NULL REFERENCES card_variants(id) ON DELETE CASCADE,
    condition   TEXT NOT NULL,               -- near_mint, lightly_played, etc.
    price_usd   NUMERIC(12, 4) NOT NULL,
    price_cop   BIGINT        NOT NULL DEFAULT 0,
    trm_used    NUMERIC(10, 4),              -- TRM con la que se calculó el COP
    fetched_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (variant_id, condition)
);

-- Vista: variantes cuyo precio lleva más de 7 días sin actualizarse
CREATE OR REPLACE VIEW v_variants_needing_price_refresh AS
SELECT cv.id AS variant_id,
       cv.card_id,
       cv.variant_name,
       cv.last_price_check_at
FROM card_variants cv
WHERE cv.last_price_check_at IS NULL
   OR cv.last_price_check_at < now() - INTERVAL '7 days';
