//go:build wasm

package game

import "rock-brothers/internal/sim"

// RunWeb arranca el juego en el navegador con presentación propia: sin ventana
// ni lienzo de Fyne, dibujando en el canvas de index.html y dejando que el CSS
// lo escale a la ventana.
//
// Sin app de Fyne no hay preferencias (el récord pasa a ser de la sesión), pero
// sí se puede usar la fuente embebida del tema, que es lo único que el juego
// necesita de Fyne además de los tipos.
func RunWeb() {
	// La resolución la manda el canvas (index.html), no un presupuesto: si el
	// usuario pide más píxeles, se le dan (y verá el coste en los fps).
	sim.RenderExplicit = true

	g := sim.New()
	wireAudio(g)
	currentGame = g          // hooks de consola (__bench, __entities) e input
	runRAF(g, presentCanvas) // rAF: sincronizado con la pantalla

	// Multijugador: si hay servidor, la partida pasa a ser suya (el bucle deja
	// de simular y solo pinta los snapshots). Si no, se juega en solitario.
	netConnect()
}
