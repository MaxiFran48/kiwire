package enlazecapa

import (
	"sync"
	"time"
)

// Receptor es la mitad del enlace que recibe. Se ocupa de tres cosas, que son
// justamente los problemas que tiene que resolver la capa de enlace:
//
//   - Marcos danados: verifica el CRC de cada trama. Si no cierra, la descarta
//     y le manda un NAK al emisor para que la reenvie.
//   - Marcos duplicados: si llega una trama con un numero de secuencia que ya
//     habia procesado, no se la vuelve a pasar a la capa de red, pero igual
//     reenvia el ACK, asi el emisor deja de reintentar.
//   - Marcos perdidos: si una trama no llega nunca, no hay nada que hacer aqui.
//     El que se da cuenta es el emisor, por su temporizador, y la reenvie.
//
// Cada trama recibida se entrega a la capa de red una sola vez y en orden,
// que es la garantia que la capa de red necesita.
type Receptor struct {
	medio       InterfazCapaFisica
	verificador *VerificadorErrores
	registro    *Registro
	cfg         Configuracion

	// secEsperada es el numero de secuencia de la trama de datos que se
	// espera ahora. Con Stop-and-Wait solo hay una trama en vuelo, asi que el
	// receptor sabe siempre cual es.
	secEsperada uint8
	// recibido guarda los datos ya aceptados, a la espera de que la capa de
	// red los pida.
	mutex    sync.Mutex
	recibido [][]byte
	// estadisticas para el resumen final.
	aceptadas   int
	descartadas int
	danadas     int
	duplicadas  int
}

// NuevoReceptor crea la punta que recibe del protocolo de enlace sobre el medio
// fisico indicado.
func NuevoReceptor(medio InterfazCapaFisica, cfg Configuracion) *Receptor {
	registro := NuevoRegistro(cfg.Verboso)
	return &Receptor{
		medio:       medio,
		verificador: NuevoVerificadorErrores(registro, actorReceptor),
		registro:    registro,
		cfg:         cfg,
		secEsperada: 0,
	}
}

// UsarRegistro reemplaza la bitacora interna del receptor por la del programa,
// para que toda la traza quede unificada.
func (r *Receptor) UsarRegistro(registro *Registro) {
	r.registro = registro
	r.verificador.registro = registro
}

// Ejecutar es el ciclo del receptor: lee tramas del medio para siempre y las va
// procesando. Se usa en una goroutine, mientras la capa de red consume en la
// otra. Devuelve cuando el medio se cierra.
func (r *Receptor) Ejecutar() {
	r.registro.Info(actorReceptor, "receptor en marcha, medio %s", r.medio.Descripcion())
	for {
		// La espera es un poco mas larga que el temporizador del emisor: si no
		// llega nada en ese tiempo, es que no hay nada que hacer, no que haya
		// que reintentar. El receptor no reintenta nunca.
		bits, err := r.medio.Recibir(esperaDelReceptor)
		if err != nil {
			// Un simple vencimiento de plazo no es un error: se sigue esperando.
			if err == ErrTiempoEspera {
				continue
			}
			// Si el medio ya se cerro, no es una falla: es el fin del programa.
			if r.medio.EstaCerrado() {
				r.registro.Info(actorReceptor, "fin de la recepcion: el medio se cerro")
				return
			}
			r.registro.Aviso(actorReceptor, "el medio fallo: %v", err)
			return
		}
		r.procesarTrama(bits)
	}
}

// esperaDelReceptor es cada cuanto se vuelve a mirar el medio cuando no hay
// nada pendiente. Es un valor grande a proposito: el receptor es pasivo.
const esperaDelReceptor = 500 * time.Millisecond

// procesarTrama aplica, en orden, las cuatro comprobaciones del protocolo sobre
// una trama que llego del medio.
func (r *Receptor) procesarTrama(bits []byte) {
	// 1) Enmarcado: sacar los limites de la trama. Si no se pueden sacar, la
	// trama esta danada y no se puede ni siquiera saber que numero de
	// secuencia tiene, asi que se responde con el NAK de la trama esperada.
	trama, err := extraerTrama(bits)
	if err != nil {
		r.registro.Aviso(actorReceptor, "no se pudo leer la trama: %v -> se descarta", err)
		r.contarDanada()
		r.enviarAcuse(TramaAcuseNegativo, r.secEsperada)
		return
	}
	r.registro.mostrarTrama(actorReceptor, trama, bits)

	// Un ACK o un NAK del otro extremo no es algo que el receptor tenga que
	// procesar: en este protocolo el otro lado actua como emisor, asi que acá no
	// hay nada que hacer con ellos.
	if trama.Tipo != TramaDatos {
		r.registro.Aviso(actorReceptor, "trama %s que no es de datos, se ignora",
			trama.Tipo.nombreLegible())
		return
	}

	// 2) Control de errores: el CRC tiene que coincidir. Con CRC-16 se detectan
	// todos los errores de hasta 16 bits, asi que si el CRC cierra, la trama
	// llego integra.
	if !r.verificador.verificarTrama(trama) {
		r.registro.Aviso(actorReceptor, "trama sec=%d danada -> se pide reenvio con NAK", trama.Sec)
		r.contarDanada()
		r.enviarAcuse(TramaAcuseNegativo, trama.Sec)
		return
	}

	// 3) Marcos duplicados: si el numero de secuencia es el de la trama que ya
	// se proceso, la carga ya esta en la capa de red. Se descarta y se vuelve a
	// mandar el ACK, que es lo que le falta al emisor para dejar de insistir.
	if trama.Sec != r.secEsperada {
		r.registro.Aviso(actorReceptor, "trama sec=%d duplicada (se esperaba sec=%d) -> se descarta y se reack",
			trama.Sec, r.secEsperada)
		r.contarDuplicada()
		r.enviarAcuse(TramaAcuse, trama.Sec)
		return
	}

	// 4) Marco intacto y esperado: se entrega a la capa de red y se confirma.
	r.registro.Info(actorReceptor, "trama sec=%d aceptada, %d bytes para la capa de red",
		trama.Sec, len(trama.Carga))
	r.mutex.Lock()
	r.recibido = append(r.recibido, trama.Carga)
	r.aceptadas++
	r.mutex.Unlock()

	// Con el acknowledgment enviado, el emisor puede seguir. El receptor pasa a
	// esperar la trama del otro numero de secuencia.
	r.enviarAcuse(TramaAcuse, trama.Sec)
	r.secEsperada ^= 1
}

// enviarAcuse arma y manda un ACK o un NAK. Son tramas de protocolo, sin carga,
// que no las espera nadie: si se pierden, el emisor simplemente reintenta.
func (r *Receptor) enviarAcuse(tipo TipoTrama, sec uint8) {
	acuse := Trama{Tipo: tipo, Sec: sec}
	completarCRC(&acuse)
	bits, err := construirBits(acuse)
	if err != nil {
		r.registro.Error(actorReceptor, "no se pudo armar el acuse: %v", err)
		return
	}
	r.registro.Info(actorReceptor, "envio %s sec=%d crc=%04X", tipo.nombreLegible(), sec, acuse.CRC)
	r.registro.mostrarTrama(actorReceptor, acuse, bits)
	if err := r.medio.Transmitir(bits); err != nil {
		r.registro.Aviso(actorReceptor, "no se pudo mandar el acuse: %v", err)
	}
}

// Recibir entrega a la capa de red los datos de la trama aceptada mas antigua.
// Si todavia no llego nada, devuelve false.
func (r *Receptor) Recibir() ([]byte, bool) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if len(r.recibido) == 0 {
		return nil, false
	}
	datos := r.recibido[0]
	r.recibido = r.recibido[1:]
	return datos, true
}

// Estadisticas del receptor, para el resumen final de la demostracion.
//
// Se toman con el mutex porque el ciclo del receptor corre en una goroutine
// distinta de la que pide el resumen.
func (r *Receptor) Estadisticas() (aceptadas, danadas, duplicadas int) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.aceptadas, r.danadas, r.duplicadas
}

// Contadores de tramas. protegidos por el mismo mutex que la cola de datos
// recibidos, porque los lee y los escribe el ciclo del receptor desde otra
// goroutine.
func (r *Receptor) contarDanada() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.danadas++
}

func (r *Receptor) contarDuplicada() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.duplicadas++
}
