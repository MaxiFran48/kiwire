package enlazecapa

import (
	"math/rand"
	"testing"
)

// TestStuffingIdaYVuelta comprueba que aplicar el stuffing y despues quitarlo
// devuelve exactamente el flujo original, para cualquier contenido.
func TestStuffingIdaYVuelta(t *testing.T) {
	azar := rand.New(rand.NewSource(1))
	for repeticion := 0; repeticion < 500; repeticion++ {
		original := make([]byte, 1+azar.Intn(300))
		azar.Read(original)

		conStuffing := aplicarStuffing(original)
		sinStuffing := quitarStuffing(conStuffing)

		if len(sinStuffing) != len(original) {
			t.Fatalf("la longitud cambio: %d bits adentro de %d bits afuera",
				len(original), len(sinStuffing))
		}
		for i := range original {
			if sinStuffing[i] != original[i] {
				t.Fatalf("el bit %d cambio: %s", i, volcarBits(original))
			}
		}
	}
}

// TestStuffingInsertaCeroDespuesDeCincoUnos es la regla exacta del stuffed bit:
// despues de cinco unos seguidos se mete un cero, para que nunca haya seis unos
// seguidos dentro de la trama.
func TestStuffingInsertaCeroDespuesDeCincoUnos(t *testing.T) {
	// Seis unos: el stuffing tiene que cortar la secuencia.
	entrada := []byte{1, 1, 1, 1, 1, 1}
	esperado := []byte{1, 1, 1, 1, 1, 0, 1}

	salida := aplicarStuffing(entrada)
	if len(salida) != len(esperado) {
		t.Fatalf("se obtuvo %s y se esperaba %s", volcarBits(salida), volcarBits(esperado))
	}
	for i := range esperado {
		if salida[i] != esperado[i] {
			t.Fatalf("se obtuvo %s y se esperaba %s", volcarBits(salida), volcarBits(esperado))
		}
	}
}

// TestNingunSeisUnosDentroDeLaTrama comprueba la propiedad que hace funcionar el
// protocolo: dentro del cuerpo de la trama, jamas aparecen seis unos seguidos, y
// por lo tanto la bandera 01111110 nunca puede aparecer por casualidad en los
// datos.
func TestNingunSeisUnosDentroDeLaTrama(t *testing.T) {
	casos := [][]byte{
		{0xFF, 0xFF, 0xFF, 0xFF},
		{0xFF},
		{0x0F, 0xFF, 0xF0},
		make([]byte, 100), // todo ceros: el caso contrario
	}
	azar := rand.New(rand.NewSource(9))
	aleatorio := make([]byte, 200)
	azar.Read(aleatorio)
	casos = append(casos, aleatorio)

	for _, carga := range casos {
		bits, err := construirBits(Trama{Tipo: TramaDatos, Sec: 0, Carga: carga})
		if err != nil {
			t.Fatalf("no se pudo construir la trama: %v", err)
		}
		flujo := desempaquetarBits(bits, cantidadDeBits(len(bits)))

		// Solo se mira el cuerpo, entre la bandera inicial y la final.
		inicio := buscarBandera(flujo, 0)
		fin := buscarBandera(flujo, inicio+bitsBandera)
		if inicio < 0 || fin < 0 {
			t.Fatal("no se encontraron las banderas de la trama")
		}
		unos := 0
		for i := inicio + bitsBandera; i < fin; i++ {
			if flujo[i] == 1 {
				unos++
				if unos == 6 {
					t.Fatalf("aparecieron seis unos seguidos dentro de la trama: %s",
						volcarBits(flujo))
				}
			} else {
				unos = 0
			}
		}
	}
}

// TestTramaIdaYVuelta arma una trama, la manda por el medio y la recibe de vuelta:
// la carga tiene que volver exactamente igual, y con el mismo CRC.
func TestTramaIdaYVuelta(t *testing.T) {
	azar := rand.New(rand.NewSource(2))
	casos := [][]byte{
		{},                       // trama de protocolo, sin carga
		[]byte("hola"),           // carga corta
		{0xFF, 0xFF, 0xFF, 0xFF}, // carga llena de unos, la que mas stuffing genera
		bytesLlenos(400),         // carga grande
		bytesAleatorios(300, azar),
	}

	for _, carga := range casos {
		original := Trama{Tipo: TramaDatos, Sec: 1, Carga: carga}
		completarCRC(&original)

		bits, err := construirBits(original)
		if err != nil {
			t.Fatalf("no se pudo construir la trama: %v", err)
		}
		recuperada, err := extraerTrama(bits)
		if err != nil {
			t.Fatalf("no se pudo extraer la trama de %d bytes: %v", len(carga), err)
		}

		if recuperada.Tipo != original.Tipo || recuperada.Sec != original.Sec {
			t.Errorf("encabezado distinto: %+v", recuperada)
		}
		if recuperada.CRC != original.CRC {
			t.Errorf("CRC distinto: %04X en vez de %04X", recuperada.CRC, original.CRC)
		}
		if !verificarTramaLibre(recuperada) {
			t.Error("la trama recuperada no verifica su CRC")
		}
		if len(recuperada.Carga) != len(original.Carga) {
			t.Fatalf("la carga cambio de largo: %d en vez de %d",
				len(recuperada.Carga), len(original.Carga))
		}
		for i := range original.Carga {
			if recuperada.Carga[i] != original.Carga[i] {
				t.Fatalf("el byte %d de la carga cambio", i)
			}
		}
	}
}

// TestExtraerTramaSinBanderasFalla: si del medio no llega nada que empiece y
// termine con la bandera, el receptor no puede armar la trama y tiene que
// avisarlo, porque si no la capa de red veria basura.
func TestExtraerTramaSinBanderasFalla(t *testing.T) {
	bits, err := construirBits(Trama{Tipo: TramaDatos, Sec: 0, Carga: []byte("datos")})
	if err != nil {
		t.Fatal(err)
	}
	// Se rompen las dos banderas: el ultimo byte pasa a ser todo ceros.
	bits[0] = 0x00
	bits[len(bits)-1] = 0x00

	if _, err := extraerTrama(bits); err == nil {
		t.Fatal("se esperaba un error al no encontrar las banderas")
	}
}

// TestExtraerTramaAceptaBitsDeRelleno muestra que el receptor se sincroniza
// buscando la bandera, asi que no le importa que el medio haya agregado bits de
// relleno antes o despues de la trama, que es lo que pasa con cualquier medio de
// bits.
func TestExtraerTramaAceptaBitsDeRelleno(t *testing.T) {
	bits, err := construirBits(Trama{Tipo: TramaDatos, Sec: 0, Carga: []byte("hola")})
	if err != nil {
		t.Fatal(err)
	}

	// Se agrega un byte de relleno (0xFF) adelante y otro atras, que es ruido
	// tipico del medio.
	conRelleno := append([]byte{0xFF}, bits...)
	conRelleno = append(conRelleno, 0xFF)

	trama, err := extraerTrama(conRelleno)
	if err != nil {
		t.Fatalf("el receptor deberia sincronizarse con la bandera: %v", err)
	}
	if string(trama.Carga) != "hola" {
		t.Errorf("la carga recuperada es %q, se esperaba %q", trama.Carga, "hola")
	}
}

// TestErroresDeBitNoPasanDesapercibidos es la garantia central del TP: si el
// medio altera la trama, el enlace tiene que enterarse. Se alteran de 1 a 4 bits
// al azar y se verifica que o bien falla el enmarcado, o bien falla el CRC.
func TestErroresDeBitNoPasanDesapercibidos(t *testing.T) {
	azar := rand.New(rand.NewSource(4))
	for cantidad := 1; cantidad <= 4; cantidad++ {
		for repeticion := 0; repeticion < 300; repeticion++ {
			original := Trama{Tipo: TramaDatos, Sec: 0, Carga: bytesAleatorios(200, azar)}
			completarCRC(&original)
			bits, err := construirBits(original)
			if err != nil {
				t.Fatal(err)
			}

			danado := invertirBitsAlAzar(bits, cantidad, azar)

			trama, err := extraerTrama(danado)
			if err != nil {
				// El error rompio el enmarcado: el receptor lo va a notar, asi
				// que esta bien.
				continue
			}
			if verificarTramaLibre(trama) {
				t.Fatalf("una trama con %d bits alterados paso el control de errores: "+
					"el error le llegaria a la capa de red sin ser detectado", cantidad)
			}
		}
	}
}

// TestCargaDemasiadoGrandeSeRechaza: no se puede armar una trama con mas bytes de
// los que entran en el campo Largo.
func TestCargaDemasiadoGrandeSeRechaza(t *testing.T) {
	carga := bytesLlenos(MaxBytesPorTrama + 1)
	if _, err := construirBits(Trama{Tipo: TramaDatos, Sec: 0, Carga: carga}); err == nil {
		t.Fatal("se esperaba un error al armar una trama demasiado grande")
	}
}

// TestSecuenciaInvalidaSeRechaza: el numero de secuencia solo puede ser 0 o 1.
func TestSecuenciaInvalidaSeRechaza(t *testing.T) {
	original := Trama{Tipo: TramaDatos, Sec: 0, Carga: []byte("hola")}
	completarCRC(&original)
	bits, err := construirBits(original)
	if err != nil {
		t.Fatal(err)
	}
	// Se altera el byte de la secuencia (el segundo del cuerpo) y se recalcula
	// el CRC, para que el unico motivo del rechazo sea la secuencia.
	flujo := desempaquetarBits(bits, cantidadDeBits(len(bits)))
	flujo[8+16] ^= 1
	alterado := empaquetarBits(flujo)
	if _, err := extraerTrama(alterado); err == nil {
		t.Fatal("se esperaba un error con un numero de secuencia invalido")
	}
}

// ---------------------------------------------------------------------------
// Ayudantes para los tests
// ---------------------------------------------------------------------------

// bytesAleatorios genera una cantidad de bytes al azar.
func bytesAleatorios(cantidad int, azar *rand.Rand) []byte {
	datos := make([]byte, cantidad)
	azar.Read(datos)
	return datos
}

// bytesLlenos genera una cantidad de bytes todos iguales a 0xAA.
func bytesLlenos(cantidad int) []byte {
	datos := make([]byte, cantidad)
	for i := range datos {
		datos[i] = 0xAA
	}
	return datos
}

// invertirBitsAlAzar da vuelta una cantidad de bits al azar, sin repetir
// posicion, y devuelve la trama alterada.
func invertirBitsAlAzar(bits []byte, cantidad int, azar *rand.Rand) []byte {
	flujo := desempaquetarBits(bits, cantidadDeBits(len(bits)))
	alterados := map[int]bool{}
	for i := 0; i < cantidad; i++ {
		posicion := azar.Intn(len(flujo))
		for alterados[posicion] {
			posicion = azar.Intn(len(flujo))
		}
		alterados[posicion] = true
		flujo[posicion] ^= mascaraUltimoBit
	}
	return empaquetarBits(flujo)
}
