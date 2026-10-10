// Paquete enlazecapa contiene la implementación de la capa de enlace de datos
// solicitada en el TP: transforma un medio de transmisión en bruto en una línea
// que parece libre de errores no detectados hacia la capa de red.
//
// Cada archivo del paquete representa un componente del diseño:
//
//	InterfazCapaRed.go      qué expone el enlace hacia la capa de red
//	InterfazCapaFisica.go   qué se le exige al medio de transmisión
//	MedioSimulado.go        medio en bruto con errores, pérdidas y duplicados
//	MedioTCP.go             mismo medio, pero entre dos máquinas por TCP
//	MedioSerie.go           mismo medio, pero entre dos equipos por cable serie
//	ConversorTramas.go      arma y desarma las tramas (enmarcado + stuffing)
//	VerificadorErrores.go   cálculo y verificación del CRC-16
//	Emisor.go               lado que envía: Stop-and-Wait con temporizador
//	Receptor.go             lado que recibe: CRC, duplicados y acuses
//	TiposDeTrama.go         estructuras de datos compartidas
//	Registro.go             bitácora de lo que va pasando
package enlazecapa

import "time"

// TipoTrama identifica para qué sirve una trama. El receptor decide qué hacer
// con ella (o qué contestarle al emisor) a partir de este campo.
type TipoTrama byte

const (
	// TramaDatos lleva carga de la capa de red y espera un acuse de recibo.
	TramaDatos TipoTrama = 0x01
	// TramaAcuse (ACK) confirma que una trama de datos llegó íntegra.
	TramaAcuse TipoTrama = 0x02
	// TramaAcuseNegativo (NAK) avisa que la trama llegó dañada y debe reenviarse.
	TramaAcuseNegativo TipoTrama = 0x03
)

// nombreLegible devuelve el nombre del tipo de trama para la bitácora.
func (t TipoTrama) nombreLegible() string {
	switch t {
	case TramaDatos:
		return "DATOS"
	case TramaAcuse:
		return "ACK"
	case TramaAcuseNegativo:
		return "NAK"
	default:
		return "DESCONOCIDO"
	}
}

// esValido indica si el tipo de trama es uno de los tres que define el
// protocolo. Sirve para descartar basura que llegue del medio.
func (t TipoTrama) esValido() bool {
	return t == TramaDatos || t == TramaAcuse || t == TramaAcuseNegativo
}

// Trama es la unidad de datos de la capa de enlace. Todavía no tiene stuffing
// ni banderas: es la trama "lógica" que se calcula y después se convierte en
// bits crudos para mandarla por la capa física.
type Trama struct {
	Tipo        TipoTrama // DATOS, ACK o NAK
	Sec         uint8     // número de secuencia (0 o 1, alternado)
	Carga       []byte    // datos de la capa de red (vacío en ACK y NAK)
	CRC         uint16    // CRC-16 calculado sobre Tipo, Sec, Largo y Carga
	Largo       int       // cantidad de bytes de la carga
	NumIntentos int       // cuántas veces se intentó enviar (solo informativo, para la bitácora)
}

// Configuracion agrupa todos los parámetros del protocolo y del medio. Se
// completa desde la línea de comandos en main.go.
type Configuracion struct {
	// --- Parámetros del protocolo de enlace ---

	// MaxIntentos es la cantidad máxima de veces que se intenta enviar una
	// trama antes de darla por perdida y avisarle el error a la capa de red.
	MaxIntentos int

	// TiempoEspera es el temporizador del emisor: si no llega el ACK en este
	// plazo, se reenvía la trama. Tiene que ser mayor que el retardo máximo
	// que introduce el medio.
	TiempoEspera time.Duration

	// --- Parámetros del medio simulado ---

	// Semilla fija la generación de números aleatorios del medio simulado.
	// Con la misma semilla la simulación da siempre el mismo resultado, lo
	// que hace reproducibles las pruebas.
	Semilla int64

	// ProbErrorBit es la probabilidad de que un bit viaje invertido.
	ProbErrorBit float64

	// ProbPerdidaMarco es la probabilidad de que una trama se pierda completa.
	ProbPerdidaMarco float64

	// ProbDuplicadoMarco es la probabilidad de que una trama llegue dos veces.
	ProbDuplicadoMarco float64

	// RetardoMedio es el retardo típico que el medio simulado impone a cada
	// trama. Se sortea alrededor de este valor.
	RetardoMedio time.Duration

	// --- Parámetros de la interfaz con la capa de red ---

	// ArchivoDatos es el archivo que la capa de red entrega al enlace para
	// mandar, o que el enlace guarda con lo que recibió.
	ArchivoDatos string

	// Verboso activa el detalle de la bitácora (volcado de bits y de bytes).
	Verboso bool
}

// ConfiguracionPorDefecto devuelve la configuración usada si no se pasa
// ninguna en la línea de comandos. Los valores son conservadores: el medio
// empieza limpio y el emisor es paciente.
func ConfiguracionPorDefecto() Configuracion {
	return Configuracion{
		MaxIntentos:        5,
		TiempoEspera:       2 * time.Second,
		Semilla:            1,
		ProbErrorBit:       0,
		ProbPerdidaMarco:   0,
		ProbDuplicadoMarco: 0,
		RetardoMedio:       20 * time.Millisecond,
		ArchivoDatos:       "datos.txt",
		Verboso:            false,
	}
}
