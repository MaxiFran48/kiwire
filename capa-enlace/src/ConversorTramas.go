package enlazecapa

import "errors"

// Enmarcado: la capa de enlace tiene que poner y quitar los limites de las
// tramas sobre un medio que solo transporta un flujo de bits. Para eso usa una
// bandera (FLAG) igual que HDLC.
//
// Formato de la trama tal como viaja por el medio:
//
//	01111110 | Tipo | Sec | Largo | Carga | CRC-16 | 01111110
//
//	- Bandera inicial y final: 01111110
//	- Tipo:  0x01 DATOS, 0x02 ACK, 0x03 NAK
//	- Sec:   numero de secuencia alternado (0 o 1)
//	- Largo: cantidad de bytes de la carga, en 2 bytes
//	- Carga: datos de la capa de red (vacia en ACK y NAK)
//	- CRC:   2 bytes con el CRC-16 de Tipo+Sec+Largo+Carga
//
// El problema clasico: si la carga o el CRC tienen seis unos seguidos, se
// confundirian con la bandera y el receptor no sabria donde termina la trama.
// La solucion es el "bit stuffing": despues de cinco unos seguidos se inserta
// un cero, que el receptor saca cuando lo ve. Asi, dentro de la trama nunca
// hay mas de cinco unos seguidos y la bandera 01111110 es inconfundible.

// bandera es el patron 01111110 que delimita cada trama en el medio fisico.
var bandera = []byte{0, 1, 1, 1, 1, 1, 1, 0}

// Cantidades fijas del formato de la trama, en bits y en bytes.
const (
	// bitsBandera es el largo de la bandera inicial y final.
	bitsBandera = 8
	// bytesEncabezado es Tipo + Sec + Largo (2 bytes).
	bytesEncabezado = 4
	// bytesCRC es el largo del CRC.
	bytesCRC = 2
	// MaxBytesPorTrama es el maximo de bytes de carga que entra en una trama.
	// El campo Largo permitiria hasta 65535, pero el protocolo limita las
	// tramas a este tamaño, parecido a un MTU de red: tramas mas grandes
	// serian mas eficientes, pero con un medio ruidoso la probabilidad de que
	// una trama salga danada crece con la cantidad de bits que viaja, y
	// entonces habria que mandarla muchas veces.
	MaxBytesPorTrama = 1400
	// unosSeguidosMaximos es la cantidad de unos seguidos que se permiten
	// dentro de una trama antes de tener que insertar un cero.
	unosSeguidosMaximos = 5
)

// Errores de la conversion entre trama logica y bits del medio.
var (
	// ErrFaltanBanderas indica que el medio entrego algo que no arranca ni
	// termina con la bandera: el receptor no puede saber donde empieza la
	// trama.
	ErrFaltanBanderas = errors.New("la trama no esta delimitada por banderas 01111110")
	// ErrTramaVacia indica que el medio entrego menos bits que una trama minima.
	ErrTramaVacia = errors.New("la trama esta vacia o es demasiado corta")
	// ErrTramaIncompleta indica que los bits recibidos no forman una trama
	// bien estructurada, por ejemplo porque se perdio un byte en el camino.
	ErrTramaIncompleta = errors.New("la trama esta incompleta o mal formada")
	// ErrCargaDemasiadoGrande indica que se quiso enviar mas bytes de los que
	// entran en una trama.
	ErrCargaDemasiadoGrande = errors.New("la carga supera el maximo de bytes por trama")
	// ErrSecuenciaInvalida indica que el numero de secuencia recibido no es 0
	// ni 1.
	ErrSecuenciaInvalida = errors.New("numero de secuencia invalido")
)

// aplicarStuffing inserta un cero cada vez que encuentra cinco unos seguidos.
// El receptor usa esta funcion al reves (quitarStuffing) para deshacer lo mismo.
func aplicarStuffing(bits []byte) []byte {
	salida := make([]byte, 0, len(bits)+len(bits)/6)
	unosSeguidos := 0
	for _, bit := range bits {
		salida = append(salida, bit)
		if bit&mascaraUltimoBit == 1 {
			unosSeguidos++
			if unosSeguidos == unosSeguidosMaximos {
				// Ya se acumularon cinco unos: se mete un cero de relleno.
				salida = append(salida, 0)
				unosSeguidos = 0
			}
		} else {
			unosSeguidos = 0
		}
	}
	return salida
}

// quitarStuffing saca los ceros que el emisor metio despues de cinco unos
// seguidos. Es exactamente la inversa de aplicarStuffing.
func quitarStuffing(bits []byte) []byte {
	salida := make([]byte, 0, len(bits))
	unosSeguidos := 0
	for _, bit := range bits {
		if unosSeguidos == unosSeguidosMaximos && bit&mascaraUltimoBit == 0 {
			// Este cero es relleno: se descarta y se sigue con el siguiente bit.
			unosSeguidos = 0
			continue
		}
		salida = append(salida, bit)
		if bit&mascaraUltimoBit == 1 {
			unosSeguidos++
		} else {
			unosSeguidos = 0
		}
	}
	return salida
}

// construirBits convierte una trama logica en el flujo de bits que viaja por el
// medio: completa el CRC, arma los campos, aplica el stuffing y enclose todo
// entre las dos banderas. El resultado va empaquetado de a 8 bits, que es como
// lo entrega el medio.
func construirBits(trama Trama) ([]byte, error) {
	if len(trama.Carga) > MaxBytesPorTrama {
		return nil, ErrCargaDemasiadoGrande
	}

	// El CRC tiene que estar calculado sobre los campos de la trama. Si el
	// llamador ya lo completo, se respeta; si no, se calcula ahora.
	if trama.CRC == 0 {
		completarCRC(&trama)
	}

	// Se arma el cuerpo de la trama: Tipo, Sec, Largo, Carga y CRC, todo de a
	// 8 bits, y despues se aplica el stuffing a ese cuerpo ya completo.
	cuerpo := make([]byte, 0, bytesEncabezado+len(trama.Carga)+bytesCRC)
	cuerpo = agregarBits(cuerpo, byte(trama.Tipo))
	cuerpo = agregarBits(cuerpo, trama.Sec)
	cuerpo = agregarBits(cuerpo, byte(len(trama.Carga)>>8))
	cuerpo = agregarBits(cuerpo, byte(len(trama.Carga)))
	for _, valor := range trama.Carga {
		cuerpo = agregarBits(cuerpo, valor)
	}
	cuerpo = agregarBits(cuerpo, byte(trama.CRC>>8))
	cuerpo = agregarBits(cuerpo, byte(trama.CRC))

	// El stuffing se aplica al cuerpo, y las banderas quedan afuera: asi la
	// bandera nunca se confunde con los datos.
	bits := append([]byte{}, bandera...)
	bits = append(bits, aplicarStuffing(cuerpo)...)
	bits = append(bits, bandera...)

	return empaquetarBits(bits), nil
}

// extraerTrama hace el camino inverso: toma los bytes crudos que entrega el
// medio y devuelve la trama logica. Solo se ocupa del enmarcado: no verifica el
// CRC, de eso se encarga el receptor a traves de VerificadorErrores.
//
// Asi como en un protocolo real, el receptor no sabe de antemano donde empieza
// la trama: primero se desarma el flujo a bits sueltos y se busca la primera
// bandera 01111110, que es el punto de sincronizacion. Despues se busca la
// siguiente bandera, y lo que hay entre las dos es el cuerpo de la trama. Asi,
// si el medio agrego bits de relleno al final o entre tramas, no pasa nada.
//
// Si algo no cierra (no hay banderas, el stuffing no deja un numero entero de
// bytes, el largo no coincide con lo recibido) devuelve error, y el receptor
// trata eso como una trama danada.
func extraerTrama(bits []byte) (Trama, error) {
	var trama Trama

	// Una trama minima son dos banderas (16 bits) mas Tipo, Sec, Largo y CRC.
	// El calculo es en bits, porque bits es el numero de BYTES que trae el
	// medio: por eso se multiplica por 8 antes de comparar.
	if cantidadDeBits(len(bits)) < bitsBandera*2+cantidadDeBits(bytesEncabezado+bytesCRC) {
		return trama, ErrTramaVacia
	}

	// 1) Se desarma el flujo de bits del medio. Los bits que sobran del ultimo
	// byte (los de relleno) se van a descartar solos al buscar las banderas.
	flujo := desempaquetarBits(bits, cantidadDeBits(len(bits)))

	// 2) Sincronizacion: se busca la primera bandera, que marca donde empieza la
	// trama.
	inicio := buscarBandera(flujo, 0)
	if inicio < 0 {
		return trama, ErrFaltanBanderas
	}

	// 3) Y se busca la siguiente bandera, que marca donde termina. Si no
	// aparece, la trama esta incompleta: llego el final del medio en medio de
	// una trama.
	fin := buscarBandera(flujo, inicio+bitsBandera)
	if fin < 0 {
		return trama, ErrFaltanBanderas
	}

	// 4) Lo que hay entre las dos banderas es el cuerpo de la trama, con el
	// stuffing todavia puesto.
	cuerpo := quitarStuffing(flujo[inicio+bitsBandera : fin])
	if len(cuerpo) == 0 || len(cuerpo)%8 != 0 {
		return trama, ErrTramaIncompleta
	}
	bytes := make([]byte, len(cuerpo)/8)
	for i := range bytes {
		bytes[i] = bitsAEntero(cuerpo[i*8 : i*8+8])
	}

	// 5) Se leen los campos en el mismo orden en que los escribio el emisor.
	if len(bytes) < bytesEncabezado+bytesCRC {
		return trama, ErrTramaIncompleta
	}
	trama.Tipo = TipoTrama(bytes[0])
	trama.Sec = bytes[1]
	trama.Largo = int(bytes[2])<<8 | int(bytes[3])
	if !trama.Tipo.esValido() {
		return trama, ErrTramaIncompleta
	}
	if trama.Sec > 1 {
		return trama, ErrSecuenciaInvalida
	}
	if trama.Largo > MaxBytesPorTrama {
		return trama, ErrCargaDemasiadoGrande
	}

	// 6) El largo declarado tiene que coincidir exactamente con lo que llego:
	// si no, la trama esta truncada o tiene basura.
	if len(bytes) != bytesEncabezado+trama.Largo+bytesCRC {
		return trama, ErrTramaIncompleta
	}

	trama.Carga = append([]byte{}, bytes[bytesEncabezado:bytesEncabezado+trama.Largo]...)
	trama.CRC = uint16(bytes[len(bytes)-2])<<8 | uint16(bytes[len(bytes)-1])
	return trama, nil
}

// buscarBandera devuelve la posicion de la primera bandera 01111110 que
// aparece en el flujo a partir de la posicion indicada, o -1 si no hay ninguna.
// Es el mecanismo de sincronizacion del receptor.
func buscarBandera(flujo []byte, desde int) int {
	for i := desde; i+bitsBandera <= len(flujo); i++ {
		if sonIguales(flujo[i:i+bitsBandera], bandera) {
			return i
		}
	}
	return -1
}

// sonIguales compara dos pedazos de flujo de bits.
func sonIguales(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i]&mascaraUltimoBit != b[i]&mascaraUltimoBit {
			return false
		}
	}
	return true
}
