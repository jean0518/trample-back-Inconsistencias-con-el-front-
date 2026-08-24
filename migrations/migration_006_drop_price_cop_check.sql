-- Migración 006: eliminar el CHECK legacy inventory_listings_price_cop_check.
-- La regla vieja exigía price_cop >= 1000 (mínimo ~USD 0,25) y bloquea la
-- publicación de cartas baratas, p. ej. "Diana, Lunari" de Riftbound:
--   ERROR: new row violates check constraint "inventory_listings_price_cop_check"
--   (SQLSTATE 23514)
-- El precio mínimo ya no es regla de la DB; se reemplaza por un límite
-- defensivo >= 0. Idempotente. Ejecutar con:
--   go run ./cmd/migrate docs/migration_006_drop_price_cop_check.sql

ALTER TABLE public.inventory_listings
    DROP CONSTRAINT IF EXISTS inventory_listings_price_cop_check;

ALTER TABLE public.inventory_listings
    ADD CONSTRAINT inventory_listings_price_cop_check CHECK (price_cop >= 0);
