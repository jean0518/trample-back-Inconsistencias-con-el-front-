# Release/Bold — Checkout con pasarela Bold

Reescribe el flujo de pago con tarjeta para que **no exista ningún pedido hasta que
el pago esté aprobado**, y mueve el botón de pago al paso de confirmar compra.

Ramada: `Release/Bold`

---

## El problema que resuelve

El flujo anterior insertaba la fila en `sales` + `sale_items` con estado
`pending_payment` **antes** de que el cliente pagara. Eso producía tres fallos:

1. El historial mostraba pedidos que nadie había pagado.
2. Esos pedidos contaban en las estadísticas y generaban PDF.
3. El botón de Bold se pintaba con `<script data-bold-button>`, que **redirige a
   otra pestaña** y perdía el estado del carrito.

---

## Modelo de datos

Se separa la **intención de pago** del **pedido**:

| Concepto | Dónde vive | Cuándo existe |
|---|---|---|
| `CheckoutIntent` | tabla `payment_checkouts` (nueva) | desde que el cliente pulsa Pagar hasta que el pago se resuelve |
| `Sale` | tabla `sales` | **solo** cuando el pago fue aprobado |

Una venta nace `paid` o no existe. Se eliminaron los estados `pending_payment`,
`failed` y `cancelled` de `sale.Sale`.

Estados del checkout: `open` → `paid` | `failed` | `expired`.

> ⚠️ **La migración NO está en el repo.** `/db/` está en `.gitignore`, así que
> `db/migrations/015_payment_checkouts.sql` hay que aplicarla a mano en cada
> entorno antes de desplegar:
>
> ```bash
> go run ./cmd/migrate db/migrations/015_payment_checkouts.sql
> ```
>
> Sin ella, `POST /sales/checkout` responde **500** porque la tabla no existe.
> La migración también borra las ventas `pending_payment` antiguas y libera sus
> reservas, registrando el movimiento en `cart_reservation_logs`.

---

## Endpoints

### `POST /sales/checkout` (nuevo)

Prepara el pago. **No crea ningún pedido**: extiende la retención del stock
(`BOLD_PAYMENT_HOLD_MINUTES`, por defecto 30 min) y devuelve los datos del modal
de Bold.

```json
{
  "reference": "TRM-1790784528271-3166b9e8ca2c884d",
  "status": "open",
  "total_cop": 98000,
  "total_usd": 24.5,
  "expires_at": "2026-09-30T18:19:57Z",
  "bold": {
    "reference": "TRM-...",
    "amount_cop": 98000,
    "currency": "COP",
    "integrity_signature": "…",
    "identity_key": "…",
    "redirection_url": "https://…/carrito",
    "origin_url": "https://…/carrito",
    "description": "Pedido TRM-…",
    "expiration_ns": 1790785197566135400,
    "customer_data": "{…}",
    "billing_address": "{…}"
  }
}
```

- Si el cliente ya tiene un checkout abierto **con el mismo carrito**, se devuelve
  ese mismo (misma referencia) en vez de crear otro. Así Bold nunca ve dos
  pedidos para una sola compra y el botón "reintentar" no puede duplicar el cobro.
- Si el carrito cambió, los checkouts abiertos anteriores se cierran y liberan su
  stock antes de crear el nuevo.

### `POST /sales/payment-status`

Reconcilia con la pasarela. Idempotente.

```json
{ "reference": "TRM-…", "status": "open | paid | failed | expired", "sale": { … } }
```

`sale` viene **solo** si el pago ya fue aprobado.

### `POST /sales`

Ahora solo acepta `efectivo` y `transferencia`. Si llega `bold`, responde **400**
con un mensaje que apunta a `/sales/checkout`.

### `POST /webhooks/bold`

Notificaciones de la pasarela. Registra el evento y solo aplica el cambio la
primera vez que llega (Bold reintenta hasta 5 veces).

| Evento | Efecto |
|---|---|
| `SALE_APPROVED` | crea el pedido, confirma reservas y descuenta inventario |
| `SALE_REJECTED` | libera el stock, no crea pedido |
| `VOID_APPROVED` / `VOID_REJECTED` | solo si el checkout seguía abierto; si ya se aprobó, el reembolso es manual |

---

## Archivos nuevos

| Archivo | Responsabilidad |
|---|---|
| `internal/domain/payment/payment.go` | tipos de dominio de la pasarela y del checkout |
| `internal/ports/out/payment_gateway.go` | puerto `PaymentGateway` (construir checkout, consultar estado) |
| `internal/adapters/out/bold/client.go` | adaptador Bold: firma de integridad, checkout, consulta de estado |
| `internal/adapters/in/http/bold_webhook_handler.go` | handler del webhook con verificación de firma |
| `internal/application/sale/checkout.go` | `CreateCheckoutUseCase`: valida, retiene stock, reutiliza o crea |
| `internal/application/sale/bold.go` | `CheckPaymentStatusUseCase`, `ProcessPaymentEventUseCase`, `ExpireCheckoutsUseCase` |

## Archivos modificados

| Archivo | Cambio |
|---|---|
| `internal/domain/sale/sale.go` | `CheckoutIntent`, `PaymentStatus`, estados del checkout; fuera `PaymentExpiresAt` y los estados no pagados |
| `internal/ports/out/sale_repository.go` | puerto rediseñado; `ApprovePayment` es el único camino que crea una venta |
| `internal/adapters/out/postgres/sale_repository_pg.go` | reescrito: checkouts, aprobación atómica, rechazo, cierre, expiración |
| `internal/application/sale/confirm.go` | `ConfirmSaleUseCase` solo cobra en el acto; comparte el cálculo del carrito |
| `internal/adapters/in/http/sale_handler.go` | handlers y contratos JSON nuevos |
| `internal/adapters/in/http/router.go` | ruta `POST /sales/checkout` |
| `cmd/api/main.go` | wiring y `expireCheckoutsLoop` |
| `pkg/config/config.go` | variables de Bold |

---

## Decisiones de diseño

**Aprobación atómica.** `ApprovePayment` inserta el pedido, confirma las
reservas y descuenta el inventario en una sola transacción. Si el pago llegó
tarde y el stock ya se había liberado, el pedido se crea igual pero con
`requires_review = true` para resolverlo a mano: el cliente pagó.

**Stock y checkout vencen juntos.** Al reintentar se extiende la retención de las
reservas **y** `expires_at` del checkout. Si solo se moviera el stock, el barrido
(`ExpireCheckouts`) podría liberar las cartas mientras el cliente seguía
pagando. Por eso existe `ExtendCheckout`.

**Idempotencia en las dos vías.** El webhook se deduplica por ID de evento y
`ApprovePayment` detecta que el checkout ya estaba pagado. El frontend puede
preguntar el estado todas las veces que quiera sin crear pedidos de más.

**La llave secreta nunca sale del servidor.** La firma de integridad se calcula
en el backend; el frontend solo recibe `integrity_signature` e `identity_key`
(esta última es pública).

---

## Pruebas

38 casos nuevos en `internal/application/sale/`:

| Archivo | Cubre |
|---|---|
| `checkout_test.go` | no crea pedido, totales, envío, retención de stock, reutilización, cierre por cambio de carrito o vencimiento, referencia con formato válido para Bold, renovación de expiración, recreación si el checkout se cerró |
| `confirm_test.go` | efectivo exige staff, venta de admin, **tarjeta rechazada sin crear nada**, reserva vencida, carrera al confirmar, datos de envío obligatorios |
| `bold_test.go` | pago aprobado crea el pedido, **no lo duplica** en consultas repetidas, pendiente/rechazado no crean nada, pasarela caída, propiedad del checkout, referencia desconocida, webhook idempotente |
| `fakes_test.go` | dobles de los puertos |

```bash
go build ./... && go vet ./... && go test ./...
```

---

## Variables de entorno

| Variable | Para qué |
|---|---|
| `BOLD_IDENTITY_KEY` | llave de identidad, pública |
| `BOLD_SECRET_KEY` | llave secreta, solo en el servidor |
| `BOLD_ENV` | `test` o `production`; cambia la verificación de la firma del webhook |
| `BOLD_API_BASE_URL` | por defecto `https://payments.api.bold.co` |
| `BOLD_PAYMENT_HOLD_MINUTES` | minutos de retención al pulsar Pagar |
| `FRONTEND_URL` | base del frontend; Bold **exige https** |

> Bold exige que `FRONTEND_URL` use `https://`. Con `http://localhost:5173` el
> botón falla con **BTN-001**. Para pruebas locales usa `https://localhost:5173`,
> nunca `127.0.0.1`.