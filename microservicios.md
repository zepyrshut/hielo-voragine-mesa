# Tiempo real, resiliencia y microservicios

Notas de estudio: WebSockets frente a POST + SSE, qué profundizar más allá del transporte, graceful shutdown, cómo montar un laboratorio de microservicios en local y qué proyecto construir (con foco en banca).

## Índice

1. [WebSockets por dentro](#1-websockets-por-dentro)
2. [POST + SSE frente a WebSockets](#2-post--sse-frente-a-websockets)
3. [Qué profundizar de verdad](#3-qué-profundizar-de-verdad)
4. [Graceful shutdown](#4-graceful-shutdown)
5. [Cómo usar todo esto en entrevistas](#5-cómo-usar-todo-esto-en-entrevistas)
6. [Laboratorio de microservicios en local](#6-laboratorio-de-microservicios-en-local)
7. [Proyecto: venta de entradas para conciertos](#7-proyecto-venta-de-entradas-para-conciertos)
8. [Proyecto: sistema bancario (recomendado)](#8-proyecto-sistema-bancario-recomendado)

---

## 1. WebSockets por dentro

### Handshake: empieza siendo HTTP

El cliente abre una conexión TCP normal y manda un GET con cabeceras especiales:

```http
GET /ws HTTP/1.1
Host: api.ejemplo.com
Upgrade: websocket
Connection: Upgrade
Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==
Sec-WebSocket-Version: 13
Origin: https://app.ejemplo.com
```

El servidor contesta:

```http
HTTP/1.1 101 Switching Protocols
Upgrade: websocket
Connection: Upgrade
Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=
```

- `Sec-WebSocket-Accept = base64(SHA1(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))`. **No es seguridad**: solo prueba que el servidor entiende WebSocket y no es un proxy devolviendo una respuesta cacheada.
- Tras el `101`, **HTTP desaparece**. El mismo socket TCP habla el protocolo de frames de WebSocket (RFC 6455) en ambas direcciones.

### Frames

Ya no hay requests ni responses, solo frames:

```
FIN | RSV | opcode(4) | MASK | payload len (7/16/64 bits) | [masking key 4B] | payload
```

- **opcode**: `0x1` texto (UTF-8), `0x2` binario, `0x0` continuación, `0x8` close, `0x9` ping, `0xA` pong.
- **FIN**: un mensaje puede ir fragmentado en varios frames.
- **Masking**: los frames cliente → servidor van obligatoriamente enmascarados con XOR y una clave aleatoria. Sirve para no envenenar cachés de proxies intermedios; no es cifrado. El cifrado lo da `wss://` (TLS).
- **Overhead**: de 2 a 14 bytes por mensaje, frente a los cientos de bytes de cabeceras de cada POST.

### Control: ping/pong y close

- **Ping/Pong**: heartbeat a nivel de protocolo. Detecta conexiones muertas (TCP puede tardar minutos en enterarse) y evita que los load balancers corten por inactividad.
- **Close**: cierre con código (`1000` normal, `1001` going away, `1008` policy violation, `1011` error interno…). Ambas partes confirman.

### Lo que WebSocket NO te da

- **Reconexión automática**: hay que implementarla, con backoff.
- **Semántica de mensajes**: no hay rutas, status codes, request IDs ni acks. Es un tubo con delimitación de mensajes; el protocolo de aplicación lo diseñas tú.
- **Reanudación**: no existe un equivalente a `Last-Event-ID` de SSE.
- **Backpressure en el navegador**: el `WebSocket` del browser no la tiene (solo `bufferedAmount`). `WebSocketStream` aún no está en todos los navegadores.

---

## 2. POST + SSE frente a WebSockets

### La dinámica POST + SSE

```
cliente ──POST /acciones──▶ servidor      (cliente → servidor)
cliente ◀──── SSE stream ── servidor      (servidor → cliente)
```

SSE es una respuesta HTTP que nunca termina, con `Content-Type: text/event-stream`:

```
id: 42
event: pedido_actualizado
data: {"id":7,"estado":"listo"}

```

Lo que SSE da gratis:

- `EventSource` reconecta solo y envía `Last-Event-ID`, así que el servidor puede reenviar lo perdido.
- Es HTTP puro: cookies, auth, CORS, middlewares, logs, proxies y CDNs funcionan sin hacer nada.
- Con HTTP/2 o HTTP/3, las conexiones SSE y los POST se multiplexan sobre una sola conexión TCP. El límite de 6 conexiones por dominio de HTTP/1.1 deja de aplicar.
- El canal cliente → servidor sigue siendo HTTP completo: status codes, validación, idempotencia, rate limiting por endpoint, reintentos.

### Comparativa

| Aspecto | POST + SSE | WebSocket |
|---|---|---|
| Latencia cliente → servidor | Un request HTTP por mensaje. Con HTTP/2 y keep-alive es bajo, pero no cero | Un frame sobre un socket abierto: casi nada |
| Overhead por mensaje | Cientos de bytes de cabeceras por POST | 2–14 bytes |
| Alta frecuencia (decenas o cientos de msgs/s por cliente) | Se nota | Gana claramente |
| Datos binarios | Solo texto (habría que usar base64) | Binario nativo (protobuf, msgpack, audio…) |
| Correlación envío/recepción | Dos canales: hay que correlacionar y pueden llegar desordenados | Un canal ordenado |
| Estado de sesión | Cada POST es stateless | La conexión *es* la sesión |
| Presencia / "está escribiendo…" | Incómodo | Natural |
| Reconexión y reanudación | Gratis (`Last-Event-ID`) | La implementas tú |
| Infraestructura | HTTP normal | Upgrade en proxies/LB, timeouts de inactividad, sticky sessions, firewalls corporativos |
| Escalado horizontal | Canal SSE stateful; los POST van a cualquier nodo | Todo stateful; pub/sub entre nodos |
| Observabilidad y seguridad | Logs, métricas, WAF y rate limit estándar | Una "request" que dura horas: métricas, auth por mensaje y rate limit son cosa tuya |
| Auth | Cookie o header en cada request | Solo en el handshake; el navegador no permite headers custom en `new WebSocket()`. La expiración del token a mitad de conexión es cosa tuya |

### Conclusión

Las ventajas reales de WebSocket son tres:

1. Mensajes cliente → servidor **frecuentes y baratos**: juegos, colaboración en tiempo real, chat con typing indicators, trading, telemetría.
2. **Binario nativo**.
3. **Un solo canal bidireccional ordenado**, con la conexión como sesión.

Si el cliente manda acciones esporádicas y el servidor empuja actualizaciones, **POST + SSE es probablemente la mejor opción**: menos código, menos infraestructura, mejor observabilidad y reconexión gratis. Las APIs de LLMs, por ejemplo, hacen streaming por SSE.

> **Regla práctica**: si el cliente envía más de unos pocos mensajes por segundo de forma sostenida, o necesitas binario, WebSocket. Si no, SSE.

### Si algún día quieres aprender WebSocket haciendo (Go)

1. Escribe el handshake a mano con `net/http` + `http.Hijacker`: calcula `Sec-WebSocket-Accept`, toma el `net.Conn` y parsea un frame desenmascarando el payload. En ~150 líneas entiendes el protocolo.
2. Pasa a una librería: `github.com/coder/websocket` (antes `nhooyr.io/websocket`, idiomática y con `context`) o `gorilla/websocket`.
3. Implementa lo que WS no trae: sobre de mensaje (`{"type", "id", "payload"}`) con acks, heartbeat (ping cada ~30 s), una goroutine lectora y otra escritora por conexión con canal de salida con buffer, reconexión con backoff exponencial y jitter, reanudación por número de secuencia, y fan-out multi-nodo con Redis Pub/Sub o NATS.
4. Mira los frames en DevTools (Network → WS → Messages) o con Wireshark sobre `ws://`.

---

## 3. Qué profundizar de verdad

Lo difícil del tiempo real casi nunca es el transporte, sino lo que va por encima: garantías de entrega, consistencia y operación.

### 3.1 Garantías de entrega con SSE

- **Reanudación real con `Last-Event-ID`**: el servidor necesita un log de eventos con IDs monótonos (tabla en Postgres, Redis Streams o ring buffer en memoria). Si el ID pedido ya no está en el buffer, se envía un evento `resync` y el cliente recarga el estado completo.
- **At-most-once vs at-least-once**: con reanudación habrá duplicados, así que el cliente debe ser idempotente y descartar por `id`.
- **Idempotencia en los POST**: header `Idempotency-Key`. Si el cliente reintenta por timeout, la acción no se ejecuta dos veces.
- **Outbox pattern**: si el POST escribe en la DB y luego publica el evento, ¿qué pasa si el commit va bien y el publish falla? Solución: escribir el evento en una tabla `outbox` dentro de la misma transacción y que un worker lo publique.

**Experimento**: Toxiproxy entre cliente y servidor (latencia, cortes, conexiones medio abiertas) y comprobar que no se pierden ni duplican eventos visibles.

### 3.2 La carrera entre POST y SSE

Son dos canales, así que el orden no está garantizado:

- El evento SSE del propio cambio puede llegar **antes** que la respuesta del POST, o después.
- **Read-your-writes**: el POST responde OK pero el SSE aún no trae el cambio.
- **Solución típica**: versión por entidad (o secuencia global). El cliente solo aplica un cambio si `version > la que tengo`. Encaja muy bien con UI optimista.

### 3.3 Operar SSE en producción

- **Buffering de proxies**: nginx bufferiza por defecto y los eventos llegan en bloques. Se arregla con `X-Accel-Buffering: no` o `proxy_buffering off`. Ojo también con gzip.
- **Heartbeats**: comentarios `: ping\n\n` cada 15–30 s para que los load balancers no corten por inactividad.
- **Clientes lentos**: si `Write` bloquea en un cliente lento, no puede bloquear el fan-out del resto. Canal con buffer por cliente; si se llena, desconectar.
- **Graceful shutdown**: ver [sección 4](#4-graceful-shutdown).
- **Multi-nodo**: el POST cae en el nodo A y el cliente SSE está en el B. Hace falta pub/sub: Postgres `LISTEN/NOTIFY` para empezar (muy didáctico), luego Redis Pub/Sub o NATS.

### 3.4 Límites y carga

Con k6 o un cliente en Go, abrir 10k–50k conexiones SSE y medir memoria por conexión (goroutine + buffers), file descriptors (`ulimit -n`) y el tiempo que tarda un fan-out en llegar a todos. Así se entiende por qué Go aguanta tantas conexiones (netpoller sobre epoll) y dónde está el techo real.

### 3.5 La capa de abajo

- **HTTP/1.1 vs HTTP/2 vs HTTP/3**: multiplexación, head-of-line blocking en TCP y cómo lo resuelve QUIC. En DevTools se ve cómo SSE y los POST comparten conexión con HTTP/2.
- **WebTransport** (sobre HTTP/3): la alternativa moderna a WebSocket, con streams y datagramas no fiables.

---

## 4. Graceful shutdown

### Por qué importa tanto con SSE

Con HTTP normal los requests duran milisegundos: dejas de aceptar conexiones, esperas un par de segundos y sales.

Con SSE **las conexiones nunca terminan solas**. Y en Go hay una trampa: `http.Server.Shutdown()` espera a que las conexiones activas terminen, pero **no cancela el contexto de los handlers**. Un handler SSE en un `for { select ... }` infinito deja a `Shutdown` esperando para siempre, o hasta su timeout.

### Qué pasa si no se hace bien

1. **Muerte forzada**: llega `SIGTERM`, el proceso no termina y a los ~30 s llega `SIGKILL`.
   - Los clientes reciben un corte TCP abrupto (RST), no un cierre limpio.
   - Se pierden los eventos que estaban en buffers o canales sin escribir.
   - Los POST en curso se cortan a mitad. Sin transacciones u outbox, el estado puede quedar inconsistente (DB escrita, evento no emitido).
2. **Thundering herd**: 10.000 clientes conectados al nodo se desconectan en el mismo milisegundo, y `EventSource` reconecta a los ~3 s por defecto, todos a la vez.
   - Pico de conexiones contra los nodos que quedan.
   - 10.000 replays simultáneos por `Last-Event-ID`, todos golpeando la DB a la vez.
   - En un rolling deploy se repite **por cada pod**. Hay sistemas que se caen *durante el deploy*, no por el código nuevo.
3. **El load balancer sigue enviando tráfico**: en Kubernetes, sacar el pod de los endpoints y enviar `SIGTERM` ocurre en paralelo, así que durante unos segundos llegan conexiones nuevas a un proceso que se está muriendo.

### Secuencia correcta

1. `SIGTERM` → readiness a **503** y **seguir sirviendo** unos segundos para que el LB deje de enviar tráfico.
2. Avisar a los handlers SSE: a cada cliente se le manda un `retry:` **aleatorio** para repartir las reconexiones, y se cierra limpio.
3. `srv.Shutdown()` con timeout para que terminen los POST en curso.
4. Cerrar pub/sub, DB, etc.

```go
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	var ready atomic.Bool
	ready.Store(true)
	shutdownCh := make(chan struct{}) // señal para los handlers SSE

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz/ready", func(w http.ResponseWriter, r *http.Request) {
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/events", sseHandler(shutdownCh))

	srv := &http.Server{Addr: ":8080", Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()

	ready.Store(false)          // 1. el LB deja de mandarnos tráfico
	time.Sleep(5 * time.Second) //    margen para que se entere

	close(shutdownCh) // 2. los handlers SSE se despiden

	sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil { // 3. esperar a los POST en curso
		log.Printf("shutdown: %v", err)
	}
	// 4. cerrar pub/sub, DB...
}
```

Bucle del handler SSE:

```go
for {
	select {
	case ev := <-sub:
		fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.ID, ev.Data)
		flusher.Flush()
	case <-r.Context().Done(): // el cliente se fue
		return
	case <-shutdownCh: // nos apagamos: reconecta en 1–10 s, repartido
		fmt.Fprintf(w, "retry: %d\n\n", 1000+rand.IntN(9000)) // math/rand/v2
		flusher.Flush()
		return
	}
}
```

> **Ojo**: si respondes **204**, `EventSource` deja de reconectar para siempre. En un shutdown simplemente se sale del handler; el 204 es para decir "no vuelvas".

### Experimento

1. Levantar el servidor y abrir ~1.000 clientes SSE (un script en Go con goroutines).
2. `kill -9` → histograma de reconexiones: un pico enorme en un instante.
3. `kill -TERM` con `retry` aleatorio → reconexiones repartidas a lo largo de 10 s.

---

## 5. Cómo usar todo esto en entrevistas

Las entrevistas suelen preguntar lo que cabe en una pizarra (patrones, arquitectura, interfaces). Los temas operativos rara vez se preguntan porque solo los conoce quien ha visto caerse algo en producción, y por eso mismo diferencian.

Además, **esto también es microservicios**: rolling deploys, autoscaling y reinicio de pods solo funcionan si cada servicio sabe morir bien.

**Estrategia**: no esperar a que lo pregunten. En un system design, al dibujar un servicio con SSE o un worker de cola, añadir en 20 segundos algo como: *"en el deploy: readiness a 503, drenar conexiones y retry con jitter para evitar thundering herd"*. Pasas de "conoce patrones" a "ha operado sistemas reales".

Temas del mismo tipo:

- **Idempotencia y reintentos**: ¿qué pasa si el cliente reintenta un pago?
- **Timeouts encadenados**: si A → B → C, el timeout de A debe ser mayor que el de B. En Go, `context` lo propaga.
- **Backpressure**: consumidor más lento que el productor.
- **Outbox pattern**: escribir en DB y publicar un evento de forma consistente.
- **Retry storms**: un servicio cae, todos reintentan a la vez y lo rematan. Backoff, jitter, circuit breakers.

Todos comparten patrón: el sistema funciona en el caso feliz y se rompe en las **transiciones** (deploys, cortes, picos). Entender las transiciones es lo que hace bueno a alguien en sistemas distribuidos.

---

## 6. Laboratorio de microservicios en local

Se puede hacer todo en un solo equipo. Un servicio en Go en reposo consume ~10–30 MB de RAM: 5–6 servicios más Postgres, un broker y observabilidad caben en un portátil normal.

### Herramientas

| Herramienta | Para qué |
|---|---|
| **Docker Compose** (o `podman compose` en Fedora) | Orquestar todo. Cada servicio tiene su IP y DNS interno (`http://orders:8080`), como si fueran máquinas distintas. Empezar aquí. |
| **Toxiproxy** | Fallos de red controlados por API: latencia, cortes, conexiones medio abiertas. |
| **Docker directamente** | Fallos de proceso (ver abajo). |
| **k3d / kind** | Kubernetes local para rolling deploys, readiness probes, `terminationGracePeriodSeconds` y autoscaling. Arranca en ~20 s. |
| **OpenTelemetry + Jaeger** | Trazas distribuidas: ver por qué un request tardó 3 s. |
| **Prometheus + Grafana** | Métricas: conexiones SSE, latencias, errores. |
| **k6** | Generar carga. |

### Toxiproxy

```yaml
services:
  toxiproxy:
    image: ghcr.io/shopify/toxiproxy:2.9.0
    ports: ["8474:8474"]          # API de control

  api:
    build: ./api
    environment:
      ORDERS_URL: http://toxiproxy:21000   # api → toxiproxy → orders

  orders:
    build: ./orders
```

```bash
# crear el proxy api → orders
toxiproxy-cli create -l 0.0.0.0:21000 -u orders:8080 orders
# añadir 2 s de latencia
toxiproxy-cli toxic add -t latency -a latency=2000 orders
# cortar la conexión del todo
toxiproxy-cli toggle orders
```

### Fallos de proceso con Docker

- `docker kill -s SIGKILL orders` → crash.
- `docker stop orders` → `SIGTERM`, para probar el graceful shutdown.
- `docker pause orders` → proceso congelado con la conexión abierta. El peor caso: comprueba si los timeouts funcionan.
- `cpus: "0.2"` y `mem_limit: 64m` en el compose → servicio lento o que muere por OOM.

### Lo que no se simula bien en un solo equipo

- **Contención de recursos**: todos comparten CPU y disco. Los límites de CPU lo mitigan.
- **Particiones de red reales y asimétricas**: Toxiproxy y `tc netem` lo aproximan.
- **Desfase de reloj**: posible con `libfaketime`, pero más laborioso.

Para aprender resiliencia, timeouts, idempotencia, shutdown y consistencia es secundario: el 95 % de lo interesante se reproduce en local.

### Escenario genérico de práctica

```
navegador ──POST/SSE──▶ api ──HTTP──▶ orders ──HTTP──▶ payments
                         ▲              │
                         └──── NATS ◀───┘  (eventos → fan-out SSE)
                                Postgres (outbox, event log)
```

1. Latencia en `payments`: ¿se propagan los timeouts con `context`?
2. `payments` cae a mitad de un pago y `orders` reintenta: ¿cobro doble? → idempotencia.
3. `docker kill` a NATS justo tras el commit en `orders`: ¿se pierde el evento? → outbox.
4. `docker pause` a `orders`: ¿cuánto tarda `api` en enterarse? ¿Se acumulan goroutines?
5. Dos réplicas de `api` y `docker stop` de una con 1.000 clientes SSE → thundering herd frente a retry con jitter.
6. Todos reintentan contra `payments` caído → retry storm; arreglarlo con backoff y circuit breaker.

**Método**: romperlo, verlo fallar en Jaeger o Grafana, arreglarlo y repetir.

---

## 7. Proyecto: venta de entradas para conciertos

"Diseña Ticketmaster" es una pregunta clásica de system design, y cada servicio tiene un motivo real para estar separado: escalan y fallan de formas distintas.

```
                    ┌──────────────┐
navegador ─POST/SSE─▶   gateway    │  auth, POST de acciones, SSE de estado
                    └──────┬───────┘
          ┌────────────────┼─────────────────┐
          ▼                ▼                 ▼
  ┌──────────────┐  ┌─────────────┐  ┌──────────────┐
  │ waiting-room │  │   orders    │  │  inventory   │
  │ cola virtual │  │ orquesta la │──▶ asientos,    │
  │ posición SSE │  │   saga      │  │ holds con TTL│
  └──────────────┘  └──────┬──────┘  └──────────────┘
                           ▼
                    ┌─────────────┐  webhooks   ┌───────────────┐
                    │  payments   │────────────▶│ notifications │
                    │ (fake Stripe│   NATS      │ (consumidor)  │
                    │ con fallos) │             └───────────────┘
                    └─────────────┘
```

| Servicio | Problema real |
|---|---|
| **waiting-room** | 50.000 usuarios en un segundo. Se admiten poco a poco y reciben su posición por SSE. Recibe el pico, así que escala distinto. |
| **inventory** | Nunca vender el mismo asiento dos veces. Holds de 10 min que caducan solos. `UPDATE ... WHERE estado='libre'`, `SELECT ... FOR UPDATE SKIP LOCKED`. |
| **orders** | Saga: reservar → cobrar → confirmar. Si falla el pago, compensar liberando el asiento. ¿Y si el servicio muere a mitad? |
| **payments** | Proveedor falso con latencia y errores configurables, `Idempotency-Key` y confirmación por webhook asíncrono (duplicado, desordenado o perdido). Como Stripe. |
| **notifications** | Consumidor at-least-once que deduplica para no enviar el email dos veces. |

Cada servicio tiene su propio schema en Postgres y ninguno lee tablas ajenas.

**Fases**: (1) camino feliz; (2) 1.000 compras simultáneas del mismo asiento con k6; (3) fallos en `payments` y en mitad de la saga; (4) venta masiva con 10.000 clientes SSE y deploy en mitad; (5) observabilidad: explicar con Jaeger por qué *esa* compra tardó 4 s.

> No partir de más: 4–5 servicios bastan. Saber cuándo una separación **no** merece la pena también es un aprendizaje (y luce en entrevistas).

---

## 8. Proyecto: sistema bancario (recomendado)

Es donde más demanda hay y donde más se nota todo lo anterior: un evento duplicado es una molestia, una transferencia duplicada es dinero real. Más adelante se puede unir con el proyecto de conciertos: el `payments` de la ticketera pasa a ser este banco.

### Conceptos que un entrevistador de banca quiere oír

- **Libro mayor de partida doble (double-entry ledger)**: cada movimiento son al menos dos apuntes que suman cero. El dinero no se crea ni se destruye, solo se mueve.
- **Append-only**: en el ledger nunca hay `UPDATE` ni `DELETE`. Los errores se corrigen con apuntes de reversión. El historial es la auditoría.
- **Dinero como entero**: céntimos en `int64` (o `NUMERIC` en Postgres). **Nunca `float64`**.
- **Saldo contable vs saldo disponible**: retenciones (holds), como la autorización de una tarjeta antes de la liquidación.
- **Exactly-once efectivo** = entrega at-least-once + procesamiento idempotente.
- **Sagas en lugar de 2PC** entre servicios; **consistencia fuerte** dentro del ledger.
- **Reconciliación**: comparar el ledger con lo que dice el otro banco o la cámara de compensación. Proceso diario y crítico en banca real.
- **Vocabulario**: SEPA (SCT, SCT Inst), ISO 20022 (`pacs.008`), PSD2/SCA, cut-off times, devoluciones.

### Arquitectura

```
                     ┌──────────────┐
app ──POST/SSE──────▶   gateway    │  auth, idempotency-key, SSE de movimientos
                     └──────┬───────┘
           ┌────────────────┼──────────────────┐
           ▼                ▼                  ▼
   ┌──────────────┐  ┌──────────────┐   ┌─────────────┐
   │   accounts   │  │  transfers   │──▶│    fraud    │
   │ clientes,    │  │ orquesta la  │   │ scoring con │
   │ cuentas, KYC │  │    saga      │   │  timeout    │
   └──────────────┘  └──────┬───────┘   └─────────────┘
                            ▼
                     ┌──────────────┐        ┌──────────────────┐
                     │    ledger    │◀──────▶│ interbank (fake  │
                     │ partida doble│  NATS  │ SEPA, async,     │
                     │ append-only  │        │ con devoluciones)│
                     └──────┬───────┘        └──────────────────┘
                            ▼
                     ┌──────────────┐
                     │reconciliation│  job: ledger vs informes del "otro banco"
                     └──────────────┘
```

| Servicio | Qué enseña |
|---|---|
| **ledger** | El corazón. Partida doble, append-only, saldo nunca negativo con 100 cargos concurrentes (`SELECT ... FOR UPDATE` o locking optimista con versión). Es un event store natural: event sourcing de verdad. |
| **transfers** | Saga: validar → fraude → retener → ejecutar → liquidar. Compensaciones e idempotencia de punta a punta. |
| **fraud** | Llamada síncrona con timeout. Pregunta clave: si no responde, ¿**fail closed** (rechazar) o **fail open** (dejar pasar)? Hay que argumentarlo. |
| **interbank** | Simula otro banco o SEPA: respuestas asíncronas, duplicados y devoluciones días después que obligan a compensar algo ya "terminado". |
| **reconciliation** | Detecta discrepancias entre el ledger y el informe externo. Casi nadie lo menciona en entrevistas: punto diferencial. |
| **gateway** | SSE de movimientos en tiempo real y del estado de cada transferencia. |

### Experimentos

1. **Doble clic en "Enviar" y reintento por timeout** → con idempotency key, un solo cargo.
2. **100 transferencias concurrentes** desde una cuenta con saldo para 10 → pasan exactamente 10 y el saldo nunca es negativo.
3. **`docker kill` a `transfers` entre cargo y abono** → el dinero no puede quedar en el limbo (outbox y recuperación de la saga).
4. **Conservación del dinero**: un checker que, mientras corre el caos, verifica continuamente que la suma de todas las cuentas del sistema es constante. Si cambia, hay un bug. Es el test más bancario de todos.
5. **`fraud` con 3 s de latencia** → decidir e implementar fail open o fail closed.
6. **El otro banco devuelve una transferencia 48 h después** → reversión en el ledger y aviso al cliente.
7. **Discrepancia inyectada en el informe interbancario** → reconciliation la detecta.
8. **Deploy en mitad de transferencias** → graceful shutdown con dinero en vuelo.

### Por dónde empezar: fase 1, solo el ledger

Es lo que más se pregunta y lo que más enseña. Un servicio en Go con Postgres (pgx) que:

- abra cuentas,
- haga transferencias internas con partida doble,
- garantice saldo no negativo bajo concurrencia,
- incluya idempotencia en la API,
- y tenga el test de conservación del dinero con k6 golpeándolo.

Cuando aguante, añadir `transfers`, `fraud` e `interbank` alrededor.
