# Análisis de inconsistencias — trample-back

> Revisión de arquitectura y calidad realizada sobre el código fuente (backend Go, arquitectura hexagonal).
> Fecha: 2026-08-13
>
> **Estado:** los ítems 1–13 y 15 ya están aplicados. Los marcados
> **(recomendación)** (14, 16–23) siguen abiertos como backlog de mejora;
> revisarlos antes de descartar este documento.

## Resumen

Se detectaron **23 inconsistencias** clasificadas por severidad:

| Severidad | Cantidad |
|-----------|----------|
| Alta      | 5        |
| Media     | 10       |
| Baja      | 8        |

La mayoría se corrigió en este mismo cambio. Las que quedaron como recomendación se marcan con **(recomendación)** y no implican cambios en este commit.

---

## Inconsistencias de severidad ALTA

### 1. El registro mapea cualquier error a `409 "email already registered"`
- **Archivo:** `internal/adapters/in/http/auth_handler.go:67-78`
- **Problema:** el `default` del `switch` devuelve 409 para **cualquier** error que no sea de validación. Una caída de la base de datos, un timeout o cualquier error interno se reportaría al cliente como "email ya registrado".
- **Solución aplicada:**
  - Nuevo error de dominio `ErrEmailTaken` en `internal/domain/auth/user.go`.
  - El repositorio (`user_repository_pg.go`) mapea la violación de unicidad `23505` de PostgreSQL a `ErrEmailTaken`.
  - El handler mapea: `ErrEmailTaken` → `409`, errores de validación → `422`, cualquier otro → `500` con el error logueado (nunca expuesto al cliente).

### 2. Registro deja estado inconsistente si falla la emisión del token
- **Archivo:** `internal/adapters/in/http/auth_handler.go:81-88`
- **Problema:** si `login.Execute` falla tras crear el usuario, el cliente recibe un `500` pero la cuenta **ya quedó creada**. Un reintento del cliente devuelve luego `409` (email ya registrado). Además se hacía una segunda consulta a la DB + comparación bcrypt innecesaria.
- **Solución aplicada:**
  - Se extrajo `LoginUseCase.IssueToken(ctx, user)` que solo firma el JWT a partir del usuario ya creado (sin re-consultar credenciales).
  - El handler de registro usa `IssueToken` sobre el usuario recién creado.

### 3. Roles de usuario inconsistentes (`cliente` vs `customer` vs `admin`)
- **Archivos:** `internal/application/auth/register.go:52`, `docs/schema.sql:18`
- **Problema:** el código registra usuarios con rol `"cliente"`, el `DEFAULT` del esquema es `'customer'` y el middleware valida `"admin"`. Tres convenciones distintas para el mismo concepto.
- **Solución aplicada:**
  - Se normalizó el rol de registro a `"customer"` (en inglés, como `admin`), y se actualizó `docs/schema.sql`.
  - Se define una constante `RoleCustomer` en el dominio para evitar repetir el literal.

### 4. Contrato JSON inconsistente: claves PascalCase vs snake_case
- **Archivo:** `internal/adapters/in/http/auth_handler.go:16-22`
- **Problema:** la respuesta de `register`/`login` devuelve claves `ID`, `FirstName`, `LastName`, mientras que el resto de la API usa snake_case (`external_id`, `first_name`, `expansion_code`, etc.). Esto rompe la convención del contrato y obliga al frontend a manejar dos estilos.
- **Solución aplicada:**
  - Claves normalizadas a snake_case: `id`, `first_name`, `last_name`, `email`, `role`.
  - Se actualizaron las anotaciones Swagger del handler.
  - **Nota:** es un cambio de contrato; el frontend debe consumir `id`/`first_name`/`last_name`.

### 5. Mensajes de error mezclan idiomas
- **Archivos:** `auth_handler.go`, `validate.go`, `pokemon_handler.go`, `search.go`
- **Problema:** unos endpoints responden en inglés ("invalid body", "invalid email", "email already registered") y otros en español ("body JSON inválido", "name es requerido", "game_code y name son requeridos").
- **Solución aplicada:**
  - Mensajes orientados al usuario estandarizados en español (mercado objetivo del proyecto). Los errores técnicos se loguean y no se exponen.

---

## Inconsistencias de severidad MEDIA

### 6. Manejo de errores inconsistente entre paquetes (`panic`/`os.Exit` vs errores)
- **Archivos:** `pkg/config/config.go:31-35`, `pkg/db/db.go:18,24`, `cmd/api/main.go`
- **Problema:** `config.Load()` devuelve error pero `require()` hace `panic`; `db.Connect` hace `os.Exit(1)` internamente. El llamador no tiene control sobre el arranque.
- **Solución aplicada:**
  - `require` ahora devuelve `(string, error)`, `Load` reporta el error correctamente.
  - `db.Connect` devuelve `(*pgxpool.Pool, error)`.
  - `main.go` maneja ambos errores logueándolos antes de salir.

### 7. El cliente TRM sostiene el mutex durante una llamada HTTP
- **Archivo:** `internal/adapters/out/trm/client.go:32-46`
- **Problema:** todos los requests de búsqueda quedan serializados detrás del `Lock()` mientras se llama a la API externa (hasta 5s de timeout).
- **Solución aplicada:**
  - Lock solo para lectura/escritura de la caché; la llamada HTTP ocurre fuera del mutex (patrón *double-checked locking*).

### 8. Filtro "Pokémon Pocket" aplicado a todos los juegos
- **Archivo:** `internal/adapters/out/scrydex/client.go:320-334`
- **Problema:** `buildQuery` agrega `-expansion.series:"Pokémon Pocket"` siempre, incluso para `mtg` y `riftbound`, donde el filtro no tiene sentido y puede alterar resultados.
- **Solución aplicada:** el parámetro `gameCode` se propaga a `buildQuery` y el filtro solo se aplica a `pokemon`.

### 9. Swagger de `ListExpansions` declara el tipo incorrecto
- **Archivo:** `internal/adapters/in/http/pokemon_handler.go:110`
- **Problema:** anotación `{array} catalog.Card`, pero el endpoint devuelve `[]catalog.Expansion`.
- **Solución aplicada:** corregida la anotación a `{array} catalog.Expansion`.

### 10. JWT sin claim `iat`
- **Archivo:** `internal/application/auth/login.go:52-60`
- **Problema:** el token no incluye `iat` (issued at), útil para auditoría y control de expiración.
- **Solución aplicada:** se agregó `iat`.

### 11. `.gitignore` incompleto: artefactos de build/log sucios en git status
- **Problema:** `trample-api.exe` (32 MB) y `api.log` aparecían como untracked.
- **Solución aplicada:** se agregaron `*.exe`, `*.log`, `bin/`, `dist/` a `.gitignore`.

### 12. `gofmt` reporta todos los archivos por CRLF
- **Problema:** con `core.autocrlf=true` en Windows los archivos se editan con CRLF y `gofmt -l` los marca todos. Sin `.gitattributes`, cada desarrollador/CI puede tener resultados distintos.
- **Solución aplicada:**
  - Se agregó `.gitattributes` (`*.go text eol=lf`) para forzar LF en Go.
  - Se normalizaron todos los archivos Go con `gofmt -w`.

### 13. `docs/schema.sql` solo documenta la tabla `users`
- **Problema:** las tablas `games`, `expansions`, `cards`, `pokemon_card_details`, `card_images`, `card_variants`, `variant_prices` no están versionadas; cualquier cambio en Supabase queda invisible para el repo.
- **Solución:** se actualizó el comentario de rol. **(recomendación)** versionar el esquema completo o moverlo a migraciones versionadas (golang-migrate/atlas).

### 14. `strconv.Atoi(card.HP)` ignora el error
- **Archivo:** `internal/adapters/out/postgres/card_repository_pg.go:75`
- **Solución:** **(recomendación)** decidir explícitamente el valor por defecto o propagar el error si un HP inválido debe fallar la importación.

### 15. Registro hace doble trabajo (crear + login completo)
- **Problema:** superpuesto con el punto 2; se resuelve con `IssueToken`.

---

## Inconsistencias de severidad BAJA (recomendaciones)

| # | Hallazgo | Recomendación |
|---|----------|---------------|
| 16 | `Decode` (respond.go) no limita el tamaño del body ni rechaza trailing data | Usar `http.MaxBytesReader` y `decoder.DisallowUnknownFields()` |
| 17 | `FetchOne` usa `POST` con body para "get by id" | Usar `GET /cards/{id}?variants=...` |
| 18 | Endpoints `/scrydex/*` públicos sin rate limit (solo `/auth`) | Extender `httprate` a las rutas públicas de búsqueda |
| 19 | `@host localhost:8080` hardcodeado en Swagger | Parametrizar por entorno |
| 20 | `magic_handler.go` y `riftbound_handler.go` casi idénticos | Unificar en un handler parametrizado por `gameCode` |
| 21 | `applyTRM` hace fallar toda la búsqueda si la TRM cae | Degradar a solo USD si TRM no disponible |
| 22 | `docs/` generados con swag commiteados (pueden quedar stale) | Regenerar en CI o ignorar los archivos generados |
| 23 | `README.md` solo tiene el título | Documentar setup, endpoints y despliegue |

---

## Cambios aplicados en este commit

- `internal/domain/auth/user.go` — error `ErrEmailTaken` + constante `RoleCustomer`.
- `internal/application/auth/register.go` — rol normalizado + pre-check de email duplicado.
- `internal/application/auth/login.go` — `IssueToken` + claim `iat`.
- `internal/application/auth/validate.go` — mensajes en español.
- `internal/adapters/in/http/auth_handler.go` — snake_case, mapeo de errores correcto, `IssueToken`.
- `internal/adapters/out/postgres/user_repository_pg.go` — mapeo `23505` → `ErrEmailTaken`.
- `internal/adapters/out/trm/client.go` — concurrencia de caché corregida.
- `internal/adapters/out/scrydex/client.go` — filtro Pocket solo para pokemon.
- `internal/adapters/in/http/pokemon_handler.go` — anotación Swagger corregida.
- `pkg/config/config.go`, `pkg/db/db.go`, `cmd/api/main.go` — errores en vez de `panic`/`os.Exit` interno.
- `.gitignore` + `.gitattributes` nuevos; todos los `.go` normalizados con `gofmt -w`.
- `docs/schema.sql` — rol `customer` alineado.
