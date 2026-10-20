-- Correos de compra, cancelación solicitada, recordatorio de recogida, despacho
-- y pagos en proceso. Correr una vez en Supabase (SQL editor) ANTES de desplegar
-- el backend: las consultas nuevas leen estas columnas.

ALTER TABLE sales
    -- El cliente pidió cancelar un pedido de recogida; el staff aprueba o rechaza.
    ADD COLUMN IF NOT EXISTS cancel_requested_at     timestamptz,
    -- Se envió el recordatorio de "tu pedido vence mañana" (una sola vez).
    ADD COLUMN IF NOT EXISTS pickup_reminder_sent_at timestamptz,
    -- Despacho de los pedidos con envío a domicilio.
    ADD COLUMN IF NOT EXISTS shipped_at              timestamptz,
    ADD COLUMN IF NOT EXISTS carrier                 text,
    ADD COLUMN IF NOT EXISTS tracking_number         text,
    -- Entregado por la transportadora o recogido en la tienda.
    ADD COLUMN IF NOT EXISTS delivered_at            timestamptz;

-- El barrido del recordatorio solo mira los pedidos pendientes sin recordar.
CREATE INDEX IF NOT EXISTS sales_pickup_reminder_idx
    ON sales (pickup_deadline)
    WHERE status = 'pending_pickup' AND pickup_reminder_sent_at IS NULL;

-- La pasarela informó que el pago quedó en proceso (PSE, transferencia): se
-- avisó al cliente que el pedido se recibió y se espera el pago hasta 2 horas.
ALTER TABLE payment_checkouts
    ADD COLUMN IF NOT EXISTS payment_pending_at timestamptz;
