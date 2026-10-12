package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

func main() {
	serverAddr := "127.0.0.1:8080"
	if len(os.Args) > 1 {
		serverAddr = os.Args[1]
	}

	fmt.Printf("[CLIENTE] Intentando conectar con el servidor en %s...\n", serverAddr)

	// 1. Establecimiento de la conexión de transporte (Handshake TCP)
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		fmt.Printf("Error: No se pudo conectar al servidor: %v\n", err)
		return
	}
	defer conn.Close()

	serverReader := bufio.NewReader(conn)
	serverWriter := bufio.NewWriter(conn)
	stdinReader := bufio.NewScanner(os.Stdin)

	// Leer mensaje de bienvenida
	welcome, err := serverReader.ReadString('\n')
	if err == nil {
		fmt.Print("[SERVIDOR]: ", welcome)
	}

	fmt.Println("---------------------------------------------------------")
	fmt.Println("Comandos disponibles: TIME | STATS | USERS | ECHO <msg> | QUIT")
	fmt.Println("---------------------------------------------------------")

	for {
		fmt.Print("transporte> ")
		if !stdinReader.Scan() {
			break
		}

		line := strings.TrimSpace(stdinReader.Text())
		if line == "" {
			continue
		}

		// Enviar comando al servidor por el socket
		_, err := serverWriter.WriteString(line + "\n")
		if err != nil {
			fmt.Printf("Error al transmitir datos: %v\n", err)
			break
		}
		serverWriter.Flush()

		// Esperar respuesta del servidor
		response, err := serverReader.ReadString('\n')
		if err != nil {
			fmt.Println("El servidor cerró la conexión.")
			break
		}

		fmt.Print("[SERVIDOR]: ", response)

		// Si el comando fue de salida, terminar la sesión
		cmd := strings.ToUpper(strings.Fields(line)[0])
		if cmd == "QUIT" || cmd == "EXIT" {
			break
		}
	}

	fmt.Println("[CLIENTE] Sesión finalizada.")
}