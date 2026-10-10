package enlazecapa

import (
	"errors"
	"fmt"
	"time"
)

// Errores del emisor.
var (
	// ErrTramaPerdida indica que se agotaron todos los intentos de enviar una
	// trama y el otro extremo nunca acuso recibo. Es el unico caso en que la
	// capa de enlace le avisa a la capa de red que la entrega fallo.
	ErrTramaPerdida = errors.New("la trama se perdio definitivamente en el medio")

	// ErrTramaDanadaEnReceptor indica que el receptor recibio la trama pero el
	// CRC no cerraba, asi que pidio el reenvio con un NAK.
	ErrTramaDanadaEnReceptor = errors.New("el receptor recibio la trama danada y pidio reenvio")
)

// Emisor es la mitad del enlace que manda. Implementa el protocolo
// Stop-and-Wait con temporizador, que es lo que garantiza que nada se pierda,
// nada llegue danado y nada llegue duplicado.
//
// Como funciona, paso a paso:
//
//  1. La capa de red le pasa los datos.
//  2. El emisor los mete en una trama, le calcula el CRC y la manda.
//  3. Queda esperando con un temporizador. En este estado NO puede mandar
//     otra trama: de ahi el nombre "Stop and Wait". Ese es el control de
//     flujo: un emisor rapido no puede saturar a un receptor lento, porque
//     no puede tener mas de una trama en vuelo.
//  4. Si llega un ACK, sigue con la trama siguiente.
//  5. Si llega un NAK (el receptor vio la trama danada) o se vence el
//     temporizador (la trama se perdio), reintenta la misma trama.
//  6. Si se agotan los reintentos, le avisa el error a la capa de red.
type Emisor struct {
	medio       InterfazCapaFisica
	verificador *VerificadorErrores
	registro    *Registro
	cfg         Configuracion

	// sec es el numero de secuencia de la proxima trama a mandar. Se alterna
	// 0, 1, 0, 1... Es lo que permite al receptor reconocer duplicados.
	sec uint8
	// enviados cuenta cuantas tramas se entregaron con exito.
	enviados int
	// reintentos cuenta cuantas veces hubo que reenviar alguna trama, en
	// total. Se muestra al final como estadistica del enlace.
	reintentos int
	// tramasPerdidas cuenta las tramas que hubo que abandonar.
	tramasPerdidas int
}

// NuevoEmisor crea la punta que envia del protocolo de enlace sobre el medio
// fisico indicado.
func NuevoEmisor(medio InterfazCapaFisica, cfg Configuracion) *Emisor {
	registro := NuevoRegistro(cfg.Verboso)
	return &Emisor{
		medio:       medio,
		verificador: NuevoVerificadorErrores(registro, actorEmisor),
		registro:    registro,
		cfg:         cfg,
		sec:         0,
	}
}

// UsarRegistro reemplaza la bitacora interna del emisor por la del programa, para
// que toda la traza quede unificada.
func (e *Emisor) UsarRegistro(registro *Registro) {
	e.registro = registro
	e.verificador.registro = registro
}

// Transfiere es lo que llama la capa de red. Parte los datos en tramas de hasta
// 255 bytes, manda cada una y espera su acuse antes de seguir con la siguiente.
//
// Devuelve error si alguna trama no pudo entregarse ni despues de todos los
// reintentos.
func (e *Emisor) Transfiere(datos []byte) error {
	if len(datos) == 0 {
		return nil
	}

	// Los datos de la capa de red se parten en trozos de una trama. Con un solo
	// bloque de datos mas grande que el maximo, se mandan varias tramas, una
	// por vez, esperando el acuse de cada una antes de mandar la siguiente.
	for inicio := 0; inicio < len(datos); inicio += MaxBytesPorTrama {
		fin := inicio + MaxBytesPorTrama
		if fin > len(datos) {
			fin = len(datos)
		}
		carga := append([]byte{}, datos[inicio:fin]...)

		if err := e.enviarTrama(TramaDatos, carga); err != nil {
			return err
		}
		e.enviados++

		// Recien acuseada la trama, se cambia el numero de secuencia para la
		// siguiente. El receptor va haciendo lo mismo, asi que los dos llevan
		// la cuenta en paralelo. Importante: el cambio va aca y no al final del
		// bloque, porque tiene que pasar una vez por trama, y solo cuando la
		// trama quedo confirmada: mientras se reintenta, el numero de
		// secuencia tiene que seguir siendo el mismo para que el receptor
		// reconozca el reenvio y no lo tome por una trama nueva.
		e.sec ^= 1
	}
	return nil
}

// enviarTrama manda una trama y espera su acuse. Reintenta mientras haga falta.
func (e *Emisor) enviarTrama(tipo TipoTrama, carga []byte) error {
	trama := Trama{Tipo: tipo, Sec: e.sec, Carga: carga}
	// Se completa el CRC antes de la primera transmision y se reusa en todos los
	// reintentos: es siempre la misma trama.
	completarCRC(&trama)

	var ultimoError error = ErrTiempoEspera
	for intento := 1; intento <= e.cfg.MaxIntentos; intento++ {
		if intento > 1 {
			e.reintentos++
			trama.NumIntentos = intento
			e.registro.Aviso(actorEmisor, "reintento %d/%d de la trama %s sec=%d",
				intento, e.cfg.MaxIntentos, tipo.nombreLegible(), trama.Sec)
		}

		// --- Se manda la trama por la capa fisica ---
		bits, err := construirBits(trama)
		if err != nil {
			return err
		}
		e.registro.Info(actorEmisor, "envio %s sec=%d largo=%d crc=%04X",
			tipo.nombreLegible(), trama.Sec, len(trama.Carga), trama.CRC)
		e.registro.mostrarTrama(actorEmisor, trama, bits)
		if err := e.medio.Transmitir(bits); err != nil {
			return err
		}

		// --- Se espera el acuse, con temporizador ---
		err = e.esperarAcuse(trama.Sec)
		if err == nil {
			e.registro.Info(actorEmisor, "acuse recibido: la trama llego bien")
			return nil
		}
		ultimoError = err
		e.registro.Aviso(actorEmisor, "sin acuse: %v", err)
	}

	// Se agotaron los intentos: la trama se perdio de verdad y la capa de red
	// tiene que saberlo, porque su entrega quedo incompleta.
	e.tramasPerdidas++
	return fmt.Errorf("%w: %s sec=%d despues de %d intentos (ultimo fallo: %v)",
		ErrTramaPerdida, tipo.nombreLegible(), trama.Sec, e.cfg.MaxIntentos, ultimoError)
}

// esperarAcuse bloquea hasta que llegue el ACK del numero de secuencia esperado.
// Si se vence el temporizador, devuelve ErrTiempoEspera y el emisor reintenta.
func (e *Emisor) esperarAcuse(secEsperado uint8) error {
	// El temporizador es el reloj del protocolo: si no llega nada en
	// TiempoEspera, se da la trama por perdida.
	plazo := time.Now().Add(e.cfg.TiempoEspera)
	temporizador := time.NewTimer(e.cfg.TiempoEspera)
	defer temporizador.Stop()

	// La espera se hace en loop porque al medio se le pueden colgar tramas que
	// no son el acuse buscado (un duplicado, por ejemplo) y eso no puede cortar
	// la espera.
	for {
		bits, err := e.medio.Recibir(tiempoRestante(plazo))
		if err != nil {
			// ErrTiempoEspera del medio: se vencio el temporizador del emisor.
			return ErrTiempoEspera
		}

		// Del medio salen bytes crudos: primero hay que encontrar los limites de
		// la trama.
		acuse, err := extraerTrama(bits)
		if err != nil {
			e.registro.Aviso(actorEmisor, "llego algo ilegible del medio: %v", err)
			continue
		}
		e.registro.mostrarTrama(actorEmisor, acuse, bits)

		// Todo lo que llega de mas (por ejemplo un ACK viejo de una trama que
		// ya se dio por perdida) se ignora.
		if acuse.Sec != secEsperado {
			e.registro.Aviso(actorEmisor, "acuse de otra trama (sec=%d), se ignora", acuse.Sec)
			continue
		}

		// El CRC del acuse tambien se verifica: un NAK que llega danado no se
		// puede creer y se trata como si no hubiera llegado nada.
		if !e.verificador.verificarTrama(acuse) {
			e.registro.Aviso(actorEmisor, "acuse ilegible, se sigue esperando")
			continue
		}

		switch acuse.Tipo {
		case TramaAcuse:
			return nil
		case TramaAcuseNegativo:
			// El receptor vio la trama danada: se reintenta en el acto.
			return ErrTramaDanadaEnReceptor
		default:
			e.registro.Aviso(actorEmisor, "acuse de tipo inesperado, se ignora")
		}
	}
}

// tiempoRestante devuelve cuanto le queda a un plazo que empieza ahora, con un
// piso de un milisegundo para que la lectura con plazo no venza en el ultimo
// instante y se crea un bucle de lecturas vacias.
func tiempoRestante(plazo time.Time) time.Duration {
	restante := time.Until(plazo)
	if restante < time.Millisecond {
		return time.Millisecond
	}
	return restante
}

// Estadisticas del emisor, para el resumen final de la demostracion.
func (e *Emisor) Estadisticas() (enviados int, reintentos int, perdidas int) {
	return e.enviados, e.reintentos, e.tramasPerdidas
}
