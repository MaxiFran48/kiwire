package enlazecapa

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// MedioTCP lleva las tramas fisicas entre dos maquinas por una conexion TCP.
//
// A diferencia del medio simulado, este si es un medio real: no invierte bits ni
// pierde tramas por la red (TCP se encarga de eso), pero si tiene retardo
// variable, que es el retardo que hay que cubrir con el temporizador del
// emisor. Sirve para hacer la prueba de verdad entre dos equipos.
//
// Formato en el cable: cada trama viaja precedida por un entero de 4 bytes con
// su largo en bytes, y seguida por el largo exacto. Ese prefijo hace las veces
// que en un medio real cumple la bandera: le dice al otro extremo donde termino
// la trama.
const prefijoLargoEnCable = 4

// Errores propios del medio TCP.
var (
	// ErrConexionPerdida indica que el otro equipo cerro la conexion.
	ErrConexionPerdida = errors.New("se perdio la conexion con el otro equipo")
)

// MedioTCP implementa la capa fisica sobre un socket TCP. Solo se usa uno de
// los dos constructores segun el rol.
type MedioTCP struct {
	conexion net.Conn

	mutex   sync.Mutex
	cerrado bool

	descripcion string
	registro    *Registro
}

// NuevoMedioTCPServidor escucha en la direccion indicada y espera que se
// conecte el otro equipo. Es el que se usa en la maquina que hace de receptor
// (la que escucha).
func NuevoMedioTCPServidor(direccion string, registro *Registro) (*MedioTCP, error) {
	oyente, err := net.Listen("tcp", direccion)
	if err != nil {
		return nil, fmt.Errorf("no se pudo escuchar en %s: %w", direccion, err)
	}
	// Se espera a que llegue la conexion del otro equipo.
	conexion, err := oyente.Accept()
	oyente.Close()
	if err != nil {
		return nil, fmt.Errorf("no se pudo aceptar la conexion: %w", err)
	}
	if tcp, ok := conexion.(*net.TCPConn); ok {
		// Desactivamos el algoritmo de Nagle: en un protocolo de enlace cada
		// trama tiene que salir apenas se envia, no cuando se le conviene al
		// sistema operativo agrupar.
		tcp.SetNoDelay(true)
	}
	registro.Info(actorFisico, "conexion TCP aceptada desde %s", conexion.RemoteAddr())
	return &MedioTCP{
		conexion:    conexion,
		descripcion: "tcp(" + direccion + ")",
		registro:    registro,
	}, nil
}

// NuevoMedioTCPCliente se conecta al otro equipo. Es el que se usa en la maquina
// que hace de emisor.
func NuevoMedioTCPCliente(direccion string, registro *Registro) (*MedioTCP, error) {
	conexion, err := net.Dial("tcp", direccion)
	if err != nil {
		return nil, fmt.Errorf("no se pudo conectar a %s: %w", direccion, err)
	}
	if tcp, ok := conexion.(*net.TCPConn); ok {
		tcp.SetNoDelay(true)
	}
	registro.Info(actorFisico, "conexion TCP establecida con %s", conexion.RemoteAddr())
	return &MedioTCP{
		conexion:    conexion,
		descripcion: "tcp(" + direccion + ")",
		registro:    registro,
	}, nil
}

// NuevoParMedioTCP crea los dos extremos de un medio TCP entre las dos puntas
// de la misma maquina, de la misma forma que hace NuevoParMedioSimulado. Sirve
// para ejercitar el codigo de red real sin tener dos equipos a mano.
func NuevoParMedioTCP(direccion string, registro *Registro) (*MedioTCP, *MedioTCP, error) {
	oyente, err := net.Listen("tcp", direccion)
	if err != nil {
		return nil, nil, fmt.Errorf("no se pudo escuchar en %s: %w", direccion, err)
	}
	defer oyente.Close()

	// La conexion del cliente se hace en una goroutine porque Accept() se queda
	// esperando y el Dial() todavia no ocurrio.
	type resultado struct {
		conexion net.Conn
		err      error
	}
	canal := make(chan resultado, 1)
	go func() {
		conexion, err := net.Dial("tcp", direccion)
		canal <- resultado{conexion, err}
	}()

	aceptada, err := oyente.Accept()
	if err != nil {
		return nil, nil, fmt.Errorf("no se pudo aceptar la conexion: %w", err)
	}
	delCliente := <-canal
	if delCliente.err != nil {
		aceptada.Close()
		return nil, nil, fmt.Errorf("no se pudo conectar a %s: %w", direccion, delCliente.err)
	}

	nuevo := func(c net.Conn, descripcion string) *MedioTCP {
		if tcp, ok := c.(*net.TCPConn); ok {
			tcp.SetNoDelay(true)
		}
		return &MedioTCP{conexion: c, descripcion: descripcion, registro: registro}
	}
	return nuevo(aceptada, "tcp(servidor:"+direccion+")"),
		nuevo(delCliente.conexion, "tcp(cliente:"+direccion+")"), nil
}

// Transmitir envia la trama completa: primero el largo, despues los bytes.
func (m *MedioTCP) Transmitir(bits []byte) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.cerrado {
		return ErrMedioCerrado
	}

	// Se arma el mensaje con el prefijo de longitud delante de la trama.
	mensaje := make([]byte, prefijoLargoEnCable+len(bits))
	escribirEnteroGrande(mensaje[:prefijoLargoEnCable], len(bits))
	copy(mensaje[prefijoLargoEnCable:], bits)

	if _, err := m.conexion.Write(mensaje); err != nil {
		m.registro.Aviso(actorFisico, "fallo al escribir en el socket: %v", err)
		return fmt.Errorf("fallo al escribir en el socket: %w", err)
	}
	return nil
}

// Recibir lee una trama del socket. Si pasan los bytes del prefijo y no llegan
// todos los de la trama, se pierde la conexion: en un medio real no tiene sentido
// seguir.
func (m *MedioTCP) Recibir(espera time.Duration) ([]byte, error) {
	m.mutex.Lock()
	if m.cerrado {
		m.mutex.Unlock()
		return nil, ErrMedioCerrado
	}
	m.mutex.Unlock()

	// Si el llamador puso un plazo, el socket se lee con deadline. Si puso
	// cero, la lectura es bloqueante.
	if espera > 0 {
		m.conexion.SetReadDeadline(time.Now().Add(espera))
		defer m.conexion.SetReadDeadline(time.Time{})
	}

	prefijo := make([]byte, prefijoLargoEnCable)
	if _, err := io.ReadFull(m.conexion, prefijo); err != nil {
		return nil, traducirErrorLectura(err)
	}
	largo := leerEnteroGrande(prefijo)
	if largo <= 0 {
		return nil, ErrTramaVacia
	}
	bits := make([]byte, largo)
	if _, err := io.ReadFull(m.conexion, bits); err != nil {
		return nil, traducirErrorLectura(err)
	}
	return bits, nil
}

// Cerrar cierra el socket.
func (m *MedioTCP) Cerrar() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.cerrado {
		return nil
	}
	m.cerrado = true
	return m.conexion.Close()
}

// Descripcion devuelve el modo de transporte para la bitacora.
func (m *MedioTCP) Descripcion() string {
	return m.descripcion
}

// traducirErrorLectura distingue "se vencio el plazo" (que el enlace trata como
// trama perdida) de "se cayo la conexion" (que es un error de verdad).
func traducirErrorLectura(err error) error {
	if err == nil {
		return nil
	}
	// El timeout de un socket...
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ErrTiempoEspera
	}
	// ...y el timeout de un puerto serie, que no es un error de red.
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return ErrTiempoEspera
	}
	return fmt.Errorf("%w: %v", ErrConexionPerdida, err)
}

// escribirEnteroGrande escribe un entero de 4 bytes en orden de red (big endian).
func escribirEnteroGrande(destino []byte, valor int) {
	valor = valor & 0xFFFFFFFF
	destino[0] = byte(valor >> 24)
	destino[1] = byte(valor >> 16)
	destino[2] = byte(valor >> 8)
	destino[3] = byte(valor)
}

// leerEnteroGrande lee un entero de 4 bytes en orden de red.
func leerEnteroGrande(origen []byte) int {
	return int(origen[0])<<24 | int(origen[1])<<16 | int(origen[2])<<8 | int(origen[3])
}

// EstaCerrado dice si el medio ya se cerro.
func (m *MedioTCP) EstaCerrado() bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.cerrado
}
