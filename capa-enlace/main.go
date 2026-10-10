// Programa principal del trabajo practico de capa de enlace de datos.
//
// Arma la capa de enlace (Stop-and-Wait con banderas, bit stuffing, CRC-16 y
// acuses de recibo) sobre el medio de transmision que se elija, y la hace
// trabajar.
//
// Hay tres modos:
//
//	-modo=demo      las dos puntas en la misma maquina, con traza completa.
//	                Sirve para ver como el protocolo resuelve los errores.
//	-modo=emisor    manda el contenido de un archivo al otro extremo.
//	-modo=receptor  escucha y guarda en un archivo lo que le llega.
//
// El medio fisico se elige con -medio=simulado|tcp|serie. Los tres implementan
// la misma interfaz, asi que el protocolo de enlace es exactamente el mismo en
// los tres casos.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	enlazecapa "utn/capa-enlace/src"
)

// Valores por defecto de los parametros de linea de comandos.
const (
	modoPorDefecto      = "demo"
	medioPorDefecto     = "simulado"
	direccionPorDefecto = "127.0.0.1:9000"
	puertoSerieDefecto  = "/dev/ttyUSB0"
	baudiosPorDefecto   = 9600

	// silencioReceptor son los segundos que el receptor espera sin recibir nada
	// antes de dar la transferencia por terminada.
	silencioReceptor = 5 * time.Second
)

func main() {
	cfg := leerConfiguracion()
	registro := enlazecapa.NuevoRegistro(cfg.Verboso)

	// Se imprime la configuracion para que la traza se entienda sola.
	registro.Info("PROGRAMA", "modo=%s medio=%s reintentos=%d temporizador=%s verboso=%t",
		cfg.Modo, cfg.Medio, cfg.Enlace.MaxIntentos, cfg.Enlace.TiempoEspera, cfg.Verboso)

	var codigo int
	switch cfg.Modo {
	case "demo":
		codigo = ejecutarDemo(cfg, registro)
	case "emisor":
		codigo = ejecutarEmisor(cfg, registro)
	case "receptor":
		codigo = ejecutarReceptor(cfg, registro)
	default:
		fmt.Fprintf(os.Stderr, "modo desconocido: %q (use demo, emisor o receptor)\n", cfg.Modo)
		codigo = 2
	}
	os.Exit(codigo)
}

// configuracion guarda los parametros tomados de la linea de comandos.
type configuracion struct {
	Modo      string
	Medio     string
	Direccion string
	Puerto    string
	Archivo   string
	Salida    string
	Baudios   int
	Verboso   bool
	Enlace    enlazecapa.Configuracion
}

// leerConfiguracion define los parametros de linea de comandos y arma la
// configuracion del protocolo de enlace.
func leerConfiguracion() configuracion {
	// Se parte de la configuracion por defecto del paquete de la capa de enlace
	// y se pisa con lo que venga en la linea de comandos.
	enlace := enlazecapa.ConfiguracionPorDefecto()
	var cfg configuracion

	flag.StringVar(&cfg.Modo, "modo", modoPorDefecto, "demo, emisor o receptor")
	flag.StringVar(&cfg.Medio, "medio", medioPorDefecto, "simulado, tcp o serie")
	flag.StringVar(&cfg.Direccion, "direccion", direccionPorDefecto, "direccion TCP con el otro extremo: host:puerto")
	flag.StringVar(&cfg.Puerto, "puerto", puertoSerieDefecto, "dispositivo serie, por ejemplo /dev/ttyUSB0")
	flag.StringVar(&cfg.Archivo, "datos", "datos.txt", "archivo con los datos que manda el emisor")
	flag.StringVar(&cfg.Salida, "salida", "recibidos.txt", "archivo donde el receptor guarda lo que recibio")
	flag.IntVar(&cfg.Baudios, "baudios", baudiosPorDefecto, "velocidad del puerto serie")
	flag.BoolVar(&cfg.Verboso, "verboso", false, "muestra el detalle de bits y bytes de cada trama")

	// Parametros del protocolo de enlace.
	flag.IntVar(&enlace.MaxIntentos, "reintentos", enlace.MaxIntentos, "intentos maximos por trama")
	flag.DurationVar(&enlace.TiempoEspera, "espera", enlace.TiempoEspera, "temporizador del emisor")

	// Parametros del medio simulado.
	flag.Int64Var(&enlace.Semilla, "semilla", enlace.Semilla, "semilla del azar del medio simulado")
	flag.Float64Var(&enlace.ProbErrorBit, "prob-error", enlace.ProbErrorBit, "probabilidad de error por bit")
	flag.Float64Var(&enlace.ProbPerdidaMarco, "prob-perdida", enlace.ProbPerdidaMarco, "probabilidad de perder una trama")
	flag.Float64Var(&enlace.ProbDuplicadoMarco, "prob-duplicado", enlace.ProbDuplicadoMarco, "probabilidad de duplicar una trama")
	flag.DurationVar(&enlace.RetardoMedio, "retardo", enlace.RetardoMedio, "retardo del medio simulado")

	flag.Parse()

	enlace.Verboso = cfg.Verboso
	cfg.Enlace = enlace
	return cfg
}

// ---------------------------------------------------------------------------
// Armado del medio fisico
//
// Los tres medios implementan la misma interfaz, asi que desde aca para abajo
// el protocolo no se entera de en que esta corriendo. Un unico detalle: en el
// modo demo hacen falta las dos puntas del enlace (una envia y la otra recibe),
// y en los otros modos hace falta una sola punta, porque la otra esta en la otra
// maquina o en el otro equipo.
// ---------------------------------------------------------------------------

// crearParDeMedios arma los dos extremos del enlace, para el modo demo.
func crearParDeMedios(cfg configuracion, registro *enlazecapa.Registro) (enlazecapa.InterfazCapaFisica, enlazecapa.InterfazCapaFisica, error) {
	switch cfg.Medio {
	case "simulado":
		extremoA, extremoB := enlazecapa.NuevoParMedioSimulado(cfg.Enlace, registro)
		return extremoA, extremoB, nil

	case "tcp":
		extremoA, extremoB, err := enlazecapa.NuevoParMedioTCP(cfg.Direccion, registro)
		if err != nil {
			return nil, nil, err
		}
		return extremoA, extremoB, nil

	case "serie":
		// Un puerto serie es un cable entre dos equipos, no dos puntas dentro
		// de la misma maquina. Para verlo hay que correr dos procesos, como se
		// explica en el README.
		return nil, nil, fmt.Errorf("el modo demo no se puede usar con el medio serie: " +
			"correr dos procesos con -modo=emisor y -modo=receptor, como se explica en el README")
	}
	return nil, nil, fmt.Errorf("medio desconocido: %q (use simulado, tcp o serie)", cfg.Medio)
}

// crearMedioEmisor arma el extremo que manda.
func crearMedioEmisor(cfg configuracion, registro *enlazecapa.Registro) (enlazecapa.InterfazCapaFisica, error) {
	switch cfg.Medio {
	case "tcp":
		return enlazecapa.NuevoMedioTCPCliente(cfg.Direccion, registro)
	case "serie":
		return enlazecapa.NuevoMedioSerie(cfg.Puerto, cfg.Baudios, registro)
	}
	return nil, fmt.Errorf("el modo emisor necesita -medio=tcp o -medio=serie, no %q", cfg.Medio)
}

// crearMedioReceptor arma el extremo que escucha.
func crearMedioReceptor(cfg configuracion, registro *enlazecapa.Registro) (enlazecapa.InterfazCapaFisica, error) {
	switch cfg.Medio {
	case "tcp":
		return enlazecapa.NuevoMedioTCPServidor(cfg.Direccion, registro)
	case "serie":
		return enlazecapa.NuevoMedioSerie(cfg.Puerto, cfg.Baudios, registro)
	}
	return nil, fmt.Errorf("el modo receptor necesita -medio=tcp o -medio=serie, no %q", cfg.Medio)
}

// ---------------------------------------------------------------------------
// Modo demo: las dos puntas en la misma maquina
// ---------------------------------------------------------------------------

// ejecutarDemo corre el enlace completo de punta a punta sobre un medio
// simulado o sobre un par de sockets TCP locales, y al final compara lo enviado
// con lo recibido.
func ejecutarDemo(cfg configuracion, registro *enlazecapa.Registro) int {
	// --- 1) Se arma el medio con los dos extremos ---
	medioA, medioB, err := crearParDeMedios(cfg, registro)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer medioA.Cerrar()
	defer medioB.Cerrar()
	registro.Info("PROGRAMA", "medio de la punta A: %s", medioA.Descripcion())
	registro.Info("PROGRAMA", "medio de la punta B: %s", medioB.Descripcion())

	// --- 2) Cada punta arma su capa de enlace ---
	nodoA := enlazecapa.NuevoEnlace(medioA, cfg.Enlace, registro)
	nodoB := enlazecapa.NuevoEnlace(medioB, cfg.Enlace, registro)

	// En este modo la punta A solo manda y la punta B solo recibe, asi que
	// alcanza con poner en marcha el receptor de B. Cada proceso hace un solo
	// rol: si los dos roles estuvieran en el mismo proceso, el emisor y el
	// receptor competirian por leer el mismo medio y se masticarian los acuses.
	go nodoB.Receptor.Ejecutar()

	// --- 3) Datos a mandar: el archivo si existe, si no un ejemplo ---
	datos, origen := datosDeLaDemo(cfg)
	registro.Info("PROGRAMA", "a transferir: %d bytes desde %s", len(datos), origen)

	// --- 4) La capa de red de A manda, y la de B recibe ---
	registro.Info("CAPA-RED", "A entrega %d bytes al enlace", len(datos))
	err = nodoA.Transfiere(datos)

	// Se juntan las tramas que fueron llegando a la capa de red de B.
	recibido := juntarLoRecibido(nodoB, len(datos))

	// --- 5) Resultado ---
	registro.Info("PROGRAMA", "estado final: envio=%v", err)
	imprimirResumen(nodoA, nodoB, recibido, datos)

	if err == nil && len(recibido) == len(datos) {
		registro.Info("PROGRAMA", "OK: la capa de red recibio todos los datos, en orden y sin repetirse")
		return 0
	}
	registro.Aviso("PROGRAMA", "FALLO: se enviaron %d bytes y llegaron %d", len(datos), len(recibido))
	return 1
}

// datosDeLaDemo devuelve los bytes a transferir: el contenido del archivo
// indicado si existe, y si no un bloque de ejemplo que incluye una tanda de
// 0xFF a proposito, porque 0xFF es el byte que mas unos seguidos genera y es el
// que hace visible el bit stuffing en la traza.
func datosDeLaDemo(cfg configuracion) ([]byte, string) {
	if contenido, err := os.ReadFile(cfg.Archivo); err == nil {
		return contenido, cfg.Archivo
	}
	// El mensaje se repite para que entren varias tramas: asi se ve el numero
	// de secuencia alternando entre 0 y 1, que es lo que permite reconocer
	// duplicados.
	parrafo := "Hola, capa de enlace de datos. " +
		"Estas lineas viajan divididas en tramas, con banderas, stuffing y CRC.\n"
	// La tanda de 0xFF: seis unos seguidos, que es justo lo que la bandera
	// 01111110 empieza con.
	parrafo += strings.Repeat(string([]byte{0xFF}), 40) + "\n"
	parrafo = strings.Repeat(parrafo, 30)
	return []byte(parrafo), "ejemplo generado"
}

// juntarLoRecibido espera a que la capa de red del nodo B reciba todos los
// bytes esperados, o a que se agote la espera de la ultima trama.
func juntarLoRecibido(nodoB *enlazecapa.EnlaceDeNodo, esperados int) []byte {
	var recibido []byte
	ultimoDato := time.Now()
	for len(recibido) < esperados {
		if datos, hayDatos := nodoB.Recibir(); hayDatos {
			recibido = append(recibido, datos...)
			ultimoDato = time.Now()
			continue
		}
		// No hay nada todavia: se espera un poco y se vuelve a preguntar, hasta
		// que pasa el silencio acordado.
		if time.Since(ultimoDato) > silencioReceptor {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return recibido
}

// imprimirResumen muestra las estadisticas del enlace y la comparacion entre lo
// enviado y lo recibido.
func imprimirResumen(nodoA, nodoB *enlazecapa.EnlaceDeNodo, recibido, enviado []byte) {
	enviados, reintentos, perdidas := nodoA.Emisor.Estadisticas()
	aceptadas, danadas, duplicadas := nodoB.Receptor.Estadisticas()

	fmt.Println()
	fmt.Println("================ RESUMEN DEL ENLACE ================")
	fmt.Printf("Tramas de datos entregadas a la capa de red : %d\n", enviados)
	fmt.Printf("Reintentos por temporalizador o por NAK     : %d\n", reintentos)
	fmt.Printf("Tramas que se perdieron definitivamente      : %d\n", perdidas)
	fmt.Printf("Tramas aceptadas por el receptor            : %d\n", aceptadas)
	fmt.Printf("Tramas detectadas como danadas (CRC/_flags)  : %d\n", danadas)
	fmt.Printf("Tramas descartadas por duplicadas           : %d\n", duplicadas)
	fmt.Printf("Bytes enviados / recibidos                   : %d / %d\n", len(enviado), len(recibido))
	if bytes.Equal(enviado, recibido) {
		fmt.Println("Contenido recibido: IDENTICO al enviado")
	} else {
		fmt.Println("Contenido recibido: DISTINTO al enviado")
	}
	fmt.Println("===================================================")
}

// ---------------------------------------------------------------------------
// Modo emisor: una maquina manda
// ---------------------------------------------------------------------------

// ejecutarEmisor manda el contenido de un archivo al otro extremo y espera el
// acuse de cada trama.
func ejecutarEmisor(cfg configuracion, registro *enlazecapa.Registro) int {
	contenido, err := os.ReadFile(cfg.Archivo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: no se pudo leer %s: %v\n", cfg.Archivo, err)
		return 1
	}
	registro.Info("PROGRAMA", "a enviar: %d bytes desde %s", len(contenido), cfg.Archivo)

	medio, err := crearMedioEmisor(cfg, registro)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer medio.Cerrar()

	nodo := enlazecapa.NuevoEnlace(medio, cfg.Enlace, registro)
	// Este proceso hace de emisor, asi que no se pone en marcha el receptor
	// local: si esta escuchando, se comeria los acuses del otro extremo. Lo que
	// se espera en este modo son tramas de datos propias para mandar.

	registro.Info("CAPA-RED", "entrega %d bytes al enlace", len(contenido))
	if err := nodo.Transfiere(contenido); err != nil {
		registro.Error("PROGRAMA", "la transferencia fallo: %v", err)
		return 1
	}
	enviados, reintentos, perdidas := nodo.Emisor.Estadisticas()
	registro.Info("PROGRAMA", "transferencia completa: %d tramas, %d reintentos, %d perdidas",
		enviados, reintentos, perdidas)
	return 0
}

// ---------------------------------------------------------------------------
// Modo receptor: una maquina escucha
// ---------------------------------------------------------------------------

// ejecutarReceptor espera a que llegue la transferencia completa y la guarda en
// un archivo. Termina cuando pasan unos segundos sin recibir nada.
func ejecutarReceptor(cfg configuracion, registro *enlazecapa.Registro) int {
	medio, err := crearMedioReceptor(cfg, registro)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer medio.Cerrar()

	nodo := enlazecapa.NuevoEnlace(medio, cfg.Enlace, registro)
	// El receptor corre en segundo plano y va dejando las tramas validas en la
	// cola; este hilo solo las va juntando.
	go nodo.Receptor.Ejecutar()

	registro.Info("PROGRAMA", "esperando datos por %s", medio.Descripcion())
	var recibido []byte
	ultimoDato := time.Now()
	for {
		if datos, hayDatos := nodo.Recibir(); hayDatos {
			recibido = append(recibido, datos...)
			ultimoDato = time.Now()
			registro.Info("CAPA-RED", "acumulados %d bytes", len(recibido))
			continue
		}
		if time.Since(ultimoDato) > silencioReceptor {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := os.WriteFile(cfg.Salida, recibido, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error: no se pudo escribir %s: %v\n", cfg.Salida, err)
		return 1
	}
	_, _, duplicadas := nodo.Receptor.Estadisticas()
	registro.Info("PROGRAMA", "recibidos %d bytes, guardados en %s (duplicadas descartadas: %d)",
		len(recibido), cfg.Salida, duplicadas)
	return 0
}
