# Guía: Catálogo público (`GET /catalog/cards`)

> **Importante:** desde la Fase 4 el catálogo se alimenta del **inventario**.
> Solo aparecen cartas con al menos un listing `active` con `quantity > 0`.
> La respuesta incluye el `stock` agregado por carta. La antigua vista admin
> "Catálogo DB" fue eliminada; este endpoint es el que consume la tienda.

Endpoint único con filtros opcionales y paginación. No requiere autenticación.

---

## Endpoint

```
GET /catalog/cards
```

---

## Query parameters

| Parámetro      | Tipo   | Requerido | Descripción                                      |
|----------------|--------|-----------|--------------------------------------------------|
| `game_code`    | string | No        | Filtra por juego: `pokemon`, `mtg`, `riftbound`  |
| `expansion_id` | int    | No        | ID interno de la expansión (viene de `/expansions`) |
| `name`         | string | No        | Búsqueda parcial por nombre (ej: `"char"`)       |
| `rarity`       | string | No        | Rareza exacta (ej: `"Double Rare"`)              |
| `type`         | string | No        | Tipo de carta (ej: `"Fire"`)                     |
| `page`         | int    | No        | Número de página, default `1`                    |
| `page_size`    | int    | No        | Resultados por página, default `20`, max `100`   |

---

## Ejemplos de uso

**Todas las cartas con stock (primera página)**
```
GET /catalog/cards
```

**Solo cartas de Pokémon**
```
GET /catalog/cards?game_code=pokemon
```

**Cartas de una expansión específica**
```
GET /catalog/cards?game_code=pokemon&expansion_id=42
```

**Buscar por nombre y rareza dentro de una expansión**
```
GET /catalog/cards?game_code=pokemon&expansion_id=42&name=char&rarity=Double%20Rare
```

**Paginar**
```
GET /catalog/cards?game_code=pokemon&page=2&page_size=50
```

---

## Response

```json
{
  "page": 1,
  "page_size": 20,
  "total": 150,
  "cards": [
    {
      "id": 7,
      "external_id": "sv8-54",
      "name": "Charizard ex",
      "number": "54",
      "rarity": "Double Rare",
      "game_code": "pokemon",
      "expansion": {
        "id": 42,
        "name": "Surging Sparks",
        "code": "SSP",
        "logo_url": "https://...",
        "symbol_url": "https://..."
      },
      "image": {
        "small": "https://...",
        "medium": "https://...",
        "large": "https://..."
      },
      "variants": [
        { "name": "Normal", "price_usd": 1.5, "price_cop": 6000 }
      ],
      "stock": 8
    }
  ]
}
```

| Campo              | Descripción                                              |
|--------------------|----------------------------------------------------------|
| `total`            | Total de cartas que coinciden con los filtros            |
| `page` / `page_size` | Página actual y tamaño de página usados               |
| `cards`            | Array de cartas (vacío `[]` si no hay resultados)        |
| `id`               | ID interno de la carta en la DB                          |
| `external_id`      | ID en Scrydex                                            |
| `expansion.id`     | Usalo como `expansion_id` en futuras consultas filtradas |
| `variants`         | Variantes publicadas con precio USD y COP                |
| `stock`            | Suma de cantidades de listings activos de la carta       |

> Una carta sin listings activos **no aparece** en la respuesta. Al llegar el
> stock a 0 (vía `PATCH /listings/{id}`) desaparece del catálogo hasta que
> vuelva a haber inventario.

---

## Flujo recomendado en el frontend

```
1. GET /games
      └─ elegir juego → guardar code e id

2. GET /expansions?game_id={id}
      └─ elegir expansión → guardar expansion.ID

3. GET /catalog/cards?game_code={code}&expansion_id={id}&name={busqueda}&page=1&page_size=20
      └─ mostrar cartas con paginación
```

### Paginación

Usá `total` y `page_size` para calcular el total de páginas en el frontend:

```js
const totalPages = Math.ceil(total / page_size)
```

Para ir a la siguiente página, incrementá `page` en 1 y repetí el request con los mismos filtros.
