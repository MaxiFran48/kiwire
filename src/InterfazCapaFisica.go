package enlazecapa

import "time"

// Errores de la capa fisica.
var (
	// ErrMedioCerrado indica que se quiso usar el medio con el socket cerrado.
	ErrMedioCerrado = errorMedio("el medio fisico esta cerrado")
	// ErrTiempoEspera indica que el medio no entrego nada a tiempo. La capa de
	// enlace lo trata igual que una trama perdida.
	ErrTiempoEspera = errorMedio("el medio no entrego datos a tiempo")
)

// errorMedio es un tipo de error propio de la capa fisica, para poder
// distinguirlo de los errores de la capa de enlace con errors.As.
type errorMedio string

func (e errorMedio) Error() string { return string(e) }

// InterfazCapaFisica es el medio de transmision en bruto. Solo mueve bits: no
// conoce tramas, ni CRC, ni acuses. Es el "canal" que puede enviar los datos
// correctos, enviarlos danados, perderlos, duplicarlos o tardarlos.
//
// La capa de enlace habla en bits porque el medio crudo no entiende de bytes:
// un cable (o un socket mal sincronizado) transporta un flujo de bits y nada
// mas. Los bytes se reconstruyen arriba, en ConversorTramas.
type InterfazCapaFisica interface {
	// Transmitir envia por el medio una trama fisica completa, ya con sus
	// banderas, stuffing y CRC incluidos. Si el medio decide perder la trama,
	// no pasa nada: el error no se conoce hasta que el ACK no llega.
	Transmitir(bits []byte) error

	// Recibir devuelve los bits crudos de la proxima trama fisica que llega.
	// Devuelve ErrTiempoEspera si pasa la duracion indicada sin recibir nada,
	// porque en ese caso la trama se considera perdida.
	Recibir(espera time.Duration) ([]byte, error)

	// Cerrar libera el medio.
	Cerrar() error

	// EstaCerrado dice si el medio ya se cerro. La capa de enlace lo consulta
	// para distinguir "el otro equipo se desconecto" de "el programa se esta
	// cerrando", que no es un error de nadie.
	EstaCerrado() bool

	// Descripcion devuelve un texto corto con el modo de transporte, para la
	// bitacora (por ejemplo "simulado(error=0.01)" o "tcp(127.0.0.1:9000)").
	Descripcion() string
}

// ---------------------------------------------------------------------------
// Utilidades de bits
//
// Un medio fisico transmite bits sueltos, asi que aqui se trabaja con
// []byte donde cada byte representa 8 bits, del mas significativo al menos
// significativo. Asi el medio no distingue "byte 0xA5" de "10100101": solo ve
// una secuencia de bits.
// ---------------------------------------------------------------------------

const mascaraUltimoBit = 0x01

// bitsAEntero reconstruye el valor entero que representan los bits de un byte
// leidos de izquierda a derecha, siendo el primero el bit mas significativo.
func bitsAEntero(bits []byte) byte {
	var valor byte
	for _, bit := range bits {
		valor = (valor << 1) | (bit & mascaraUltimoBit)
	}
	return valor
}

// enteroABits descompone un byte en sus 8 bits, del mas significativo al menos
// significativo. El resultado siempre tiene 8 elementos.
func enteroABits(valor byte) []byte {
	bits := make([]byte, 8)
	for i := 0; i < 8; i++ {
		// El bit mas significativo del byte original va primero.
		bits[i] = (valor >> (7 - i)) & mascaraUltimoBit
	}
	return bits
}

// agregarBits anexa al destino los bits de un byte, sin empaquetar todavia.
func agregarBits(destino []byte, valor byte) []byte {
	return append(destino, enteroABits(valor)...)
}

// empaquetarBits convierte el flujo plano de bits (0 y 1) en bytes de a 8,
// rellenando con ceros a la derecha el ultimo byte. Al revés de lo que hace
// el medio fisico real, que envia los bits de a 8.
func empaquetarBits(bits []byte) []byte {
	if len(bits) == 0 {
		return []byte{}
	}
	salida := make([]byte, (len(bits)+7)/8)
	for i, bit := range bits {
		if bit&mascaraUltimoBit == 1 {
			salida[i/8] |= 1 << (7 - i%8)
		}
	}
	return salida
}

// desempaquetarBits convierte los bytes que entrega el medio en un flujo plano
// de bits. Los bits de relleno del ultimo byte se descartan.
func desempaquetarBits(entrada []byte, cantidadBits int) []byte {
	if cantidadBits <= 0 {
		return []byte{}
	}
	salida := make([]byte, 0, cantidadBits)
	for _, valor := range entrada {
		for i := 0; i < 8 && len(salida) < cantidadBits; i++ {
			salida = append(salida, (valor>>(7-i))&mascaraUltimoBit)
		}
	}
	return salida
}

// cantidadDeBits dice cuantos bits tiene una trama de N bytes.
func cantidadDeBits(cantidadBytes int) int {
	return cantidadBytes * 8
}

// volcarBits escribe un flujo de bits como texto "0110 1100..." para poder
// mirarlo en la bitacora.
func volcarBits(bits []byte) string {
	var texto string
	for i, bit := range bits {
		if i > 0 && i%4 == 0 {
			texto += " "
		}
		texto += string('0' + bit)
	}
	return texto
}

// volcarBytes escribe los bytes en hexadecimal, que es la forma mas comoda de
// ver una trama completa en la bitacora.
func volcarBytes(datos []byte) string {
	const digitos = "0123456789ABCDEF"
	texto := make([]byte, 0, len(datos)*3)
	for i, valor := range datos {
		if i > 0 {
			texto = append(texto, ' ')
		}
		texto = append(texto, digitos[valor>>4], digitos[valor&0x0F])
	}
	return string(texto)
}
