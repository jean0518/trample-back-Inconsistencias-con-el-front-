-- Rediseño del panel admin (docs/nuevos cambios del panel de admin.md).
-- Correr una vez en Supabase ANTES de desplegar el backend.
--
-- Qué toca de lo existente:
--   * sale_items: agrega 4 columnas y las LLENA para las ventas pasadas (solo esas
--     columnas nuevas; no cambia cantidades, precios ni nombres).
--   * cart_reservation_logs: agrega return_reason (las filas viejas quedan NULL).
--   * admin_panels: cambia el nombre visible del panel 'inventario' a 'Cartas'.
--   * Crea la tabla product_reservation_logs y dos índices.

-- 1.3 / 1.4 — Variante, juego y expansión en las líneas de carta (snapshot).
ALTER TABLE sale_items
    ADD COLUMN IF NOT EXISTS variant_name   text,
    ADD COLUMN IF NOT EXISTS game_code      text,
    ADD COLUMN IF NOT EXISTS game_name      text,
    ADD COLUMN IF NOT EXISTS expansion_name text;

-- Ventas pasadas: la variante sale del listing (si todavía existe)...
UPDATE sale_items si
SET variant_name = cv.variant_name
FROM inventory_listings il
JOIN card_variants cv ON cv.id = il.variant_id
WHERE il.id = si.listing_id AND si.variant_name IS NULL;

-- ...y el juego y la expansión, de la carta.
UPDATE sale_items si
SET game_code = g.code, game_name = g.name, expansion_name = e.name
FROM cards c
JOIN games g ON g.id = c.game_id
JOIN expansions e ON e.id = c.expansion_id
WHERE c.id = si.card_id AND si.game_code IS NULL;

-- 3.4 — Por qué se devolvió una reserva: removed | expired | payment_failed.
ALTER TABLE cart_reservation_logs
    ADD COLUMN IF NOT EXISTS return_reason text;

-- 3.3 — Historial de reservas de productos sellados. Sin claves foráneas a
-- propósito: es un snapshot y debe sobrevivir a que se borre el producto o la
-- reserva.
CREATE TABLE IF NOT EXISTS product_reservation_logs (
    id             bigserial   PRIMARY KEY,
    reservation_id bigint      NOT NULL,
    user_id        uuid,
    customer_name  text        NOT NULL DEFAULT '',
    customer_email text        NOT NULL DEFAULT '',
    product_id     bigint,
    title          text        NOT NULL,
    quantity       int         NOT NULL,
    price_cop      bigint      NOT NULL,
    status         text        NOT NULL,   -- reserved | returned | sold
    return_reason  text,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS product_reservation_logs_created_idx
    ON product_reservation_logs (created_at DESC, id DESC);

-- 1.5 — Filtros por fecha en /admin/sales y el desglose de ventas.
CREATE INDEX IF NOT EXISTS sales_created_at_idx ON sales (created_at DESC);

-- 4.1 — El panel de inventario se llama "Cartas" (el code no cambia).
UPDATE admin_panels SET name = 'Cartas' WHERE code = 'inventario';
