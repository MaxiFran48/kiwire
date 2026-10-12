# Trabajo Práctico: Capa de Transporte

Implementación de un modelo Cliente-Servidor mediante Sockets TCP en Go.

## Características y Servicios Cubiertos
- **Tipo de Servicio**: Orientado a la conexión (TCP), garantizando entrega confiable, en orden y libre de duplicados.
- **Gestión de Conexión**: Establecimiento explícito mediante Three-Way Handshake (`net.Dial` y `net.Listen/Accept`) y cierre ordenado de canal (`QUIT` y `Close()`).
- **Concurrencia**: El servidor maneja múltiples clientes simultáneos mediante goroutines y sincronización de estado (`sync.Mutex`).
- **Informe de Estado e Información**: Comandos `TIME` (hora del servidor), `STATS` (métricas de conexiones) y `USERS` (clientes activos).
- **Interfaz de Usuario**: CLI interactiva por consola.

## Ejecución

1. Iniciar el servidor:
   ```bash
   go run server/main.go