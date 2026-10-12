# Trabajo Práctico: Capa de Transporte

Implementación de una arquitectura Cliente-Servidor mediante **Sockets TCP** en Go, cumpliendo con los lineamientos de servicios de transporte orientados a la conexión.

## Requisitos Previos
* **Go (Golang)**: versión 1.20 o superior instalada.

---

## Estructura del Proyecto

* `server/main.go`: Servidor concurrente TCP. Gestiona el ciclo de vida de las conexiones y el estado del sistema en memoria protegida por mutex.
* `client/main.go`: Cliente de consola interactivo (CLI) para envío y recepción de datos.
* `go.mod`: Módulo de dependencias del proyecto.

---

## Instrucciones de Ejecución

Desde la terminal, situarse en el directorio del trabajo práctico:

```bash
cd capa-transporte
```

### Paso 1: Iniciar el Servidor
En una primera terminal, levantar el proceso de escucha pasiva:

```bash
go run server/main.go
```
*El servidor quedará a la espera de conexiones en el puerto local `:8080`.*

### Paso 2: Iniciar el Cliente
En una **segunda terminal** (también dentro de `capa-transporte`), iniciar el cliente:

```bash
go run client/main.go
```
> *Nota:* Opcionalmente se puede especificar un host/puerto destino pasando un argumento: `go run client/main.go 127.0.0.1:8080`. Se pueden abrir múltiples terminales de cliente en simultáneo para evaluar la concurrencia.

---

## Protocolo de Comandos Disponibles

Una vez establecida la sesión en el cliente (`transporte>`), se pueden enviar los siguientes comandos:

| Comando | Descripción |
| :--- | :--- |
| `TIME` | Devuelve la fecha y hora actual del servidor. |
| `STATS` | Muestra el tiempo activo (*uptime*), total de conexiones históricas y conexiones actuales. |
| `USERS` | Lista las direcciones de todos los clientes conectados activamente. |
| `ECHO <mensaje>` | Prueba de transmisión de datos (el servidor replica el texto enviado). |
| `HELP` | Lista los comandos disponibles. |
| `QUIT` / `EXIT` | Cierra formalmente la conexión y finaliza la sesión del cliente. |

---

## Mapeo con las Consignas Teóricas

* **Servicio orientado a la conexión:** Implementado sobre TCP (`net.Dial` y `net.Listen`), asegurando entrega ordenada, fiable y control de flujo punto a punto.
* **Gestión de conexión:** Establecimiento explícito mediante *handshake* de sockets y desconexión controlada de recursos con manejo de eventos de cierre (`QUIT` / EOF).
* **Concurrencia:** El servidor despacha cada conexión entrante en una *goroutine* independiente para evitar bloqueo entre clientes.
* **Informe de estado y transferencia:** Registro sincronizado en memoria (`sync.Mutex`) para exponer métricas en tiempo real (`TIME`, `STATS`, `USERS`).