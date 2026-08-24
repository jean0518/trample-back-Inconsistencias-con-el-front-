-- Estandarización de precios COP vigentes.
--
-- Reglas:
--   * Precio < 1 USD        ⇒ price_cop fijo de 2000.
--   * Precio >= 1 USD       ⇒ redondeo hacia arriba al siguiente múltiplo
--                             de 1000 (ej: 10447 ⇒ 11000).
--
-- Idempotente: aplicarla nuevamente no cambia los valores.
-- Aplica a inventario (inventory_listings, fuente del stock) y al precio NM
-- publicado en el catálogo (variant_prices).

UPDATE inventory_listings
SET price_cop = 2000
WHERE price_usd < 1;

UPDATE inventory_listings
SET price_cop = CEIL(price_cop / 1000.0)::bigint * 1000
WHERE price_usd >= 1;

UPDATE variant_prices
SET price_cop = 2000
WHERE condition = 'near_mint'
  AND price_usd < 1;

UPDATE variant_prices
SET price_cop = CEIL(price_cop / 1000.0)::bigint * 1000
WHERE condition = 'near_mint'
  AND price_usd >= 1;
