package game

import "rock-brothers/internal/sim"

// currentGame es la partida en curso: la usan los hooks de consola (wasm) y el
// teclado del DOM, que no tiene forma de recibir la instancia.
var currentGame *sim.Sim

// sendToServer, si no es nil, se lleva el input al servidor (multijugador). Lo
// pone el cliente web cuando consigue conectarse; en solitario se queda nil y el
// input va directo a la simulación local.
var sendToServer func(name string, down bool)

// dispatchKey entrega una tecla al juego por su nombre (los mismos nombres que
// usa Fyne: "Left", "Space", "R"...).
//
// En el navegador lo usa input_wasm.go, que lee el teclado directamente del DOM
// porque el driver web de Fyne no traduce las letras; en nativo no se usa (ahí
// Fyne entrega los eventos correctamente). En multijugador, aquí es donde el
// input sale hacia el servidor.
func dispatchKey(name string, down bool) {
	if name == "" {
		return
	}
	if sendToServer != nil {
		sendToServer(name, down)
		return
	}
	g := currentGame
	if g == nil {
		return
	}
	if down {
		g.KeyDown(name)
		return
	}
	g.KeyUp(name)
}
