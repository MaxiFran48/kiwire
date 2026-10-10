package enlazecapa

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

// MedioSerie lleva las tramas fisicas por un puerto serie. Es el medio para
// hacer la prueba de verdad entre dos equipos con un cable USB-serial o un
// null-modem RS-232: sin switches, sin routers, solo el cable.
//
// Formato en el cable: el mismo que por TCP, un entero de 4 bytes con el largo
// de la trama y despues la trama. En una linea serie no hay forma de saber
// donde termino una trama, asi que ese prefijo cumple el papel de la bandera.
type MedioSerie struct {
	archivo *os.File

	mutex   sync.Mutex
	cerrado bool

	descripcion string
	registro    *Registro
}

// NuevoMedioSerie abre el puerto serie indicado (por ejemplo /dev/ttyUSB0 o
// COM3 en Windows), configura la velocidad y lo deja en modo crudo, es decir,
// sin ningun tipo de procesamiento de texto: el puerto tiene que entregar los
// bytes tal cual, porque el protocolo de enlace ya se encarga del framing.
//
// La configuracion del puerto se hace con la herramienta stty del sistema, que
// viene en Linux y en macOS. Asi el paquete no necesita librerias externas.
func NuevoMedioSerie(puerto string, baudios int, registro *Registro) (*MedioSerie, error) {
	comando := exec.Command("stty", "-F", puerto, fmt.Sprintf("%d", baudios), "raw", "-echo", "-echoe", "-echok", "cs8")
	salida, err := comando.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("no se pudo configurar el puerto %s con stty: %v (%s)", puerto, err, salida)
	}

	archivo, err := os.OpenFile(puerto, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir el puerto %s: %w", puerto, err)
	}
	registro.Info(actorFisico, "puerto serie %s abierto a %d baudios", puerto, baudios)
	return &MedioSerie{
		archivo:     archivo,
		descripcion: fmt.Sprintf("serie(%s,%d)", puerto, baudios),
		registro:    registro,
	}, nil
}

// Transmitir escribe el prefijo de largo y despues la trama.
func (m *MedioSerie) Transmitir(bits []byte) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.cerrado {
		return ErrMedioCerrado
	}
	mensaje := make([]byte, prefijoLargoEnCable+len(bits))
	escribirEnteroGrande(mensaje[:prefijoLargoEnCable], len(bits))
	copy(mensaje[prefijoLargoEnCable:], bits)
	if _, err := m.archivo.Write(mensaje); err != nil {
		return fmt.Errorf("fallo al escribir en el puerto serie: %w", err)
	}
	return nil
}

// Recibir lee una trama del puerto. El plazo se implementa con SetReadDeadline,
// que en puertos serie se traduce a un timeout de la lectura del sistema
// operativo. Un plazo de cero deja la lectura bloqueante.
func (m *MedioSerie) Recibir(espera time.Duration) ([]byte, error) {
	m.mutex.Lock()
	if m.cerrado {
		m.mutex.Unlock()
		return nil, ErrMedioCerrado
	}
	m.mutex.Unlock()

	if espera > 0 {
		m.archivo.SetReadDeadline(time.Now().Add(espera))
		defer m.archivo.SetReadDeadline(time.Time{})
	}

	prefijo := make([]byte, prefijoLargoEnCable)
	if _, err := leerEntero(m.archivo, prefijo); err != nil {
		return nil, traducirErrorLectura(err)
	}
	largo := leerEnteroGrande(prefijo)
	if largo <= 0 {
		return nil, ErrTramaVacia
	}
	bits := make([]byte, largo)
	if _, err := leerEntero(m.archivo, bits); err != nil {
		return nil, traducirErrorLectura(err)
	}
	return bits, nil
}

// Cerrar cierra el puerto.
func (m *MedioSerie) Cerrar() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.cerrado {
		return nil
	}
	m.cerrado = true
	return m.archivo.Close()
}

// Descripcion devuelve el modo de transporte para la bitacora.
func (m *MedioSerie) Descripcion() string {
	return m.descripcion
}

// leerEntero lee exactamente la cantidad de bytes pedida, ni mas ni menos. Si el
// puerto entrega menos, devuelve error, que en la practica casi siempre es un
// cierre del otro equipo.
func leerEntero(archivo *os.File, destino []byte) (int, error) {
	leidos := 0
	for leidos < len(destino) {
		n, err := archivo.Read(destino[leidos:])
		leidos += n
		if err != nil {
			return leidos, err
		}
	}
	return leidos, nil
}

// EstaCerrado dice si el medio ya se cerro.
func (m *MedioSerie) EstaCerrado() bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.cerrado
}
