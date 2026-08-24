-- Migración 004: eliminar la columna legacy "condition".
-- El flujo actual de listings no usa condición de carta (NM por definición
-- de negocio); la columna era NOT NULL y bloqueaba todo INSERT con:
--   ERROR: null value in column "condition" ... violates not-null constraint
-- Idempotente. Ejecutar con:
--   go run ./cmd/migrate docs/migration_004_drop_condition.sql

ALTER TABLE public.inventory_listings
    DROP COLUMN IF EXISTS condition;
