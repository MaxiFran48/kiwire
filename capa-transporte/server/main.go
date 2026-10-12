package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

type ServerState struct {
	mu             sync.Mutex
	totalConnected int
	activeClients  map[string]time.Time
	startTime      time.Time
}

var state = ServerState{
	activeClients: make(map[string]time.Time),
	startTime:     time.Now(),
}

func main() {
	puerto := ":8080"
	// 1. Creación del socket pasivo y apertura del puerto de escucha (TCP)
	listener, err := net.Listen("tcp", puerto)
	if err != nil {
		log.Fatalf("Error al iniciar el servidor en el puerto %s: %v", puerto, err)
	}
	defer listener.Close()

	log.Printf("[SERVIDOR TRANSPORTE] Escuchando en el puerto %s (TCP - Orientado a conexión)...\n", puerto)

	for {
		// 2. Gestión de conexión: Aceptación del handshake de transporte
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Error al aceptar conexión: %v", err)
			continue
		}

		// Registrar cliente conectado
		clientAddr := conn.RemoteAddr().String()
		state.mu.Lock()
		state.totalConnected++
		state.activeClients[clientAddr] = time.Now()
		total := state.totalConnected
		state.mu.Unlock()

		log.Printf("[+] Conexión establecida desde %s. Conexiones históricas: %d\n", clientAddr, total)

		// 3. Concurrencia: Atención en hilo/goroutine separado
		go handleClient(conn, clientAddr)
	}
}

func handleClient(conn net.Conn, clientAddr string) {
	defer func() {
		// Cierre ordenado de la conexión
		conn.Close()
		state.mu.Lock()
		delete(state.activeClients, clientAddr)
		state.mu.Unlock()
		log.Printf("[-] Conexión cerrada con %s\n", clientAddr)
	}()

	scanner := bufio.NewScanner(conn)
	writer := bufio.NewWriter(conn)

	sendResponse(writer, fmt.Sprintf("200 OK - Conectado al servidor de transporte UTN. Tu dirección: %s. Escribe HELP para ayuda.", clientAddr))

	for scanner.Scan() {
		rawInput := strings.TrimSpace(scanner.Text())
		if rawInput == "" {
			continue
		}

		parts := strings.SplitN(rawInput, " ", 2)
		cmd := strings.ToUpper(parts[0])
		arg := ""
		if len(parts) > 1 {
			arg = parts[1]
		}

		switch cmd {
		case "TIME":
			now := time.Now().Format("2006-01-02 15:04:05 MST")
			sendResponse(writer, fmt.Sprintf("TIME: %s", now))

		case "STATS":
			state.mu.Lock()
			uptime := time.Since(state.startTime).Truncate(time.Second)
			total := state.totalConnected
			activos := len(state.activeClients)
			state.mu.Unlock()
			sendResponse(writer, fmt.Sprintf("STATS: Total históricas: %d | Activas: %d | Uptime: %s", total, activos, uptime))

		case "USERS":
			state.mu.Lock()
			var users []string
			for addr, since := range state.activeClients {
				users = append(users, fmt.Sprintf("%s (conectado hace %s)", addr, time.Since(since).Truncate(time.Second)))
			}
			state.mu.Unlock()
			sendResponse(writer, fmt.Sprintf("USERS [%d]: %s", len(users), strings.Join(users, ", ")))

		case "ECHO":
			sendResponse(writer, fmt.Sprintf("ECHO: %s", arg))

		case "HELP":
			sendResponse(writer, "COMANDOS: TIME, STATS, USERS, ECHO <msg>, QUIT")

		case "QUIT", "EXIT":
			sendResponse(writer, "221 BYE - Conexión finalizada por el usuario.")
			return

		default:
			sendResponse(writer, "400 ERROR - Comando no reconocido. Escribe HELP.")
		}
	}
}

func sendResponse(w *bufio.Writer, msg string) {
	w.WriteString(msg + "\n")
	w.Flush()
}