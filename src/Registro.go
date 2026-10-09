package enlazecapa

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

// Nombres de los actores que aparecen en la bitacora, para poder distinguir
// de un vistazo que parte del protocolo esta hablando.
const (
	actorFisico   = "FISICO"
	actorEmisor   = "EMISOR"
	actorReceptor = "RECEPTOR"
	actorCapaRed  = "CAPA-RED"
)

// Registro es la bitacora del protocolo: deja escrito que hizo cada componente
// (capa fisica, emisor, receptor) con marca de tiempo. Sirve para demostrar en
// la practica como el enlace resuelve errores, perdidas y duplicados.
//
// Es seguro para uso concurrente: la demostracion corre las dos puntas del
// enlace en goroutines separadas y pueden escribir a la vez.
type Registro struct {
	mutex   sync.Mutex
	verboso bool
}

// NuevoRegistro crea una bitacora. Si verboso es true, ademas imprime el
// volcado de bytes y de bits de cada trama.
func NuevoRegistro(verboso bool) *Registro {
	return &Registro{verboso: verboso}
}

// Info escribe una linea de traza normal.
func (r *Registro) Info(actor, formato string, argumentos ...any) {
	r.escribir(actor, "INFO", formato, argumentos...)
}

// Aviso escribe una linea de traza de una situacion anomala pero controlada,
// como una trama que llego danada o un reintento.
func (r *Registro) Aviso(actor, formato string, argumentos ...any) {
	r.escribir(actor, "AVISO", formato, argumentos...)
}

// Error escribe una linea de error.
func (r *Registro) Error(actor, formato string, argumentos ...any) {
	r.escribir(actor, "ERROR", formato, argumentos...)
}

// escribir arma la linea y la imprime. El mutex evita que dos goroutines
// mezclen sus mensajes.
func (r *Registro) escribir(actor, nivel, formato string, argumentos ...any) {
	if r == nil {
		return
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	fmt.Fprintf(os.Stdout, "%s %-8s %-5s %s\n",
		time.Now().Format("15:04:05.000"), actor, nivel, fmt.Sprintf(formato, argumentos...))
}

// Verboso indica si hay que mostrar el detalle de bits y bytes.
func (r *Registro) Verboso() bool {
	return r != nil && r.verboso
}

// mostrarTrama imprime el detalle de una trama, solo si la bitacora es verbosa.
func (r *Registro) mostrarTrama(actor string, trama Trama, bits []byte) {
	if !r.Verboso() {
		return
	}
	r.escribir(actor, "TRAMA", "%s sec=%d largo=%d crc=%04X carga=[%s]",
		trama.Tipo.nombreLegible(), trama.Sec, trama.Largo, trama.CRC,
		volcarBytesLimitado(trama.Carga, bytesEnElVolcado))
	if len(bits) > 0 {
		bitsDelMedio := desempaquetarBits(bits, cantidadDeBits(len(bits)))
		if len(bitsDelMedio) > bitsEnElVolcado {
			bitsDelMedio = bitsDelMedio[:bitsEnElVolcado]
		}
		r.escribir(actor, "BITS ", "%s", volcarBits(bitsDelMedio))
	}
}

// Cantidad de bytes y de bits que se muestran en la bitacora verbosa. Los
// volcados se cortan porque una trama puede tener 1400 bytes de carga, y su
// equivalente en bits ocupa miles de caracteres: en una terminal eso no se lee.
const (
	bytesEnElVolcado = 24
	bitsEnElVolcado  = 8 * 2 * bytesEnElVolcado
)

// volcarBytesLimitado escribe los primeros bytes en hexadecimal y aclara que
// quedo informacion afuera.
func volcarBytesLimitado(datos []byte, maximo int) string {
	if len(datos) <= maximo {
		return volcarBytes(datos)
	}
	return volcarBytes(datos[:maximo]) + " ... (+" + strconv.Itoa(len(datos)-maximo) + " bytes)"
}

// formatearFlotante escribe un float corto, sin ceros de mas, para las
// probabilidades de la bitacora.
func formatearFlotante(valor float64) string {
	return strconv.FormatFloat(valor, 'f', -1, 64)
}
