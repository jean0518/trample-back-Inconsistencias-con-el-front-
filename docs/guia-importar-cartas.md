# Guía: Agregar cartas al catálogo (flujo único desde el inventario)

> **Flujo oficial:** la única forma de agregar cartas al catálogo es desde
> **Admin → Inventario → "Agregar cartas"**. El diálogo busca en Scrydex,
> permite seleccionar varias cartas y las publica directo al **inventario**
> (listings) con cantidad, precio e idioma. El catálogo público se alimenta
> del inventario: una carta sin listing activo no aparece en la tienda.

---

## Paso 1 — Listar juegos

```
GET /games
```

```json
[
  { "id": 1, "code": "pokemon", "name": "Pokémon" },
  { "id": 2, "code": "mtg",     "name": "Magic: The Gathering" },
  { "id": 3, "code": "riftbound", "name": "Riftbound" }
]
```

Guardá `id` y `code` del juego elegido.

## Paso 2 — Listar expansiones del juego

```
GET /expansions?game_id={id}
```

Guardá el `ExternalID` de la expansión elegida (es el `expansion_code` de Scrydex).

> Si no aparecen expansiones, un admin debe sincronizarlas primero:
> `POST /admin/pokemon/expansions/sync`, `POST /admin/magic/expansions/sync`.

## Paso 3 — Buscar en Scrydex (filtros, todos server-side)

Requiere **token de admin**.

```
POST /scrydex/{code}/cards
Authorization: Bearer <token>
Content-Type: application/json
```

| Campo            | Requerido | Descripción                                        |
|------------------|-----------|----------------------------------------------------|
| `name`           | Sí        | Nombre parcial — busca por prefijo (ej: "char")    |
| `expansion_code` | No        | `ExternalID` de la expansión (paso 2)              |
| `rarity`         | No        | Rareza exacta (ej: "Rare Holo")                    |
| `variants`       | No        | Array de variantes (ej: ["non-foil", "foil"])      |
| `type`           | No        | Tipo de carta (ej: "Fire", "Creature")             |
| `supertype`      | No        | Supertipo (ej: "Pokémon", "Trainer")               |

**Response**

```json
{
  "search_id": "search_e0d18c57ac78df66",
  "total": 3,
  "cards": [ ... ]
}
```

El `search_id` expira a los **15 minutos**. La selección persiste aunque
cambies de juego o hagas otra búsqueda: podés acumular grupos antes de importar.

## Paso 4 — Publicar al inventario TODAS las seleccionadas

Con el `search_id` y los datos de publicación de cada carta:

```
POST /admin/cards/import-listing
Authorization: Bearer <token>
Content-Type: application/json
```

```json
{
  "groups": [
    {
      "search_id": "search_e0d18c57ac78df66",
      "items": [
        { "external_id": "sv8-54", "quantity": 5, "price_usd": 2.10, "language": "Inglés" },
        { "external_id": "sv8-199", "quantity": 1 }
      ]
    }
  ]
}
```

Reglas:

- `price_usd` es opcional: si no viene se usa el **precio de mercado NM**;
  error si la carta no tiene precio de mercado.
- `language` opcional (default `"Inglés"`).
- **Reglas de precio COP** (`listing.StandardizedPriceCOP`):
  - Carta por debajo de 1 USD ⇒ precio fijo de **$2.000 COP**.
  - Resto ⇒ TRM del día redondeada hacia arriba al siguiente múltiplo de
    **1.000** (ej: 10.447 ⇒ **$11.000 COP**).
- La carta se guarda en el catálogo y se crea su listing en un solo paso.
- Si el admin ya tenía un listing vigente para esa variante, **se suma stock**
  conservando precio e idioma originales (`merged: true`).

**Response**

```json
{
  "imported": 2,
  "listings": [
    {
      "listing_id": 12, "game_code": "pokemon", "external_id": "sv8-54",
      "card_name": "...", "variant_name": "Normal",
      "quantity": 5, "price_usd": 2.10, "price_cop": 8820,
      "status": "active", "merged": false
    }
  ]
}
```

El precio COP lo calcula el backend con la TRM del día.

## Gestión posterior del stock

```
PATCH /listings/{id}   { "quantity": 3 }
```

- `quantity == 0` ⇒ el listing pasa a `inactive` (deja de verse en la tienda).
- `quantity > 0` ⇒ vuelve a `active`.
- `DELETE /listings/{id}` elimina el listing definitivamente.

---

## Resumen del flujo

```
Inventario → "Agregar cartas"
  ├─ GET /games                          → elegir juego
  ├─ GET /expansions?game_id={id}        → elegir expansión
  ├─ POST /scrydex/{code}/cards          → buscar por nombre + filtros
  ├─ (selección múltiple entre búsquedas)
  └─ POST /admin/cards/import-listing    → carta + listing juntos

GET /catalog/cards                       → catálogo público (solo con stock activo)
PATCH /listings/{id}                     → ajustar stock (0 = inactivo)
```

---

## Endpoints eliminados (legacy)

| Endpoint eliminado                  | Reemplazo                                    |
|-------------------------------------|----------------------------------------------|
| `POST /admin/pokemon/cards/import`  | búsqueda + `POST /admin/cards/import-listing` |
| `POST /admin/magic/cards/import`    | búsqueda + `POST /admin/cards/import-listing` |
| `POST /admin/cards/import`          | `POST /admin/cards/import-listing`           |
| `GET /cards` (no existía en backend)| `GET /catalog/cards`                         |

## Seguridad

Todas las rutas `/scrydex/*` requieren **rol admin** (antes magic/riftbound
estaban sin proteger). Las rutas `/admin/*` también.
