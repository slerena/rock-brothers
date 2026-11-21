// Package proto define el protocolo entre el servidor de juego y el cliente
// (navegador). Es a propósito minúsculo y sin dependencias: lo importan los dos
// lados.
//
// Todo viaja en tramas BINARIAS de WebSocket cuyo primer byte es el tipo de
// mensaje y el resto la carga. Los mensajes de control son JSON (legibles al
// depurar); el snapshot del mundo son bytes opacos de internal/sim.
package proto

import "encoding/json"

// Tipos de mensaje: el primer byte de cada trama.
const (
	// Cliente → servidor
	MsgJoin  = 0x10 // JSON Join
	MsgReady = 0x11 // JSON Ready (estoy listo para empezar)
	MsgPong  = 0x12 // JSON Ping (respuesta al ping del servidor)
	MsgName  = 0x13 // JSON Name (cambio mi nombre)
	MsgPose  = 0x16 // JSON Pose (dónde y hacia dónde está mi nave, para disparar desde ahí)
	MsgBot   = 0x17 // JSON Bot (añadir/ quitar un bot de pruebas)
	MsgMode  = 0x14 // JSON Mode (cambio el tipo de partida)
	MsgArena = 0x15 // JSON Arena (cambio la arena: asteroides / campo fijo)

	// Servidor → cliente
	MsgHello    = 0x01 // JSON Hello (al entrar: quién eres y quién más hay)
	MsgInput    = 0x02 // JSON Input (el servidor lo reenvía a la simulación)
	MsgSnapshot = 0x03 // bytes: snapshot del mundo (internal/sim)
	MsgRoster   = 0x04 // JSON Roster (alguien entra o sale)
	MsgBye      = 0x05 // JSON Bye (el servidor te echa: sala llena, etc.)
	MsgSounds   = 0x06 // JSON Sounds (sonidos que ha disparado la simulación)
	MsgState    = 0x07 // JSON State (fase de la sala: espera, cuenta atrás, jugando)
	MsgPing     = 0x08 // JSON Ping (el servidor mide la latencia)
)

// Fases de la sala.
const (
	PhaseLobby     = "lobby"     // esperando a que todos digan "listo"
	PhaseCountdown = "countdown" // todos listos: cuenta atrás para empezar
	PhasePlaying   = "playing"   // partida en curso
	PhaseOver      = "over"      // partida acabada: se enseña la tabla de puntuaciones
)

// Modos de partida.
const (
	ModeCoop = "coop"
	ModePvP  = "pvp"
)

// PlayerInfo identifica a un jugador dentro de la sala, con su estado en la
// sala de espera (listo o no) y su latencia medida por el servidor.
type PlayerInfo struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
	Lag   int    `json:"lag"` // milisegundos de ida y vuelta
}

// Join es el primer mensaje del cliente.
type Join struct {
	Name string `json:"name"`
	Mode string `json:"mode"` // "coop" | "pvp"; vacío = coop
}

// Hello dice al cliente quién es y quién más está en la sala.
type Hello struct {
	You     int          `json:"you"`
	Mode    string       `json:"mode"`
	Players []PlayerInfo `json:"players"`
}

// Input es una pulsación o suelta de tecla (mismos nombres que en sim).
type Input struct {
	Key  string `json:"key"`
	Down bool   `json:"down"`
}

// Roster es la lista de jugadores de la sala.
type Roster struct {
	Players []PlayerInfo `json:"players"`
}

// Bye explica por qué el servidor cierra la conexión.
type Bye struct {
	Reason string `json:"reason"`
}

// Sound es un sonido que la simulación del servidor ha disparado y que el
// cliente debe reproducir: aquí no corre la simulación, así que los sonidos
// viajan por la red.
type Sound struct {
	N string `json:"n"` // nombre del sonido (los del banco compartido)
	G int    `json:"g"` // separación mínima respecto al anterior, en ms
}

// Sounds es la lista de sonidos de un intervalo (suele ser corta o vacía).
type Sounds struct {
	List []Sound `json:"sounds"`
}

// Ready dice si el jugador está listo para empezar la partida.
type Ready struct {
	Ready bool `json:"ready"`
}

// Name cambia el nombre visible del jugador (se edita en la sala de espera).
type Name struct {
	Name string `json:"name"`
}

// State es el estado de la sala: en qué fase está, de qué tipo es la partida,
// cuánto queda de cuenta atrás y quiénes están (con su "listo" y su latencia).
type State struct {
	Phase     string       `json:"phase"`     // lobby | countdown | playing
	Mode      string       `json:"mode"`      // coop | pvp
	Arena     string       `json:"arena"`     // rocks (asteroides) | fixed (campo fijo)
	Countdown float64      `json:"countdown"` // segundos restantes
	Players   []PlayerInfo `json:"players"`
}

// Mode cambia el tipo de partida de la sala (cooperativo o todos contra todos).
type Mode struct {
	Mode string `json:"mode"`
}

// Bot pide añadir (o quitar) un bot de pruebas en la sala.
type Bot struct {
	Add   bool `json:"add"`
	Level int  `json:"level"` // 0 torpe, 1 medio, 2 bueno
}

// Pose es la posición y el ángulo que el cliente ve en SU nave. El servidor la
// usa para que las balas salgan de donde él ve la nave (y no de la copia un poco
// atrasada del servidor). Se valida: si se aleja demasiado de la del servidor, se
// ignora.
type Pose struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	A float64 `json:"a"`
}

// Arena cambia la arena de la partida (asteroides que se mueven o campo fijo con
// estructuras que se rompen a tiros).
type Arena struct {
	Arena string `json:"arena"`
}

// Ping lo manda el servidor para medir la latencia; el cliente lo devuelve tal
// cual (Pong) y el servidor calcula el ida y vuelta.
type Ping struct {
	T int64 `json:"t"`
}

// Frame empaqueta un tipo de mensaje con su carga.
func Frame(typ byte, payload []byte) []byte {
	b := make([]byte, 1+len(payload))
	b[0] = typ
	copy(b[1:], payload)
	return b
}

// JSON empaqueta un valor como mensaje JSON del tipo dado.
func JSON(typ byte, v any) []byte {
	b, err := json.Marshal(v)
	if err != nil { // no debería pasar con nuestros tipos
		b = []byte("{}")
	}
	return Frame(typ, b)
}
