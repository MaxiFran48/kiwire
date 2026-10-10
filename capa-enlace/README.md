# Capa de enlace de datos — trabajo práctico

Programa en Go que implementa un protocolo de capa de enlace sobre un medio de
transmisión en bruto, tal como pide el enunciado: **dividir los datos en marcos,
transmitirlos en forma secuencial y procesar los acuses de recibo del receptor**,
de modo que la capa de red reciba una línea que parezca libre de errores no
detectados.

El protocolo es **Stop-and-Wait con temporizador, banderas 01111110, bit stuffing
y CRC-16/CCITT**. Resuelve los cuatro problemas que el enunciado menciona:

| Problema | Cómo lo resuelve |
|---|---|
| Marcos dañados | CRC-16 en cada trama. Si no cierra, el receptor la descarta y pide el reenvío con un NAK. |
| Marcos perdidos | Temporizador del emisor: si no llega el ACK dentro del plazo, se reenvía la trama. |
| Marcos duplicados | Número de secuencia alternado (0/1). El receptor descarta la repetida y vuelve a mandar el ACK. |
| Emisor rápido contra receptor lento | Stop-and-Wait: hay una sola trama en vuelo, así que nunca se pueden saturar. |

---

## Cómo compilar y probar

Requiere Go 1.27 o superior. No tiene dependencias externas: solo la biblioteca
estándar.

```bash
go build ./...          # compilar
go test ./...           # 24 pruebas: CRC, enmarcado, medio y protocolo completo
go run . -modo=demo     # la demostración, con las dos puntas en la misma máquina
```

### Demostración con el medio ruidoso

Ahí se ve el protocolo trabajando. Con esta configuración, de las 4 tramas que
viajan: 2 llegan dañadas y el receptor pide el reenvío, 3 llegan duplicadas y se
descartan, y hacen falta 7 reintentos, pero la capa de red recibe los 4290 bytes
iguales a los enviados:

```bash
go run . -modo=demo -prob-error=0.00002 -prob-perdida=0.08 -prob-duplicado=0.15 \
  -retardo=30ms -espera=500ms -reintentos=10 -semilla=1
```

Y para ver el detalle de bits y bytes de cada trama:

```bash
go run . -modo=demo -verboso=true
```

---

## Cómo funciona

### La trama

```
01111110 | Tipo | Sec | Largo (2 bytes) | Carga (0..1400 bytes) | CRC-16 | 01111110
```

- **Tipo**: `0x01` DATOS, `0x02` ACK, `0x03` NAK.
- **Sec**: número de secuencia, alternado entre 0 y 1.
- **Largo**: bytes de la carga, en 2 bytes.
- **CRC**: CRC-16/CCITT (polinomio `0x1021`, valor inicial `0xFFFF`) calculado
  sobre `Tipo + Sec + Largo + Carga`.

Las banderas `01111110` son los límites de la trama, y el **bit stuffing** evita
que los datos los imiten: después de cinco unos seguidos se inserta un cero, así
que dentro de la trama nunca hay seis unos seguidos. El receptor no sabe de
antemano dónde empieza una trama: **busca la bandera** en el flujo de bits y usa
la siguiente como cierre, que es el mecanismo de sincronización de HDLC.

### El diálogo de una trama

```
EMISOR                                        RECEPTOR
   |  DATOS sec=0 CRC=1234                      |
   |-------------------------------------------->|
   |              verifica el CRC, entrega a      |
   |              la capa de red                  |
   |  ACK  sec=0                                 |
   |<--------------------------------------------|
```

Si algo sale mal, en el camino de vuelta hay dos variantes:

- **NAK**: el receptor recibió la trama pero el CRC no cierra, así que pide el
  reenvío en el acto, sin esperar el temporizador.
- **Nada**: la trama (o el ACK) se perdió. Vence el temporizador del emisor y
  reenvía. Cuando esa trama ya había llegado, el receptor la reconoce como
  duplicada por el número de secuencia, no se la vuelve a pasar a la capa de red,
  y le reenvía el ACK para que el emisor deje de insistir.

Después de `-reintentos` intentos sin acuse, el emisor le avisa el error a la
capa de red: **no se da por entregado lo que no llegó**.

---

## Distribución del código

```
main.go                    CLI: -modo=demo|emisor|receptor, elección de medio, bitácora
src/
  TiposDeTrama.go          estructuras compartidas: Trama, TipoTrama, Configuracion
  InterfazCapaRed.go       qué ve la capa de red (Transfiere, Recibir, Cerrar)
  InterfazCapaFisica.go    qué se le exige al medio + utilidades para trabajar con bits
  MedioSimulado.go         canal con error por bit, pérdida, duplicado y retardo
  MedioTCP.go              el mismo protocolo sobre un socket TCP
  MedioSerie.go            el mismo protocolo sobre un puerto serie
  ConversorTramas.go       armado y desarmado de tramas: banderas y bit stuffing
  VerificadorErrores.go    cálculo y verificación del CRC-16
  Emisor.go                Stop-and-Wait: temporizador, reintentos, NAK
  Receptor.go              verificación, descarte de duplicados y acuses
  Registro.go              bitácora con marca de tiempo
  *_test.go                pruebas del CRC, del enmarcado, del medio y del protocolo
```

El medio físico es una interfaz (`InterfazCapaFisica`) con tres
implementaciones. **El protocolo de enlace es exactamente el mismo en las
tres**: no sabe si los bits viajan por un cable, por un socket o por un canal
simulado con errores.

---

## Pruebas entre equipos de verdad

Un proceso hace un solo rol (o manda o escucha). Para las pruebas reales se
corre un proceso en cada máquina.

### Opción A: cable de red, por TCP

En la máquina que **recibe** (el servidor):

```bash
go run . -modo=receptor -medio=tcp -direccion=0.0.0.0:9000 -salida=recibidos.txt
```

En la máquina que **manda** (el cliente), indicando la IP del servidor:

```bash
go run . -modo=emisor -medio=tcp -direccion=192.168.1.50:9000 -datos=datos.txt
```

### Opción B: cable USB-serial o null-modem, por puerto serie

Es la opción más barata y la más parecida a un enlace de punto a punto real: no
hace falta red ni switch, solo el cable entre las dos máquinas.

En la máquina que recibe:

```bash
go run . -modo=receptor -medio=serie -puerto=/dev/ttyUSB0 -baudios=9600 -salida=recibidos.txt
```

En la máquina que manda:

```bash
go run . -modo=emisor -medio=serie -puerto=/dev/ttyUSB0 -baudios=9600 -datos=datos.txt
```

Si las dos máquinas están en la misma, se puede probar con un par de puertos
virtuales que crea `socat`:

```bash
socat -d -d pty,raw,echo=0,link=/tmp/ttyA pty,raw,echo=0,link=/tmp/ttyB
# en una terminal
go run . -modo=receptor -medio=serie -puerto=/dev/pts/4 -salida=recibidos.txt
# en otra
go run . -modo=emisor   -medio=serie -puerto=/dev/pts/5 -datos=datos.txt
```

> El puerto serie se configura con `stty`, que viene en Linux y macOS. No hace
> falta instalar nada. La velocidad tiene que ser la misma en las dos puntas.

Sobre los medios reales, cada trama viaja con un entero de 4 bytes adelante que
indica su largo, y eso hace las veces que hace la bandera: en un medio de bytes
no hay forma de saber dónde termina una trama sin un delimitador.

---

## Parámetros

| Parámetro | Por defecto | Para qué sirve |
|---|---|---|
| `-modo` | `demo` | `demo`, `emisor` o `receptor` |
| `-medio` | `simulado` | `simulado`, `tcp` o `serie` |
| `-verboso` | `false` | Muestra el volcado de bits y bytes de cada trama |
| `-reintentos` | `5` | Intentos por trama antes de darla por perdida |
| `-espera` | `2s` | Temporizador del emisor |
| `-prob-error` | `0` | Probabilidad de que un bit llegue invertido |
| `-prob-perdida` | `0` | Probabilidad de que una trama se pierda entera |
| `-prob-duplicado` | `0` | Probabilidad de que una trama llegue dos veces |
| `-retardo` | `20ms` | Retardo típico del medio simulado |
| `-semilla` | `1` | Semilla del azar: con la misma, la simulación da el mismo resultado |
| `-direccion` | `127.0.0.1:9000` | Dirección TCP del otro extremo |
| `-puerto` | `/dev/ttyUSB0` | Puerto serie |
| `-baudios` | `9600` | Velocidad del puerto serie |
| `-datos` | `datos.txt` | Archivo que manda el emisor |
| `-salida` | `recibidos.txt` | Archivo donde el receptor guarda lo que llegó |

---

## Dos detalles de diseño que conviene entender

**El tamaño de la trama y la tasa de error por bit están atados.** Una trama de
1400 bytes son unos 11 200 bits. Si la probabilidad de error por bit es 0,005,
llegan unos 56 bits dañados por trama, el CRC los detecta todos, y ninguna trama
pasa nunca: el enlace queda inservible. Por eso los reintentos solo tienen sentido
con una tasa de error por bit bastante baja (del orden de 10⁻⁵, que es lo típico en
un medio real). Con `-prob-error=0.01` el programa no entrega datos: falla y lo
dice, que es el comportamiento correcto. Las pruebas del código miden justamente
esto: con el medio ruidoso, la capa de red recibe todo igual, y con el medio
inservible, no recibe nada pero jamás recibe datos alterados.

**El emisor y el receptor no pueden estar activos en el mismo proceso sobre el
mismo medio**, porque competirían por leer el mismo flujo y se masticarían los
acuses. En el modo `demo` la punta A solo manda y la punta B solo recibe; en las
pruebas reales, un proceso por rol.
