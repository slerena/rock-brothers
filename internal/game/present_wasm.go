//go:build wasm

package game

import (
	"syscall/js"
	"time"

	"rock-brothers/internal/sim"
)

var (
	canvasEl js.Value
	canvasCx js.Value
	canvasD  *canvasDrawer // reutilizado mientras el tamaño no cambie
	canvasW  int
	canvasH  int
)

// ---------- bucle de presentación (web) ----------

// runRAF presenta con requestAnimationFrame en vez de con un ticker de Go: el
// navegador lo sincroniza con la pantalla, mientras que los timers de Go en
// WebAssembly son irregulares y se notaban como tirones.
//
// En solitario avanza la simulación con dt FIJO (0.016) acumulando el tiempo
// real, para que la velocidad no dependa de los Hz de la pantalla. En
// multijugador no simula: solo pinta lo que llega por la red.
//
// No bloquea: agenda el primer frame y vuelve.
func runRAF(g *sim.Sim, present func(*sim.Sim)) {
	step := 0.016
	var last time.Time
	var acc float64
	var cb js.Func
	cb = js.FuncOf(func(this js.Value, args []js.Value) any {
		now := time.Now()
		if !last.IsZero() {
			d := now.Sub(last).Seconds()
			if remoteSim() {
				// Multijugador: el estado es del servidor, así que entre snapshots
				// se extrapola el mundo y se predice tu nave.
				netTick(g, d)
			} else {
				acc += d
				for n := 0; acc >= step && n < 5; n++ {
					g.Step(step)
					acc -= step
				}
				if acc > step*5 {
					acc = 0 // tras un parón largo (pestaña oculta), no acelerar
				}
			}
		}
		last = now
		present(g)
		js.Global().Call("requestAnimationFrame", cb)
		return nil
	})
	js.Global().Call("requestAnimationFrame", cb)
}

// setupCanvas localiza el canvas y su contexto. Devuelve false mientras la
// página no esté lista (el juego puede arrancar antes que el DOM).
func setupCanvas() bool {
	if canvasEl.Type() == js.TypeObject {
		return true
	}
	doc := js.Global().Get("document")
	if doc.Type() != js.TypeObject {
		return false
	}
	el := doc.Call("getElementById", "game")
	if el.Type() != js.TypeObject {
		return false
	}
	cx := el.Call("getContext", "2d")
	if cx.Type() != js.TypeObject {
		return false
	}
	canvasEl, canvasCx = el, cx
	return true
}

// presentCanvas dibuja el frame directamente en el canvas del navegador.
//
// La escena se dibuja con la API de canvas 2D (draw_canvas_wasm.go), que
// rasteriza la GPU: es nítida a cualquier resolución y no consume CPU como el
// rasterizado por píxel de gg (~10 ms por megapíxel, que saturaba el hilo del
// navegador y colgaba la pestaña).
//
// El tamaño del lienzo lo fija index.html (640x480 por la escala elegida) y no
// cambia con la ventana: el CSS lo estira y el coste por frame es constante.
func presentCanvas(g *sim.Sim) {
	if !setupCanvas() {
		return
	}
	w := canvasEl.Get("width").Int()
	h := canvasEl.Get("height").Int()
	if w < 2 || h < 2 {
		return
	}
	if canvasD == nil || canvasW != w || canvasH != h {
		canvasD = newCanvasDrawer(canvasCx, w, h)
		canvasW, canvasH = w, h
	}
	// La transformación y el path del contexto PERSISTEN entre frames, y
	// DrawFrame aplica translate+scale: sin resetear se multiplicarían en cada
	// frame y el dibujo acabaría fuera del lienzo. (En nativo no pasa: gg se crea
	// nuevo en cada frame.)
	canvasCx.Call("setTransform", 1, 0, 0, 1, 0, 0)
	canvasCx.Call("beginPath")

	// El mundo son unidades. En solitario, las CSS del lienzo (cada uno juega en
	// su ventana), para que las naves y el HUD tengan su tamaño de siempre. En
	// multijugador lo fija el servidor: el mismo campo para todos, y el lienzo se
	// escala para llenarlo. El lienzo tiene más píxeles (device) que se dibujan
	// escalados: subir ?scale solo da más definición, no agranda el juego.
	ww := canvasEl.Get("clientWidth").Float()
	wh := canvasEl.Get("clientHeight").Float()
	if ww < 8 || wh < 8 {
		ww, wh = sim.ArenaW, sim.ArenaH
	}
	if w2, h2 := g.WorldSize(); remoteSim() && w2 > 0 && h2 > 0 {
		ww, wh = w2, h2
	}
	g.DrawFrame(canvasD, w, h, ww, wh)
}
