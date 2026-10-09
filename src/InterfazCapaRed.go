package enlazecapa

// InterfazCapaRed es lo que el protocolo de enlace le ofrece a la capa de red.
// La capa de red no sabe nada de tramas, de CRC ni de reintentos: simplemente
// le pasa datos y espera que lleguen al otro lado, en orden y sin repetirse.
// Cualquiera que use este protocolo como capa de enlace implementa esta
// interfaz.
type InterfazCapaRed interface {
	// Transfiere entrega al enlace los datos destinedos a la otra maquina.
	// Devuelve error si la entrega no se pudo completar (por ejemplo, si se
	// agotaron los reintentos de alguna trama).
	Transfiere(datos []byte) error

	// Recibir entrega a la capa de red los datos que llegaron de la otra
	// maquina. Si no hay datos, devuelve false para que el llamador siga
	// esperando.
	Recibir() ([]byte, bool)

	// Cerrar libera los recursos de la capa de enlace.
	Cerrar() error
}

// enlace es la implementacion de la capa de enlace de datos: une la capa de
// red con la capa fisica usando el protocolo Stop-and-Wait con CRC-16 y acuses
// de recibo.
type enlace struct {
	emisor   *Emisor
	receptor *Receptor
	medio    InterfazCapaFisica
}

// EnlaceDeNodo es la capa de enlace completa de una punta de la comunicacion.
//
// Se puede usar como cualquier InterfazCapaRed, y ademas expone el receptor
// para ponerlo a trabajar en segundo plano. La separacion es a proposito: la
// capa de red solo ve la interfaz, y quien arma el nodo (main.go) es el que
// decide en que momento arranca el ciclo de recepcion.
type EnlaceDeNodo struct {
	InterfazCapaRed
	Emisor   *Emisor
	Receptor *Receptor
}

// NuevoEnlace arma la capa de enlace de una punta sobre el medio fisico indicado.
func NuevoEnlace(medio InterfazCapaFisica, cfg Configuracion, registro *Registro) *EnlaceDeNodo {
	emisor := NuevoEmisor(medio, cfg)
	receptor := NuevoReceptor(medio, cfg)
	emisor.UsarRegistro(registro)
	receptor.UsarRegistro(registro)
	return &EnlaceDeNodo{
		InterfazCapaRed: &enlace{emisor: emisor, receptor: receptor, medio: medio},
		Emisor:          emisor,
		Receptor:        receptor,
	}
}

// Transfiere cumple el contrato de la capa de red: delega en el emisor.
func (e *enlace) Transfiere(datos []byte) error {
	return e.emisor.Transfiere(datos)
}

// Recibir cumple el contrato de la capa de red: delega en el receptor, que ya
// se encarga de verificar el CRC y de descartar los duplicados.
func (e *enlace) Recibir() ([]byte, bool) {
	return e.receptor.Recibir()
}

// Cerrar cierra el medio fisico que comparten ambas mitades del enlace.
func (e *enlace) Cerrar() error {
	return e.medio.Cerrar()
}
