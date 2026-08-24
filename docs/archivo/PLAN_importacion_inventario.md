# Plan de acción: Flujo importación → inventario → catálogo

> ⚠️ **DOCUMENTO ARCHIVADO — PLAN COMPLETADO.** Las 4 fases se implementaron y
> verificaron (agosto 2026). Se conserva como registro histórico; el flujo
> vigente está documentado en `docs/guia-importar-cartas.md` y
> `docs/guia-listar-cartas-db.md`.

> Alcance: cambios en `trample-back` (Go) y `Ecommerce-TrampleStore` (React).
> Rama de trabajo: `feature/allgame` en ambos repositorios.

## Contexto y problema actual

1. Al buscar cartas en el diálogo de importación, cambiar de juego/expansión o hacer una
   nueva búsqueda **borra la selección anterior**: solo se puede importar de una búsqueda a la vez.
2. El import guarda las cartas únicamente en la tabla `cards` (catálogo DB). El listing
   (inventario) se crea después a mano con `POST /listings`, lo que rompe la consistencia
   del flujo inicial: **el inventario (listings) debe ser la fuente del catálogo público**.
3. No existe forma de indicar la **cantidad** al dar de alta una carta encontrada.
4. Los botones +/− de stock del inventario son solo visuales: no hay `PATCH /listings/{id}`
   en el backend, ni regla de estado cuando el stock llega a 0.
5. El menú admin expone "Catálogo DB", que muestra cartas aunque no tengan inventario.
6. El catálogo público (`GET /catalog/cards`) lee toda la tabla `cards` sin considerar listings.

## Decisiones técnicas

| Decisión | Justificación |
|---|---|
| Selección agrupada por `search_id` (front acumula `{search_id, external_ids[]}`) | El back resuelve los datos desde su caché en memoria por búsqueda: evita manipulación de precios desde el cliente y re-consumo de Scrydex |
| Nuevo endpoint `POST /admin/cards/import-listing` | Crea carta + variante + listing con cantidad en un solo paso, atómico desde la perspectiva del admin |
| Variante por defecto para el listing: la primera variante devuelta por Scrydex | Simplifica v1; elegir variante específica queda como mejora posterior |
| Regla de estado automática: `quantity == 0 → inactive`, `quantity > 0 → active` | El requisito pide que stock 0 se muestre inactivo; el estado `sold` nunca se toca automáticamente |
| Migración SQL: relajar `CHECK (quantity > 0)` a `quantity >= 0` | Necesario para permitir stock 0 sin eliminar el listing |
| `GET /listings` enriquecido con nombre/imagen/juego de la carta | Sin esto el inventario muestra solo IDs ilegibles (#123, variante 456) |
| Catálogo público filtra por existencia activa en listings | Consistencia: solo se vende lo que hay en inventario |

---

## FASE 1 — Selección temporal multi-búsqueda ("bandeja de selección") ✅

Las cartas seleccionadas permanecen en un estado temporal mientras se buscan otras,
de otros juegos o expansiones.

### Back (trample-back)
- [x] `internal/application/catalog/import_card.go`: nuevo tipo `ImportGroup{SearchID, ExternalIDs}`
      y método `ImportByGroups(ctx, groups)`:
      - Resuelve cada grupo contra la caché de búsquedas existente.
      - Deduplica por `game_code + external_id` entre grupos (SyncCard ya es UPSERT idempotente).
      - Error claro si algún `search_id` expiró (TTL 15 min).
- [x] `internal/adapters/in/http/pokemon_handler.go`: `ImportCards` acepta body nuevo
      `{ groups: [{search_id, external_ids}] }` manteniendo compatibilidad con el formato actual.
- [x] Test unitario del caso de uso (grupos múltiples, dedupe, expiración).

### Front (Ecommerce-TrampleStore)
- [x] Reemplazar `selectedIds: Set<string>` por una selección persistente
      `[{card, searchId}]` que NO se limpia al cambiar juego/expansión ni al buscar de nuevo.
- [x] Bandeja visual de seleccionadas: miniaturas con quitar individual,
      contador global y botón "Limpiar todo".
- [x] `ImportCardsInput` envía `groups`.

### Criterios de aceptación
- Busco "pikachu" (pokemon) → selecciono 2 → cambio juego a MTG → busco → selecciono 1:
  la bandeja muestra 3 y ninguna se perdió.
- Importar envía 2 grupos y el back importa 3 cartas.

---

## FASE 2 — Importar con datos de listing (cantidad, precio, idioma) ✅

Ejemplo: busco "team rocket petrel" → la selecciono → indico cantidad 5, idioma, precio
(opcional, prellenado con el precio de mercado NM) → se crean carta + listing juntos.

### Back (trample-back)
- [x] Endpoint `POST /admin/cards/import-listing`:
      body `{ groups: [{search_id, items: [{external_id, quantity, price_usd?, language?}]}] }`,
      respuesta `{ imported, listings }` (listings creados con sus IDs).
- [x] Caso de uso `ImportListingUseCase` (`import_listing.go`): por cada carta → `SyncCard` →
      resolver variante por defecto (primera de la búsqueda cacheada) vía
      `CardRepository.GetVariantID` → crear listing del admin autenticado.
      El precio COP se calcula con la TRM del día.
- [x] Validaciones: `quantity >= 1`, precio USD > 0 (si no viene, usar precio NM;
      error si no hay ninguno). Duplicados entre búsquedas se omiten.

### Front (Ecommerce-TrampleStore)
- [x] Paso 2 del diálogo de importación ("Datos de publicación"): fila por carta
      seleccionada con cantidad, precio USD (prellenado con mercado) e idioma.
- [x] Enviar grupos con `items` por búsqueda.
- [x] Éxito: mensaje con N cartas publicadas al inventario e invalidar queries
      `listings` + `products` + `catalog-cards`.

### Criterios de aceptación
- Importo "team rocket petrel" con cantidad 5: aparece en Inventario con cantidad 5
  y en el catálogo público sin recargar manualmente.

---

## FASE 3 — Stock 0 ⇒ inactivo + gestión real de cantidades ✅

### Back (trample-back)
- [x] Migración `migrations/migration_002_listing_stock_cero.sql`: `quantity >= 0`.
- [x] `PATCH /listings/{id}` (dueño del listing): actualizar `quantity`.
      Regla automática en SQL (atómica): `quantity == 0 → status 'inactive'`;
      `quantity > 0 → status 'active'` (si estaba inactive). Nunca tocar `sold`.
      Error sentinela `listing.ErrNotFound` para 404 limpio.
- [x] `GET /listings` enriquecido: incluye `card_name`, `card_image`, `expansion_name`,
      `game_name`, `variant_name` (JOIN con cards/expansions/card_images).
      Bonus: se corrigió wiring faltante de `ListingHandler` en `main.go`
      (hubiera hecho panic al arrancar).

### Front (Ecommerce-TrampleStore)
- [x] Botones +/− llaman a `PATCH /listings/{id}` con update optimista.
- [x] Fila con stock 0: badge "Inactivo" + opacidad reducida; botón − deshabilitado en 0.
- [x] Tabla legible: miniatura + nombre de carta, juego/expansión, variante e idioma,
      precios reales del servidor. Tipo `Listing` alineado a la respuesta real
      (se quitaron campos legacy que el back nunca envió: Condition, ComputedPriceCOP...).

### Criterios de aceptación
- Bajar stock a 0 con −: el listing queda inactivo y visible como tal.
- Subir a 1+: vuelve a activo.

---

## FASE 4 — Catálogo público desde inventario + limpieza del admin ✅

### Back (trample-back)
- [x] `ListCards` (catálogo público): solo cartas con al menos un listing
      `status = 'active' AND quantity > 0`; la respuesta incluye `stock`
      agregado por carta.

### Front (Ecommerce-TrampleStore)
- [x] Eliminar entrada "Catálogo DB" de `ADMIN_NAV` (`src/constants/nav.ts`)
      y ruta `ADMIN_CATALOG` (`src/constants/routes.ts`).
- [x] Eliminar ruta `/admin/catalogo` (`src/app/routes.tsx`) y borrar
      `CatalogCardsPage.tsx` + hook `useCatalogCards`.
- [x] Cartas/Sellados/Juegos/Home/Producto consumen `useCatalog` → heredan el
      filtro por inventario; el mapper expone el stock real.

### Criterios de aceptación
- Una carta sin listings activos no aparece en el catálogo público.
- El admin ya no ve "Catálogo DB" en el menú.

---

## Operación — migraciones de base de datos

Aplicar en orden (todas idempotentes). Ya existe `cmd/migrate`, un ejecutor
que lee `DATABASE_URL` del `.env`:

```bash
go run ./cmd/migrate migrations/migration_002_listing_stock_cero.sql
go run ./cmd/migrate migrations/migration_003_listing_language.sql
```

| Archivo | Qué hace | Estado |
|---|---|---|
| `migration_001_listings.sql` | Tabla `inventory_listings` base | ✅ (tabla existía) |
| `migration_002_listing_stock_cero.sql` | CHECK `quantity >= 0` | ✅ aplicada |
| `migration_003_listing_language.sql` | Columna `language` (+ índices) | ✅ aplicada |
| `migration_004_drop_condition.sql` | Elimina columna legacy `condition` | ✅ aplicada |
| `migration_005_drop_legacy_price_columns.sql` | Elimina legacy `computed_price_cop`, `price_floor_applied`, `trm_used` | ✅ aplicada |
| `migration_006_drop_price_cop_check.sql` | Reemplaza CHECK legacy (`price_cop >= 1000`) por `price_cop >= 0` | ✅ aplicada |

Con esto el esquema vivo de `inventory_listings` queda exactamente alineado
con el que usa el backend: toda columna `NOT NULL` recibe valor del INSERT
o tiene default, y los únicos CHECK son los vigentes (`quantity >= 0`,
`price_cop >= 0`). Sin triggers. El precio COP se calcula en Go con la TRM
(`listing.CreateInput.PriceCOP`).

Diagnóstico rápido de drift de esquema:

```bash
go run ./cmd/migrate -sql "SELECT column_name, data_type FROM information_schema.columns WHERE table_schema='public' AND table_name='inventory_listings' ORDER BY ordinal_position"
```

---

## Orden de ejecución (por partes)

1. **Fase 1** — bandeja de selección (back → front).
2. **Fase 2** — import con datos de listing (back → front).
3. **Fase 3** — stock 0/inactivo + PATCH + listings enriquecidos (back → front).
4. **Fase 4** — catálogo por inventario + limpieza menú (back → front).

Cada fase termina con build/tests verdes en ambos proyectos antes de pasar a la siguiente.
