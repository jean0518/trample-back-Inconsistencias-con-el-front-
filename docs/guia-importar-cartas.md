# Guía: Importar cartas desde el frontend

Flujo de 4 pasos: elegir juego → elegir expansión → buscar carta → guardar en DB.

---

## Paso 1 — Listar juegos

Muestra al usuario los juegos disponibles para que elija uno.

**Request**
```
GET /games
```

**Response**
```json
[
  { "id": 1, "code": "pokemon", "name": "Pokémon" },
  { "id": 2, "code": "mtg",     "name": "Magic: The Gathering" }
]
```

Guardá el `id` y el `code` del juego seleccionado — los vas a usar en los pasos siguientes.

---

## Paso 2 — Listar expansiones del juego

Con el `id` del juego elegido, traé sus expansiones para mostrarlas en un selector.

**Request**
```
GET /expansions?game_id={id}
```

**Ejemplo**
```
GET /expansions?game_id=1
```

**Response**
```json
[
  {
    "ID": 42,
    "GameID": 1,
    "ExternalID": "sv8",
    "Name": "Surging Sparks",
    "Code": "SSP",
    "ReleasedAt": "2024-11-08"
  },
  ...
]
```

> Si omitís `game_id` devuelve todas las expansiones de todos los juegos.

Guardá el `ExternalID` de la expansión seleccionada — es el código que Scrydex usa para filtrar (`expansion_code`).

---

## Paso 3 — Buscar carta en Scrydex

Con el juego y la expansión elegidos, buscá la carta por nombre.  
Este endpoint requiere **token de admin**.

**Request**
```
POST /scrydex/{code}/cards
Authorization: Bearer <token>
Content-Type: application/json
```

Reemplazá `{code}` con el `code` del juego (`pokemon`, `mtg`, `riftbound`).

**Body**
```json
{
  "name": "char",
  "expansion_code": "sv8"
}
```

| Campo            | Requerido | Descripción                                      |
|------------------|-----------|--------------------------------------------------|
| `name`           | Sí        | Nombre parcial — busca por prefijo (ej: "char")  |
| `expansion_code` | No        | `ExternalID` de la expansión elegida en paso 2   |
| `rarity`         | No        | Rareza exacta (ej: "Rare Holo")                  |
| `type`           | No        | Tipo de carta (solo Pokémon, ej: "Fire")         |

**Response**
```json
{
  "search_id": "search_e0d18c57ac78df66",
  "total": 3,
  "cards": [
    {
      "external_id": "sv8-54",
      "name": "Charizard ex",
      "rarity": "Double Rare",
      "expansion": { "name": "Surging Sparks", "code": "SSP" },
      "variants": [
        {
          "name": "holofoil",
          "nm_price": {
            "market_usd": 12.50,
            "market_cop": 52000
          }
        }
      ],
      "images": [...]
    }
  ]
}
```

Mostrá las cartas al usuario para que elija cuáles guardar.  
Guardá el `search_id` y los `external_id` de las cartas seleccionadas.

---

## Paso 4 — Guardar cartas seleccionadas en la DB

Con el `search_id` del paso anterior y los `external_id` de las cartas elegidas, guardalas en la base de datos.  
Este endpoint requiere **token de admin**.

**Request**
```
POST /admin/cards/import
Authorization: Bearer <token>
Content-Type: application/json
```

**Body**
```json
{
  "search_id": "search_e0d18c57ac78df66",
  "external_ids": ["sv8-54", "sv8-55"]
}
```

**Response**
```json
{
  "imported": 2,
  "cards": [...]
}
```

> El `search_id` expira a los **15 minutos**. Si el usuario tarda más, hay que repetir el paso 3.

---

## Resumen del flujo

```
GET  /games
  └─ id, code del juego seleccionado

GET  /expansions?game_id={id}
  └─ ExternalID de la expansión seleccionada

POST /scrydex/{code}/cards          (requiere admin)
  └─ search_id + lista de cartas

POST /admin/cards/import             (requiere admin)
  └─ cartas guardadas en DB
```

---

## Endpoints de sincronización (admin)

Antes de que las expansiones aparezcan en el paso 2, un admin debe haberlas sincronizado al menos una vez:

| Juego   | Endpoint                                  |
|---------|-------------------------------------------|
| Pokémon | `POST /admin/pokemon/expansions/sync`     |
| Magic   | `POST /admin/magic/expansions/sync`       |
