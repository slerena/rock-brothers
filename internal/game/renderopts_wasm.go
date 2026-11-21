//go:build wasm

package game

import (
	"syscall/js"

	"rock-brothers/internal/sim"
)

// Opciones de render en el navegador. La resolución NO se configura aquí: en web
// la manda el tamaño del canvas de index.html (?scale=), y el juego pinta
// exactamente ese tamaño. Lo único configurable al vuelo es el halo.
func init() {
	glow := 3
	if v := js.Global().Get("__astGlow"); v.Type() == js.TypeNumber {
		glow = v.Int()
	}
	sim.ApplyRenderOpts(1, glow)
}
