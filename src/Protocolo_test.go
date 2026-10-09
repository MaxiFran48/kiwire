package enlazecapa

import (
	"bytes"
	"errors"
	"math/rand"
	"testing"
	"time"
)

// TestMedioSimuladoEntregaEnLaOtraPunta comprueba lo basico de un medio de
// transmision: lo que se transmite en una punta aparece en la otra, y nunca
// vuelve a la misma punta.
func TestMedioSimuladoEntregaEnLaOtraPunta(t *testing.T) {
	a, b := NuevoParMedioSimulado(ConfiguracionPorDefecto(), nil)
	defer a.Cerrar()

	trama := Trama{Tipo: TramaDatos, Sec: 0, Carga: []byte("hola")}
	completarCRC(&trama)
	bits, err := construirBits(trama)
	if err != nil {
		t.Fatal(err)
	}

	if err := a.Transmitir(bits); err != nil {
		t.Fatal(err)
	}

	recibido, err := b.Recibir(2 * time.Second)
	if err != nil {
		t.Fatalf("la otra punta no recibio la trama: %v", err)
	}
	if !bytes.Equal(recibido, bits) {
		t.Error("la trama llego distinta de como se mando")
	}

	// Y en la punta que transmitio no hay nada: el medio no devuelve lo que uno
	// mismo manda.
	if _, err := a.Recibir(100 * time.Millisecond); err != ErrTiempoEspera {
		t.Errorf("la punta que transmitio recibio algo: %v", err)
	}
}

// TestMedioSimuladoVenceElPlazoSinDatos: si no llega nada, el medio tiene que
// avisar que se vencio el plazo, porque el emisor lo trata como trama perdida.
func TestMedioSimuladoVenceElPlazoSinDatos(t *testing.T) {
	a, b := NuevoParMedioSimulado(ConfiguracionPorDefecto(), nil)
	defer a.Cerrar()

	inicio := time.Now()
	_, err := b.Recibir(150 * time.Millisecond)
	if err != ErrTiempoEspera {
		t.Fatalf("se esperaba ErrTiempoEspera, se obtuvo %v", err)
	}
	if elapsed := time.Since(inicio); elapsed < 100*time.Millisecond {
		t.Errorf("vencio antes de tiempo: %s", elapsed)
	}
}

// TestMedioSimuladoInvierteBits comprueba que con probabilidad 1 el medio
// entrega la trama alterada: sirve para probar el control de errores.
func TestMedioSimuladoInvierteBits(t *testing.T) {
	cfg := ConfiguracionPorDefecto()
	cfg.ProbErrorBit = 1 // todos los bits danados
	a, b := NuevoParMedioSimulado(cfg, nil)
	defer a.Cerrar()

	bits := []byte{0x0F, 0xF0}
	if err := a.Transmitir(bits); err != nil {
		t.Fatal(err)
	}
	recibido, err := b.Recibir(2 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(recibido, bits) {
		t.Fatal("con ProbErrorBit=1 la trama deberia llegar alterada")
	}
	for i := range recibido {
		if recibido[i]^bits[i] != 0xFF {
			t.Errorf("el byte %d deberia tener todos los bits invertidos", i)
		}
	}
}

// TestMedioSimuladoPierdeTramas: con ProbPerdidaMarco=1 no llega nada, y el
// emisor tiene que enterarse por su temporizador.
func TestMedioSimuladoPierdeTramas(t *testing.T) {
	cfg := ConfiguracionPorDefecto()
	cfg.ProbPerdidaMarco = 1
	a, b := NuevoParMedioSimulado(cfg, nil)
	defer a.Cerrar()

	if err := a.Transmitir([]byte{1, 2, 3}); err != nil {
		t.Fatalf("perder una trama no es un error para el emisor: %v", err)
	}
	if _, err := b.Recibir(200 * time.Millisecond); err != ErrTiempoEspera {
		t.Errorf("no deberia haber llegado nada: %v", err)
	}
}

// TestMedioSimuladoDuplicaTramas: con ProbDuplicadoMarco=1 cada trama llega dos
// veces, que es el caso que el receptor tiene que saber descartar.
func TestMedioSimuladoDuplicaTramas(t *testing.T) {
	cfg := ConfiguracionPorDefecto()
	cfg.ProbDuplicadoMarco = 1
	a, b := NuevoParMedioSimulado(cfg, nil)
	defer a.Cerrar()

	if err := a.Transmitir([]byte{0xAA, 0x55}); err != nil {
		t.Fatal(err)
	}
	for intento := 1; intento <= 2; intento++ {
		if _, err := b.Recibir(2 * time.Second); err != nil {
			t.Fatalf("no llego la copia %d: %v", intento, err)
		}
	}
	if _, err := b.Recibir(100 * time.Millisecond); err != ErrTiempoEspera {
		t.Error("no deberia haber llegado una tercera copia")
	}
}

// TestMedioSimuladoCerrarNoSeDeadloquea: cerrar un extremo cierra el otro, y
// ningun hilo puede quedar esperando para siempre. Si esto se rompe, el
// programa no termina nunca.
func TestMedioSimuladoCerrarNoSeDeadloquea(t *testing.T) {
	a, b := NuevoParMedioSimulado(ConfiguracionPorDefecto(), nil)

	termino := make(chan struct{})
	go func() {
		defer close(termino)
		// Un lector que esta esperando cuando se cierra el medio tiene que
		// despertarse y terminar.
		_, _ = a.Recibir(10 * time.Second)
	}()
	time.Sleep(50 * time.Millisecond)

	// El cierre tiene que terminar rapido: si se traba, el test falla por
	// tiempo y no por contenido.
	inicio := time.Now()
	if err := a.Cerrar(); err != nil {
		t.Errorf("error al cerrar: %v", err)
	}
	if err := b.Cerrar(); err != nil {
		t.Errorf("error al cerrar el otro extremo: %v", err)
	}
	if elapsed := time.Since(inicio); elapsed > 2*time.Second {
		t.Errorf("el cierre tardo %s: hay un deadlock", elapsed)
	}

	select {
	case <-termino:
	case <-time.After(3 * time.Second):
		t.Fatal("el hilo que estaba esperando no se desperto al cerrar el medio")
	}
}

// ---------------------------------------------------------------------------
// Pruebas de extremo a extremo del protocolo
// ---------------------------------------------------------------------------

// TestEnlaceMedioLimpio: sin errores en el medio, todo tiene que llegar una sola
// vez, en orden, y sin un solo reintento.
func TestEnlaceMedioLimpio(t *testing.T) {
	cfg := configDePrueba()
	datos := datosDePrueba(5000)
	origen, destino, recibido, err := correrEnlace(t, cfg, datos)
	if err != nil {
		t.Fatalf("la transferencia fallo: %v", err)
	}

	if !bytes.Equal(recibido, datos) {
		t.Error("el contenido recibido no es igual al enviado")
	}
	enviados, reintentos, perdidas := origen.Emisor.Estadisticas()
	if reintentos != 0 || perdidas != 0 {
		t.Errorf("con un medio limpio no deberia haber reintentos: %d, perdidas: %d",
			reintentos, perdidas)
	}
	if enviados < 4 {
		t.Errorf("se esperaban varias tramas, se mandaron %d", enviados)
	}
	aceptadas, danadas, duplicadas := destino.Receptor.Estadisticas()
	if danadas != 0 || duplicadas != 0 {
		t.Errorf("con un medio limpio no deberia haber tramas danadas (%d) ni duplicadas (%d)",
			danadas, duplicadas)
	}
	if aceptadas != enviados {
		t.Errorf("el receptor acepto %d tramas y el emisor mando %d", aceptadas, enviados)
	}
}

// TestEnlaceMedioRuidoso es la prueba de fondo del TP: con errores por bit,
// perdidas y duplicados, la capa de red igual tiene que recibir todo, en orden y
// sin repeticiones. Afuera de la capa de enlace se ven los datos intactos.
//
// Ojo con el valor de ProbErrorBit: una trama de 1400 bytes son unos 11200 bits,
// asi que con 0.0005 de error por bit llegan unos 5 bits danados por trama y el
// CRC los detecta todos: ninguna trama pasa nunca. Para que el enlace funcione,
// la probabilidad por bit tiene que ser baja (aca 0.00002, o sea un error cada
// 50000 bits); los problemas que se ven venir son las perdidas y los duplicados.
func TestEnlaceMedioRuidoso(t *testing.T) {
	cfg := configDePrueba()
	cfg.ProbErrorBit = 0.00002
	cfg.ProbPerdidaMarco = 0.08
	cfg.ProbDuplicadoMarco = 0.15
	cfg.Semilla = 99

	// Suficiente informacion para que el medio tenga que causar problemas.
	datos := datosDePrueba(12000)
	origen, destino, recibido, err := correrEnlace(t, cfg, datos)
	if err != nil {
		t.Fatalf("la transferencia fallo: %v", err)
	}

	if !bytes.Equal(recibido, datos) {
		t.Fatalf("la capa de red recibio datos distintos de los que se enviaron (%d de %d bytes)",
			len(recibido), len(datos))
	}

	aceptadas, danadas, duplicadas := destino.Receptor.Estadisticas()
	enviados, reintentos, perdidas := origen.Emisor.Estadisticas()

	t.Logf("tramas enviadas: %d, reintentos: %d, perdidas: %d", enviados, reintentos, perdidas)
	t.Logf("aceptadas: %d, danadas: %d, duplicadas: %d", aceptadas, danadas, duplicadas)

	if perdidas != 0 {
		t.Errorf("no deberia haberse perdido ninguna trama a la larga: %d", perdidas)
	}
	// Con esos parametros el medio tiene que haber causado algun problema, si no
	// la prueba no estaria probando nada.
	if danadas == 0 && reintentos == 0 {
		t.Error("el medio no produjo ningun error: la prueba no probaria nada")
	}
	// Lo importante: la capa de red recibio cada byte una sola vez.
	if aceptadas != enviados {
		t.Errorf("el receptor acepto %d tramas y el emisor mando %d", aceptadas, enviados)
	}
}

// TestEnlaceMedioInservible: con una probabilidad de error por bit tan alta que
// ninguna trama puede salir intacta, el enlace no puede garantizar la entrega.
// Lo que tiene que cumplir SIEMPRE es la otra garantia: jamas entregar a la capa
// de red datos que esten alterados. O entrega todo bien, o avisa que fallo.
func TestEnlaceMedioInservible(t *testing.T) {
	cfg := configDePrueba()
	cfg.ProbErrorBit = 0.01
	cfg.MaxIntentos = 5
	cfg.Semilla = 5

	datos := datosDePrueba(2000)
	origen, destino, recibido, err := correrEnlace(t, cfg, datos)
	if err != nil && !errors.Is(err, ErrTramaPerdida) {
		t.Fatalf("se esperaba ErrTramaPerdida o una entrega completa, se obtuvo: %v", err)
	}

	_, danadas, _ := destino.Receptor.Estadisticas()
	enviados, reintentos, perdidas := origen.Emisor.Estadisticas()
	t.Logf("enviadas: %d, reintentos: %d, perdidas: %d, danadas: %d",
		enviados, reintentos, perdidas, danadas)

	if perdidas == 0 {
		t.Skip("con esta semilla el enlace logro entregar todo; la prueba no dice nada")
	}
	if !bytes.Equal(recibido, datos) && len(recibido) > 0 {
		t.Errorf("la capa de red recibio datos que no coinciden con lo enviado: "+
			"el enlace entrego datos alterados (%d de %d bytes)", len(recibido), len(datos))
	}
	if len(recibido) > 0 && reintentos == 0 {
		t.Error("llego algo sin un solo reintento con un medio inservible")
	}
}

// TestEmisorInformaCuandoLaTramaSePierde: si la trama nunca llega y se agotan
// los reintentos, la capa de red tiene que enterarse de que la entrega fallo. No
// se puede dar por entregada algo que no llego.
func TestEmisorInformaCuandoLaTramaSePierde(t *testing.T) {
	cfg := configDePrueba()
	cfg.ProbPerdidaMarco = 1 // no llega nunca nada
	cfg.MaxIntentos = 3

	a, b := NuevoParMedioSimulado(cfg, nil)
	defer a.Cerrar()
	defer b.Cerrar()

	enlace := NuevoEnlace(a, cfg, nil)
	err := enlace.Transfiere([]byte("esto no va a llegar"))
	if err == nil {
		t.Fatal("se esperaba un error de entrega")
	}
	if !errors.Is(err, ErrTramaPerdida) {
		t.Errorf("se esperaba ErrTramaPerdida, se obtuvo: %v", err)
	}
}

// TestReceptorDescartaDuplicados: si el medio entrega dos veces la misma trama,
// la capa de red tiene que ver los datos una sola vez, igual que si el medio
// fuera perfecto.
func TestReceptorDescartaDuplicados(t *testing.T) {
	cfg := configDePrueba()
	cfg.ProbDuplicadoMarco = 1 // cada trama llega dos veces, seguro

	datos := datosDePrueba(2500)
	_, destino, recibido, err := correrEnlace(t, cfg, datos)
	if err != nil {
		t.Fatalf("la transferencia fallo: %v", err)
	}

	if !bytes.Equal(recibido, datos) {
		t.Fatalf("con duplicados, la capa de red recibio %d bytes en vez de %d",
			len(recibido), len(datos))
	}
	_, _, duplicadas := destino.Receptor.Estadisticas()
	if duplicadas == 0 {
		t.Error("con duplicado garantizado deberia haber tramas descartadas por duplicadas")
	}
}

// TestSecuenciaAlternaEntreTramas: el numero de secuencia tiene que alternar
// 0, 1, 0, 1... entre trama y trama, que es lo que permite distinguir una trama
// nueva de un reenvio.
//
// Aca el test hace de receptor a mano: lee las tramas del medio, anota el numero
// de secuencia y contesta el ACK. Asi se ve el comportamiento del emisor desde
// afuera, sin meterse en medio de su logica.
func TestSecuenciaAlternaEntreTramas(t *testing.T) {
	cfg := configDePrueba()
	origen, destino := enlaceParaPruebaDeEmisor(t, cfg)

	// Se manda un bloque que ocupa varias tramas.
	go func() {
		_ = origen.Transfiere(datosDePrueba(3000))
	}()

	for i := 0; i < 3; i++ {
		trama := leerTramaDeDatos(t, destino)
		if esperada := uint8(i % 2); trama.Sec != esperada {
			t.Errorf("la trama %d llego con secuencia %d, se esperaba %d", i+1, trama.Sec, esperada)
		}
		// Se acusa recibo, como haria el receptor del protocolo.
		enviarAcuseDePrueba(t, destino, TramaAcuse, trama.Sec)
	}
}

// TestControlDeFlujoEsperaElAcuse comprueba el Stop-and-Wait: el emisor NO puede
// mandar la segunda trama hasta que la primera fue aprobada. Es el control de
// flujo del protocolo, y lo que impide que un emisor rapido sature a un
// receptor lento.
func TestControlDeFlujoEsperaElAcuse(t *testing.T) {
	cfg := configDePrueba()
	origen, destino := enlaceParaPruebaDeEmisor(t, cfg)

	go func() {
		_ = origen.Transfiere(datosDePrueba(3000))
	}()

	primera := leerTramaDeDatos(t, destino)

	// Sin acuse, no puede llegar nada mas. El plazo es corto a proposito: si
	// el emisor respetara el Stop-and-Wait, no mandaria nada hasta que se
	// venza su temporizador.
	if bits, err := destino.Recibir(100 * time.Millisecond); err != ErrTiempoEspera {
		t.Fatalf("llego una segunda trama sin acuse de la primera: %d bytes", len(bits))
	}

	// Ahora si, con el acuse, el emisor puede seguir.
	enviarAcuseDePrueba(t, destino, TramaAcuse, primera.Sec)
	segunda := leerTramaDeDatos(t, destino)
	if segunda.Sec == primera.Sec {
		t.Error("la segunda trama tiene el mismo numero de secuencia que la primera")
	}
}

// TestReceptorPideReenvioConNak: si la trama llega con el CRC roto, el emisor
// tiene que enterarse por el NAK y reenviarla, sin esperar al temporizador.
func TestReceptorPideReenvioConNak(t *testing.T) {
	cfg := configDePrueba()
	cfg.ProbErrorBit = 1 // todo llega alterado
	cfg.Semilla = 11
	origen, destino := enlaceParaPruebaDeEmisor(t, cfg)

	// El emisor manda en segundo plano y este test va respondiendo NAK a cada
	// trama que llega, como haria el receptor al ver el CRC roto.
	go func() {
		_ = origen.Transfiere(datosDePrueba(1000))
	}()

	reenvios := 0
	var ultimoSec uint8
	plazo := time.Now().Add(3 * time.Second)
	for reenvios < 3 {
		bits, err := destino.Recibir(time.Until(plazo))
		if err != nil {
			t.Fatalf("dejo de llegar tramas: %v", err)
		}

		// Con todos los bits invertidos tampoco se pueden leer las banderas, que
		// es el caso mas pesimista: el receptor no sabe ni siquiera que numero
		// de secuencia tiene la trama, asi que responde el NAK de la trama que
		// estaba esperando, igual que hace el protocolo.
		trama, err := extraerTrama(bits)
		if err == nil {
			if verificarTramaLibre(trama) {
				t.Fatal("una trama con todos los bits alterados no deberia verificar")
			}
			ultimoSec = trama.Sec
		}
		enviarAcuseDePrueba(t, destino, TramaAcuseNegativo, ultimoSec)
		reenvios++
	}
}

// ---------------------------------------------------------------------------
// Ayudantes para las pruebas de extremo a extremo
// ---------------------------------------------------------------------------

// configDePrueba devuelve una configuracion con tiempos cortos, para que las
// pruebas no tarden segundos de mas.
func configDePrueba() Configuracion {
	cfg := ConfiguracionPorDefecto()
	cfg.MaxIntentos = 8
	cfg.TiempoEspera = 300 * time.Millisecond
	cfg.RetardoMedio = 5 * time.Millisecond
	cfg.Semilla = 7
	return cfg
}

// datosDePrueba arma un bloque de datos que incluye bytes de todos los valores,
// para que el CRC y el stuffing trabajen con cualquier contenido.
func datosDePrueba(cantidad int) []byte {
	azar := rand.New(rand.NewSource(1))
	datos := make([]byte, cantidad)
	azar.Read(datos)
	// Se agrega una tanda de 0xFF, que es la que obliga al bit stuffing.
	for i := 0; i < cantidad/4; i++ {
		datos[i] = 0xFF
	}
	return datos
}

// correrEnlace arma un enlace completo sobre un medio simulado, manda los datos
// desde una punta y espera a que lleguen a la otra. Devuelve las dos puntas, lo
// que llego a la capa de red y el error de la transferencia (que puede ser
// ErrTramaPerdida si el medio fue demasiado malo).
func correrEnlace(t *testing.T, cfg Configuracion, datos []byte) (*EnlaceDeNodo, *EnlaceDeNodo, []byte, error) {
	t.Helper()

	medioOrigen, medioDestino := NuevoParMedioSimulado(cfg, nil)
	origen := NuevoEnlace(medioOrigen, cfg, nil)
	destino := NuevoEnlace(medioDestino, cfg, nil)

	// El receptor de destino corre en segundo plano mientras el emisor manda.
	go destino.Receptor.Ejecutar()
	defer medioOrigen.Cerrar()
	defer medioDestino.Cerrar()

	err := origen.Transfiere(datos)

	// Se junta lo que fue llegando a la capa de red del destino.
	var recibido []byte
	ultimo := time.Now()
	for len(recibido) < len(datos) {
		if trozo, hayDatos := destino.Recibir(); hayDatos {
			recibido = append(recibido, trozo...)
			ultimo = time.Now()
			continue
		}
		if time.Since(ultimo) > 2*time.Second {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	return origen, destino, recibido, err
}

// enlaceParaPruebaDeEmisor arma un enlace donde el test hace de receptor a
// mano: devuelve la punta que envia y el medio donde llegan las tramas.
func enlaceParaPruebaDeEmisor(t *testing.T, cfg Configuracion) (*EnlaceDeNodo, *MedioSimulado) {
	t.Helper()
	medioOrigen, medioDestino := NuevoParMedioSimulado(cfg, nil)
	origen := NuevoEnlace(medioOrigen, cfg, nil)
	t.Cleanup(func() {
		medioOrigen.Cerrar()
		medioDestino.Cerrar()
	})
	return origen, medioDestino
}

// leerTramaDeDatos espera la proxima trama de datos que llega por el medio.
func leerTramaDeDatos(t *testing.T, medio *MedioSimulado) Trama {
	t.Helper()
	plazo := time.Now().Add(5 * time.Second)
	for {
		bits, err := medio.Recibir(time.Until(plazo))
		if err != nil {
			t.Fatalf("no llego ninguna trama de datos: %v", err)
		}
		trama, err := extraerTrama(bits)
		if err != nil {
			t.Fatalf("llego una trama ilegible: %v", err)
		}
		if trama.Tipo == TramaDatos {
			return trama
		}
		if time.Now().After(plazo) {
			t.Fatal("no llego ninguna trama de datos antes de tiempo")
		}
	}
}

// enviarAcuseDePrueba manda un ACK o un NAK desde el medio del receptor.
func enviarAcuseDePrueba(t *testing.T, medio *MedioSimulado, tipo TipoTrama, sec uint8) {
	t.Helper()
	acuse := Trama{Tipo: tipo, Sec: sec}
	completarCRC(&acuse)
	bits, err := construirBits(acuse)
	if err != nil {
		t.Fatal(err)
	}
	if err := medio.Transmitir(bits); err != nil {
		t.Fatalf("no se pudo mandar el acuse: %v", err)
	}
}
