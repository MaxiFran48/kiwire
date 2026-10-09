package enlazecapa

// Control de errores: CRC-16 con polinomio CCITT (x^16 + x^12 + x^5 + 1),
// valor inicial 0xFFFF y sin reflejo final. Es el mismo que usa HDLC, asi que
// es un control de errores real y no una simplificacion.
//
// Idea del CRC: se trata el mensaje como un polinomio con coeficientes en
// GF(2) (donde sumar es un XOR) y se lo divide por el polinomio generador. El
// resto de esa division son los 16 bits que se envian al final de la trama. El
// receptor hace la misma division y si el resto da cero, la trama llego bien.
//
// Capacidad de deteccion: con CRC-16 se detectan todos los errores de hasta 16
// bits de una trama, y siempre se detecta un numero impar de bits erroneos.

// Polinomio generador del CRC-16/CCITT.
const polinomioCRC uint16 = 0x1021

// Valor inicial del registro CRC.
const valorInicialCRC uint16 = 0xFFFF

// VerificadorErrores implementa el control de errores del enlace. Se puede
// usar como funcion libre (calcularCRC) o a traves de esta estructura, que
// guarda la bitacora y el nombre del actor para poder explicar que paso con
// cada trama.
type VerificadorErrores struct {
	registro *Registro
	actor    string
}

// NuevoVerificadorErrores crea un verificador que deja rastro en la bitacora,
// con el nombre del componente que lo usa (EMISOR o RECEPTOR).
func NuevoVerificadorErrores(registro *Registro, actor string) *VerificadorErrores {
	return &VerificadorErrores{registro: registro, actor: actor}
}

// calcularCRC aplica byte a byte el algoritmo de division por polinomio en
// GF(2). Por cada byte se hacen ocho desplazamientos del registro; cada uno
// puede necesitar un XOR con el polinomio.
func calcularCRC(datos []byte) uint16 {
	registro := valorInicialCRC
	for _, valor := range datos {
		// Los ocho bits del byte entran del mas significativo al menos
		// significativo, que es como viaja por el cable.
		registro ^= uint16(valor) << 8
		for i := 0; i < 8; i++ {
			if registro&0x8000 != 0 {
				// El bit mas significativo salio: se descarta y se descuenta
				// el polinomio generador.
				registro = (registro << 1) ^ polinomioCRC
			} else {
				registro <<= 1
			}
		}
	}
	return registro
}

// verificarTrama calcula el CRC de la trama recibida y lo compara con el CRC
// que viaja en ella. Si son distintos devuelve false: la trama llego danada.
func (v *VerificadorErrores) verificarTrama(trama Trama) bool {
	esperado := calcularCRC(camposParaCRC(trama))
	if v.registro != nil {
		if esperado == trama.CRC {
			v.registro.Info(v.actor, "CRC correcto: %04X (trama integra)", esperado)
		} else {
			v.registro.Aviso(v.actor, "CRC incorrecto: recibido %04X, esperado %04X -> trama danada",
				trama.CRC, esperado)
		}
	}
	return esperado == trama.CRC
}

// verificarTramaLibre es la misma comprobacion sin bitacora, para usar en los
// tests.
func verificarTramaLibre(trama Trama) bool {
	return calcularCRC(camposParaCRC(trama)) == trama.CRC
}

// completarCRC calcula y guarda en la trama el CRC de sus campos. Se llama
// antes de convertir la trama en bits para mandarla.
func completarCRC(trama *Trama) {
	trama.Largo = len(trama.Carga)
	trama.CRC = calcularCRC(camposParaCRC(*trama))
}

// camposParaCRC arma los bytes que entran en el calculo del CRC: tipo,
// secuencia, largo y carga. El CRC y las banderas quedan afuera, justamente
// porque el CRC es lo que se quiere comparar.
func camposParaCRC(trama Trama) []byte {
	campos := make([]byte, 0, bytesEncabezado+len(trama.Carga))
	campos = append(campos, byte(trama.Tipo), trama.Sec,
		byte(len(trama.Carga)>>8), byte(len(trama.Carga)))
	campos = append(campos, trama.Carga...)
	return campos
}
