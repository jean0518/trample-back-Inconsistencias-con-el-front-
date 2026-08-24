-- Migración 005: eliminar las últimas columnas legacy de inventory_listings.
-- El cálculo de precios COP lo hace el backend en Go (columna price_cop);
-- estas columnas del diseño anterior son NOT NULL sin default y bloquean
-- todo INSERT que no las incluya (SQLSTATE 23502).
--   computed_price_cop : COP calculado antes en DB
--   price_floor_applied: flag del antiguo "precio piso"
--   trm_used           : TRM congelada por fila (hoy se consulta al vuelo)
-- Idempotente. Ejecutar con:
--   go run ./cmd/migrate docs/migration_005_drop_legacy_price_columns.sql

ALTER TABLE public.inventory_listings
    DROP COLUMN IF EXISTS computed_price_cop,
    DROP COLUMN IF EXISTS price_floor_applied,
    DROP COLUMN IF EXISTS trm_used;
