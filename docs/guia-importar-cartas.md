# Guía: Agregar cartas al catálogo (flujo único desde el inventario)

> **Flujo oficial:** la única forma de agregar cartas al catálogo es desde
> **Admin → Inventario → "Agregar cartas"**. El diálogo busca en Scrydex,
> permite seleccionar varias cartas y las importa todas juntas a la DB.

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

## Paso 3 — Buscar en Scrydex (5 filtros, todos server-side)

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

**Response**

```json
{
  "search_id": "search_e0d18c57ac78df66",
  "total": 3,
  "cards": [ ... ]
}
```

El `search_id` expira a los **15 minutos**.

## Paso 4 — Importar TODAS las cartas seleccionadas

Con el `search_id` y los `external_id` de todas las cartas marcadas:

```
POST /admin/cards/import
Authorization: Bearer <token>
Content-Type: application/json
```

```json
{
  "search_id": "search_e0d18c57ac78df66",
  "external_ids": ["sv8-54", "sv8-55", "sv8-199"]
}
```

**Response**

```json
{ "imported": 3, "cards": [...] }
```

Las cartas quedan guardadas en el catálogo (`GET /catalog/cards`) y son
consumidas por la tienda pública.

---

## Resumen del flujo

```
Inventario → "Agregar cartas"
  ├─ GET /games                        → elegir juego
  ├─ GET /expansions?game_id={id}      → elegir expansión
  ├─ POST /scrydex/{code}/cards        → buscar por nombre + filtros
  ├─ (selección múltiple en el diálogo)
  └─ POST /admin/cards/import          → guarda todas las seleccionadas

GET /catalog/cards                     → catálogo consumido por la tienda
```

---

## Endpoints eliminados (legacy)

| Endpoint eliminado                  | Reemplazo                          |
|-------------------------------------|------------------------------------|
| `POST /admin/pokemon/cards/import`  | búsqueda + `POST /admin/cards/import` |
| `POST /admin/magic/cards/import`    | búsqueda + `POST /admin/cards/import` |
| `GET /cards` (no existía en backend)| `GET /catalog/cards`               |

## Catálogo DB

```
GET /catalog/cards?game_code=&expansion_id=&name=&rarity=&type=&page=&page_size=
```

Acepta los mismos parámetros de búsqueda (más paginación) y es público
(lo consume la tienda y la vista admin "Catálogo DB").

## Seguridad

Todas las rutas `/scrydex/*` requieren **rol admin** (antes magic/riftbound
estaban sin proteger).
