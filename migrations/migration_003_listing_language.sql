-- Migración 003: alinear inventory_listings con el esquema esperado por el backend.
-- Corrige: ERROR column "language" of relation "inventory_listings" does not exist
-- (SQLSTATE 42703) al crear listings vía /admin/cards/import-listing o POST /listings.
--
-- Cubre de una vez todas las columnas que el código usa al insertar/leer
-- listings, por si el esquema vivo quedó atrás en más de una columna.
-- Idempotente: puede ejecutarse varias veces sin romper nada.
-- Ejecutar en Supabase SQL Editor o via psql.

ALTER TABLE public.inventory_listings
    ADD COLUMN IF NOT EXISTS price_cop numeric NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS status    text    NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS language  text    NOT NULL DEFAULT 'Inglés';

-- Índices esperados (por si esta tabla se creó a mano sin ellos).
CREATE INDEX IF NOT EXISTS idx_inventory_listings_seller  ON public.inventory_listings (seller_id);
CREATE INDEX IF NOT EXISTS idx_inventory_listings_variant ON public.inventory_listings (variant_id);
CREATE INDEX IF NOT EXISTS idx_inventory_listings_status  ON public.inventory_listings (status);

-- Verificación posterior: deben aparecer TODAS estas columnas:
--   id, seller_id, variant_id, quantity, price_usd, price_cop,
--   status, language, created_at, updated_at
-- SELECT column_name, data_type, column_default
-- FROM information_schema.columns
-- WHERE table_schema = 'public' AND table_name = 'inventory_listings'
-- ORDER BY ordinal_position;
