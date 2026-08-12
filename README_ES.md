[English](README.md) | **Español**

# FLASH-BLIP Leaderboard API

Servidor backend para la recepción, gestión, validación de replays y almacenamiento de high scores del juego [FLASH-BLIP](https://github.com/plinkr/flash-blip).

Desarrollado a modo de proyecto personal (hobbie), actualmente se utiliza activamente como la API backend del leaderboard del juego. Su objetivo principal es servir como base extensible para que otros desarrolladores puedan implementar su propio sistema de leaderboard, adaptando simplemente el validador de replays y el motor de simulación a las mecánicas de sus propios juegos.

Está escrito en Go utilizando el framework Fiber v2 y persiste datos en PostgreSQL. Cuenta con un pipeline de seguridad en dos fases (handshake HMAC) y un motor de simulación de física asíncrono para verificar la integridad de los *replays* de juego antes de confirmarlos en la tabla de posiciones.

## Características Clave

- **Handshake Challenge-Response**: Protocolo de envío de puntuación en dos pasos mediante tokens UUID temporales de un solo uso y firmas HMAC-SHA256.
- **Validación Anti-Cheat de Replays**:
  - *Fase síncrona (Light)*: Análisis inmediato de ordenamiento de ticks, límites de frecuencia de eventos (BLIPs/PINGs por minuto) y validación de cabeceras binarias.
  - *Fase asíncrona (Simulación de Física)*: Re-ejecución determinista de la partida (física de salto `playerY`, bonos de altura, multiplicadores y curva de dificultad) para candidatos a entrar en el Top N.
- **Soporte de Replays V1 y V2**: Compatible con el formato binario legado V1 y la especificación V2 con cabecera de validación (`FBRP`) y eventos explícitos de multiplicador.
- **Compresión LZ4**: Descompresión del flujo de eventos generado por el cliente en LOVE2D.
- **Filtrado por User-Agent**: Restricción configurable de clientes autorizados (por defecto `LuaSocket 3.1.0`).
- **Limpieza de Nonces y Mantenimiento**: Deduplicación de peticiones y purga automática en segundo plano de tokens y nonces caducados.
- **Moderación Comunitaria**: Sistema de reportes de puntuaciones con auto-invalidación al alcanzar el umbral configurado.

## Requisitos Previos

- **Go**: Versión 1.26 o superior.
- **PostgreSQL**: Versión 15 o superior.
- **Entorno de Desarrollo**: Desarrollado y probado nativamente en Linux.
- **Docker y Docker Compose** *(Opcional)*: Para despliegue en contenedores.

## Configuración y Variables de Entorno

El servidor lee las variables de entorno desde el sistema o desde un archivo `.env` en la raíz del proyecto. Puedes tomar como referencia `.env.example` e `internal/config/config.go`.

| Variable | Descripción | Valor por Defecto |
| :--- | :--- | :--- |
| `PORT` | Puerto de escucha del servidor HTTP | `8080` (`.env.example`) |
| `DATABASE_URL` | Cadena de conexión PostgreSQL | `postgres://postgres:postgres@localhost:5432/flash-blip-leaderboard?sslmode=disable` |
| `RATE_LIMIT_RPM` | Límite de peticiones por minuto por IP en endpoints críticos | `5` |
| `LEADERBOARD_DEPTH` | Cantidad de posiciones del Top a validar y mostrar | `100` |
| `ALLOWED_USER_AGENTS` | Lista separada por comas de User-Agents autorizados | `LuaSocket 3.1.0` |
| `ALLOWED_ORIGINS` | Orígenes permitidos para CORS | `*` |
| `TIMESTAMP_SKEW_SECONDS` | Margen de desvío aceptado para marcas de tiempo | `60` |

## Instalación y Compilación desde Cero

1. Clonar el repositorio y acceder al directorio del proyecto:
   ```bash
   git clone https://github.com/plinkr/flash-blip-leaderboard-api
   cd flash-blip-leaderboard-api
   ```

2. Descargar e instalar las dependencias de Go:
   ```bash
   go mod download
   ```

3. Compilar el ejecutable para la arquitectura de destino:

   - **Linux x86-64**:
     ```bash
     GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o flash-blip-leaderboard-api-backend.bin ./cmd/server/main.go
     ```

   - **Raspberry Pi (ARM64 / armv8)**:
     ```bash
     GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o flash-blip-leaderboard-api-backend-rpi.bin ./cmd/server/main.go
     ```

4. Ejecutar las migraciones e iniciar el servidor:
   ```bash
   ./flash-blip-leaderboard-api-backend.bin
   ```
   *Nota: Al iniciar, el servidor ejecuta automáticamente las migraciones SQL presentes en `internal/db/migrations/001_init.sql`.*

## Despliegue con Docker y Docker Compose

El repositorio incluye un `Dockerfile` en dos etapas (compilación estática sobre `golang:alpine` y ejecución sobre `scratch`) y un archivo `docker-compose.yml` que levanta la base de datos PostgreSQL 15 junto a la aplicación.

Para iniciar toda la infraestructura con Docker:

```bash
docker compose up --build -d
```

Esto ejecutará:
- Un contenedor PostgreSQL (`leaderboard_db`) en el puerto `5432` con comprobación de salud (*healthcheck*).
- El contenedor de la API backend (`leaderboard_app`) en el puerto `8080` una vez que la base de datos esté lista.

Para detener los servicios:
```bash
docker compose down
```

## Arquitectura de Seguridad

El envío de puntuaciones no se realiza mediante una simple petición HTTP POST directa, sino que requiere un protocolo de validación:

```
[Cliente Juego]                      [Servidor Backend]                     [PostgreSQL]
       |                                      |                                  |
       |--- 1. POST /scores/prepare --------->|                                  |
       |    (Metadatos: score, ticks, etc)    | Genera Token (30s) y Nonce       |
       |<--- Devuelve { token, nonce } -------|                                  |
       |                                      |                                  |
       | Comprime Replay (LZ4)                |                                  |
       | Firma HMAC-SHA256(Body, key=Nonce)   |                                  |
       |                                      |                                  |
       |--- 2. POST /scores/submit ---------->|                                  |
       |    Headers: X-Submit-Token,          | Verifica Token + Firma HMAC      |
       |             X-Signature              | Consume Token (uso único)        |
       |                                      | Descomprime LZ4 y Parser (V1/V2) |
       |                                      | Validación Light Síncrona        |
       |                                      |--- Guarda Score + Replay ------->|
       |<--- Devuelve { ok, score_id } -------| (Score en estado Pending)        |
       |                                      |                                  |
       |                                      | [Goroutine Asíncrona]            |
       |                                      | ¿Está dentro del Top N?          |
       |                                      | Simula Replay (Física + Ticks)   |
       |                                      | Compara Claimed vs Simulated     |
       |                                      |--- Marca Validated True/False -->|
```

### Protocolo de Envío de Puntuación
1. **Preparación (`POST /scores/prepare`)**: El cliente envía los metadatos de la partida (`p`, `s`, `total_ticks`, `replay_version`, `rng_seed`, `difficulty`). El servidor almacena temporalmente la sesión en memoria y devuelve un `token` único (UUID) y un `nonce` criptográfico con validez de 30 segundos.
2. **Firma y Envío (`POST /scores/submit`)**: El cliente antepone un prefijo de 4 bytes (little-endian) con el tamaño descomprimido, comprime el flujo binario de entradas con LZ4 (formato de bloques LOVE2D) y codifica el resultado en Base64. A continuación, calcula la firma HMAC-SHA256 utilizando el `nonce` como clave secreta sobre el cuerpo Base64 y realiza el envío con las cabeceras `X-Submit-Token` y `X-Signature`.
3. **Consumo de Token**: El servidor valida el token, verifica la firma HMAC en tiempo constante y marca el token como consumido para prevenir reataques (*replay attacks*) o condiciones de carrera.

### Niveles de Validación de Replays
- **Validación Síncrona (Light)**: Descomprime el flujo LZ4 y analiza la estructura del replay (`ParseReplay`). Comprueba la ordenación monotónica de los ticks, que el número de entradas no sobrepase los límites de frecuencia humana (máximo de BLIPs/PINGs por minuto) y, en V2, valida las secuencias de multiplicador y la cabecera binaria `FBRP`.
- **Validación Asíncrona (Simulación de Física)**: Si la puntuación es candidata a ingresar en el Top N (`LEADERBOARD_DEPTH`), se ejecuta de forma asíncrona `validator.SimulateReplay`:
  - Simula la trayectoria vertical del jugador (`playerY`), la velocidad de desplazamiento (*scroll*), la acumulación de bonos de altura y la progresión de la dificultad.
  - Genera cotas dinámicas de tolerancia (`ToleranceLow` y `ToleranceHigh`).
  - Si la puntuación declarada se encuentra dentro del rango simulado, la entrada se marca como `validated = true`. De lo contrario, se rechaza (`validated = false`) almacenando el motivo.
- **Barredor de Replays Pendientes (*Sweeper*)**: Un proceso periódico (`RunPendingReplaySweeper`) revisa entradas pendientes no validadas que califiquen para el Top N para procesarlas en segundo plano.
- **Moderación Comunitaria**: El endpoint `POST /scores/:id/report` permite a los usuarios reportar puntuaciones. Al acumular 3 o más reportes, el servidor invalida la puntuación automáticamente.

## Endpoints de la API

### Públicos
- `GET /health`: Estado de salud del servidor.
- `GET /scores`: Retorna las mejores puntuaciones validadas dentro del Top N.
- `GET /scores/check/:score` | `POST /scores/check`: Comprueba si una puntuación calificaría para entrar al Top N (acepta el valor por parámetro en la URL, parámetro de consulta `?score=` o cuerpo JSON `{"score": 1234}`).
- `GET /replays/:id`: Descarga los datos binarios del replay comprimido junto con sus metadatos en las cabeceras HTTP (`X-Replay-Seed`, `X-Replay-Version`, `X-Replay-Ticks`, `X-Replay-Difficulty`).

### Envío de Puntuaciones (Protegido)
- `POST /scores/prepare`: Inicia el handshake y genera el token/nonce de sesión.
- `POST /scores/submit`: Envía el replay firmado para su procesamiento.

### Moderación
- `POST /scores/:id/report`: Registra un reporte sobre una puntuación.

## Licencia

Este proyecto se distribuye bajo la licencia **MIT**. Consulta el archivo [LICENSE](LICENSE) para más información.
