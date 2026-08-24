-- Migración 002: permitir stock cero en inventory_listings.
-- Regla de negocio: quantity = 0 ⇒ status 'inactive' (lo aplica el backend al
-- actualizar); el listing NO se elimina para conservar histórico.
-- Ejecutar en Supabase SQL Editor o via psql.

ALTER TABLE public.inventory_listings
    DROP CONSTRAINT IF EXISTS inventory_listings_quantity_check;

ALTER TABLE public.inventory_listings
    ADD CONSTRAINT inventory_listings_quantity_check CHECK (quantity >= 0);
