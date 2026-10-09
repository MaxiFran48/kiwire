package enlazecapa

import (
	"math/rand"
	"sync"
	"time"
)

// MedioSimulado es el "medio de transmision en bruto": un canal que solo mueve
// bits y que puede hacer todas las cosas que hace un medio real y no controlado.
//
// A diferencia de un medio bien behaved, este NO garantiza nada: puede invertir
// bits, puede perder una trama entera, puede entregarla dos veces y siempre
// tarda un poco. Todo eso lo decide al azar, con las probabilidades de la
// configuracion, y con una semilla fija para que las pruebas sean reproducibles.
//
// Cada extremo del enlace tiene su propio MedioSimulado, y los dos estan
// conectados entre si (como un par de cables): lo que uno transmite aparece en
// la cola de recepcion del otro, y nunca vuelve al mismo lado. Asi el emisor no
// puede leer sus propias tramas.
type MedioSimulado struct {
	mutex sync.Mutex
	// cola son las tramas que todavia no fueron entregadas a este extremo.
	cola []entregaFisica
	// aviso se cierra y se reemplaza cada vez que llega algo a la cola. Quien
	// este esperando toma el canal cerrado como "hay news que revisar".
	aviso chan struct{}
	// cerrado indica que el medio dejo de funcionar.
	cerrado bool

	// contraparte es el otro extremo del cable. Al transmitir se le entrega la
	// trama a el, nunca a uno mismo.
	contraparte *MedioSimulado

	azar     *rand.Rand
	cfg      Configuracion
	registro *Registro
}

// entregaFisica es una trama que viaja por el medio, con el momento en que va a
// estar disponible en la cola del otro extremo.
type entregaFisica struct {
	bits     []byte
	descanso time.Time // instante en que la trama llega al otro extremo
}

// NuevoParMedioSimulado crea los dos extremos de un medio simulado, ya
// conectados entre si. Devuelve dos medios porque el enlace es punto a punto:
// una punta emite y la otra recibe.
func NuevoParMedioSimulado(cfg Configuracion, registro *Registro) (*MedioSimulado, *MedioSimulado) {
	extremoA := &MedioSimulado{
		aviso:    make(chan struct{}),
		azar:     rand.New(rand.NewSource(cfg.Semilla)),
		cfg:      cfg,
		registro: registro,
	}
	extremoB := &MedioSimulado{
		aviso:    make(chan struct{}),
		azar:     rand.New(rand.NewSource(cfg.Semilla + 1)),
		cfg:      cfg,
		registro: registro,
	}
	extremoA.contraparte = extremoB
	extremoB.contraparte = extremoA
	return extremoA, extremoB
}

// Transmitir simula el envio de una trama fisica por el cable. Decide al azar si
// la trama se pierde, si llega corrupta y si llega duplicada, y en que momento
// se hace entrega en el otro extremo.
//
// Ojo con el detalle importante: si la trama se pierde o se duplica, Transmitir
// NO devuelve error. El emisor no se entera de nada; se va a enterar cuando no
// llegue el acuse de recibo dentro del plazo. Asi es como funciona de verdad.
func (m *MedioSimulado) Transmitir(bits []byte) error {
	m.mutex.Lock()
	if m.cerrado {
		m.mutex.Unlock()
		return ErrMedioCerrado
	}
	contraparte := m.contraparte
	azar := m.azar
	cfg := m.cfg
	m.mutex.Unlock()

	// 1) Perdida total de la trama: se simula con un simple return, como si el
	// cable se hubiera cortado en ese instante.
	if azar.Float64() < cfg.ProbPerdidaMarco {
		m.registro.Aviso(actorFisico, "el medio se traga la trama completa (perdida simulada)")
		return nil
	}

	// 2) Errores de bit: cada bit se invierte con probabilidad ProbErrorBit.
	// Este es el error "no detectado" tipico de un medio analogico.
	entregada := voltearBitsAlAzar(bits, azar, cfg.ProbErrorBit)
	if !igualesBytes(entregada, bits) {
		m.registro.Aviso(actorFisico, "la trama llega alterada: se invirtieron %d bits",
			bitsDistintos(bits, entregada))
	}

	// 3) Entrega normal, con un retardo tipico mas algo de fluctuacion.
	contraparte.agregarEnCola(entregada, m.calcularRetardo(azar, cfg))

	// 4) Duplicado: la misma trama vuelve a aparecer, como cuando un repetidor
	// o una congestion en la red la entregan dos veces.
	if azar.Float64() < cfg.ProbDuplicadoMarco {
		m.registro.Aviso(actorFisico, "el medio entrega la trama por duplicado")
		contraparte.agregarEnCola(entregada, m.calcularRetardo(azar, cfg))
	}
	return nil
}

// Recibir espera a que llegue la proxima trama fisica. Si pasa el tiempo de
// espera indicado sin que llegue nada, devuelve ErrTiempoEspera, que la capa de
// enlace interpreta como "esa trama se perdio".
func (m *MedioSimulado) Recibir(espera time.Duration) ([]byte, error) {
	limite := time.Now().Add(espera)

	for {
		m.mutex.Lock()
		if m.cerrado {
			m.mutex.Unlock()
			return nil, ErrMedioCerrado
		}

		// Se ve si hay alguna trama en la cola que ya llego a su hora.
		ahora := time.Now()
		for i, entrega := range m.cola {
			if !ahora.Before(entrega.descanso) {
				bits := entrega.bits
				m.cola = append(m.cola[:i], m.cola[i+1:]...)
				m.mutex.Unlock()
				return bits, nil
			}
		}

		// Si ya se cumplio el plazo que dio el llamador, la trama se da por
		// perdida: no hay nada mas que esperar.
		if !ahora.Before(limite) {
			m.mutex.Unlock()
			return nil, ErrTiempoEspera
		}

		// Todavia hay margen: se espera lo que falte para que llegue la trama
		// mas cercana, o hasta el plazo del llamador, lo que ocurra primero.
		esperaMinima := limite.Sub(ahora)
		for _, entrega := range m.cola {
			if restante := entrega.descanso.Sub(ahora); restante < esperaMinima {
				esperaMinima = restante
			}
		}
		aviso := m.aviso
		m.mutex.Unlock()

		// Se espera a que llegue algo, o a que se venza el reloj. Cuando suena
		// el reloj es porque ya deberia haber una trama lista, asi que el ciclo
		// vuelve arriba a buscarla.
		temporizador := time.NewTimer(esperaMinima)
		select {
		case <-aviso:
			temporizador.Stop()
		case <-temporizador.C:
		}
	}
}

// Cerrar apaga el medio y libera a quien este esperando.
func (m *MedioSimulado) Cerrar() error {
	m.mutex.Lock()
	if m.cerrado {
		m.mutex.Unlock()
		return nil
	}
	m.cerrado = true
	m.cola = nil
	// Se cierra el canal de aviso para despertar a todos los que esperan: van a
	// ver que el medio esta cerrado y van a terminar.
	close(m.aviso)
	// Se suelta la referencia a la contraparte y se la va a cerrar DESPUES de
	// liberar el mutex. Si se hiciera adentro, cada extremo cerraria al otro
	// con el mutex tomado y los dos quedarian esperando al otro para siempre.
	contraparte := m.contraparte
	m.contraparte = nil
	m.mutex.Unlock()

	if contraparte != nil {
		return contraparte.Cerrar()
	}
	return nil
}

// Descripcion resume como esta configurado el medio, para la bitacora.
func (m *MedioSimulado) Descripcion() string {
	return "simulado(error=" + formatearProbabilidad(m.cfg.ProbErrorBit) +
		", perdida=" + formatearProbabilidad(m.cfg.ProbPerdidaMarco) +
		", duplicado=" + formatearProbabilidad(m.cfg.ProbDuplicadoMarco) + ")"
}

// agregarEnCola deja una trama lista para ser entregada a este extremo, con un
// retardo de reposo. Quien este esperando en Recibir se entera por el canal de
// aviso.
func (m *MedioSimulado) agregarEnCola(bits []byte, descanso time.Duration) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.cerrado {
		return
	}
	m.cola = append(m.cola, entregaFisica{bits: bits, descanso: time.Now().Add(descanso)})
	// Se cierra el canal viejo y se crea uno nuevo: es la forma de avisarle a
	// todos los que estan esperando sin que se bloqueen entre ellos.
	close(m.aviso)
	m.aviso = make(chan struct{})
}

// calcularRetardo sortea el retardo de una trama: la mitad del retardo tipico mas
// algo de variacion, para que las tramas no lleguen todas en fila.
func (m *MedioSimulado) calcularRetardo(azar *rand.Rand, cfg Configuracion) time.Duration {
	if cfg.RetardoMedio <= 0 {
		return 0
	}
	variacion := int64(cfg.RetardoMedio)
	return time.Duration(int64(cfg.RetardoMedio)/2 + azar.Int63n(variacion+1))
}

// voltearBitsAlAzar devuelve una copia de los bits con algunos bits invertidos.
// Cada bit se invierte con la probabilidad indicada.
func voltearBitsAlAzar(bits []byte, azar *rand.Rand, probabilidad float64) []byte {
	if probabilidad <= 0 {
		return append([]byte{}, bits...)
	}
	// Se trabaja bit a bit sobre el flujo empaquetado, asi que primero se
	// desarma a bits sueltos, se perturban y se vuelve a empaquetar.
	desempaquetado := desempaquetarBits(bits, cantidadDeBits(len(bits)))
	alterado := make([]byte, len(desempaquetado))
	for i, bit := range desempaquetado {
		if azar.Float64() < probabilidad {
			alterado[i] = bit ^ mascaraUltimoBit
		} else {
			alterado[i] = bit
		}
	}
	return empaquetarBits(alterado)
}

// igualesBytes compara dos secuencias de bytes.
func igualesBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// bitsDistintos cuenta cuantos bits difieren entre dos secuencias de bytes, y
// se usa para contar cuantos bits se invirtieron en una trama.
func bitsDistintos(a, b []byte) int {
	bitsA := desempaquetarBits(a, cantidadDeBits(len(a)))
	bitsB := desempaquetarBits(b, cantidadDeBits(len(b)))
	if len(bitsA) != len(bitsB) {
		return -1
	}
	diferentes := 0
	for i := range bitsA {
		if bitsA[i] != bitsB[i] {
			diferentes++
		}
	}
	return diferentes
}

// formatearProbabilidad escribe una probabilidad como porcentaje corto.
func formatearProbabilidad(valor float64) string {
	return formatearFlotante(valor*100) + "%"
}

// EstaCerrado dice si el medio ya se cerro.
func (m *MedioSimulado) EstaCerrado() bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.cerrado
}
