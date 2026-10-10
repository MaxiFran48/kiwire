package enlazecapa

import (
	"math/rand"
	"testing"
)

// TestCRCVectoresConocidos compara el CRC calculado contra los valores
// publicados del CRC-16/CCITT-FALSE (polinomio 0x1021, valor inicial 0xFFFF,
// sin reflejo y sin xor final). Si el calculo estuviera mal, estos numeros no
// saldrian.
func TestCRCVectoresConocidos(t *testing.T) {
	casos := []struct {
		nombre   string
		datos    []byte
		esperado uint16
	}{
		// Sin datos no se procesa nada, asi que el CRC queda en el valor
		// inicial.
		{"vacio", []byte{}, 0xFFFF},
		// Estos dos son los vectores publicados del CCITT-FALSE: si el
		// algoritmo esta bien escrito, tienen que dar exactamente esto.
		{"A", []byte("A"), 0xB915},
		{"123456789", []byte("123456789"), 0x29B1},
		{"hola", []byte("hola"), 0xDD4B},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			obtenido := calcularCRC(caso.datos)
			if obtenido != caso.esperado {
				t.Errorf("calcularCRC(%q) = %04X, se esperaba %04X",
					caso.datos, obtenido, caso.esperado)
			}
		})
	}
}

// TestCRCIdaYVuelta muestra que agregar un solo bit a los datos cambia el CRC:
// si no pasara, el receptor no podria distinguir dos tramas distintas.
func TestCRCIdaYVuelta(t *testing.T) {
	base := calcularCRC([]byte("mensaje de la capa de red"))
	for i := 0; i < 8; i++ {
		alterado := append([]byte{}, []byte("mensaje de la capa de red")...)
		alterado[0] ^= 1 << i
		if calcularCRC(alterado) == base {
			t.Errorf("cambiar el bit %d de un byte no cambio el CRC", i)
		}
	}
}

// TestCRCDetectaErroresDeBits es la propiedad que importa: con CRC-16 se
// detectan todos los errores de hasta 16 bits. Se prueban uno, dos, tres y
// cuatro bits alterados, en todas las posiciones.
func TestCRCDetectaErroresDeBits(t *testing.T) {
	azar := rand.New(rand.NewSource(42))
	datos := make([]byte, 200)
	azar.Read(datos)
	crcOriginal := calcularCRC(datos)

	for cantidad := 1; cantidad <= 4; cantidad++ {
		for repeticion := 0; repeticion < 200; repeticion++ {
			alterado := append([]byte{}, datos...)
			// Se alteran "cantidad" bits en posiciones distintas al azar.
			alterados := map[int]bool{}
			for i := 0; i < cantidad; i++ {
				posicion := azar.Intn(len(alterado) * 8)
				for alterados[posicion] {
					posicion = azar.Intn(len(alterado) * 8)
				}
				alterados[posicion] = true
				alterado[posicion/8] ^= 1 << (posicion % 8)
			}
			if calcularCRC(alterado) == crcOriginal {
				t.Fatalf("con %d bits alterados el CRC no cambio: el error pasaria "+
					"desapercibido para la capa de red", cantidad)
			}
		}
	}
}

// TestVerificarTramaRevisaElCRC comprueba que el verificador acepta una trama
// con el CRC bien calculado y rechaza la misma trama con el CRC alterado.
func TestVerificarTramaRevisaElCRC(t *testing.T) {
	trama := Trama{Tipo: TramaDatos, Sec: 1, Carga: []byte("hola capa de red")}
	completarCRC(&trama)

	if !verificarTramaLibre(trama) {
		t.Fatal("una trama recien construida deberia verificar bien")
	}

	trama.CRC ^= 0x0001
	if verificarTramaLibre(trama) {
		t.Fatal("una trama con el CRC alterado no deberia verificar")
	}
}

// TestVerificarTramaDetectaCargaDistinta muestra que cambiar un solo byte de la
// carga invalida el CRC, que es como se detecta que una trama llego danada.
func TestVerificarTramaDetintaCargaDistinta(t *testing.T) {
	original := Trama{Tipo: TramaDatos, Sec: 0, Carga: []byte("datos originales")}
	completarCRC(&original)
	alterada := Trama{Tipo: TramaDatos, Sec: 0, Carga: []byte("datos alterados!!")}
	completarCRC(&alterada)

	if original.CRC == alterada.CRC {
		t.Error("dos cargas distintas produjeron el mismo CRC")
	}
	if verificarTramaLibre(alterada) && original.CRC == alterada.CRC {
		t.Error("la trama alterada deberia fallar la verificacion")
	}
}
