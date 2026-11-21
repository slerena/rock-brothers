package sim

import (
	"math"
	"sync/atomic"
)

// Opciones de render medibles (spike).
//
//   - res:  escala del raster pedida. 1 = resolución del dispositivo (nítido);
//     0.5 = la mitad de píxeles (más rápido, más blando).
//   - glow: pases del halo de fósforo (3 = halo + medio + línea; 1 = solo línea).
//
// En el navegador se leen de window.__astRes / window.__astGlow (que index.html
// rellena desde ?res= y ?glow=) y en nativo de AST_RES / AST_GLOW.
//
// Arena de reserva: mientras la plataforma no ha medido el mundo (el primer
// frame), se asume el tamaño clásico del juego, 640x480.

const (
	ArenaW = 640.0
	ArenaH = 480.0
)

// pixelBudget es el máximo de píxeles por frame del raster. El rasterizado de gg
// es por CPU y cuesta ~5 ms por megapíxel, así que hay que caber en el
// presupuesto de un frame (16 ms) dejando sitio para la simulación y para el
// pintado del navegador. Por debajo de este tope se pinta a resolución de
// dispositivo (nítido); por encima se baja, que es preferible a ir a tirones.
const pixelBudget = 2.2e6

var (
	renderRes  atomic.Int64 // escala pedida × 100
	renderGlow atomic.Int64 // pases del halo (1..3)
	// RenderExplicit es true cuando la escala se ha pedido a mano (?res= /
	// AST_RES): entonces se respeta al pie de la letra, sin presupuesto, para
	// poder comparar nítido contra rápido.
	RenderExplicit bool
	// NetPlay es true cuando la partida la lleva un servidor (multijugador). Lo
	// pone el cliente al conectarse; sirve para no ofrecer cosas que en
	// multijugador no se pueden hacer desde aquí (reiniciar la partida con R).
	NetPlay bool
)

func init() {
	renderRes.Store(100) // por defecto, resolución de dispositivo (nítido)
	renderGlow.Store(3)
}

// ApplyRenderOpts fija las opciones de render (las llama cada plataforma).
func ApplyRenderOpts(res float64, glow int) {
	if res <= 0 || res > 2 {
		res = 1
	}
	renderRes.Store(int64(res*100 + 0.5))
	if glow < 1 {
		glow = 1
	}
	if glow > 3 {
		glow = 3
	}
	renderGlow.Store(int64(glow))
}

// RenderScale devuelve la escala efectiva del raster para un tamaño pedido por
// Fyne (en píxeles de dispositivo), ya recortada al presupuesto de píxeles.
func RenderScale(pw, ph int) float64 {
	s := float64(renderRes.Load()) / 100
	if !RenderExplicit {
		if px := float64(pw) * float64(ph); px > 0 {
			if fits := math.Sqrt(pixelBudget / px); fits < s {
				s = fits
			}
		}
	}
	if s < 0.2 {
		s = 0.2
	}
	return s
}

// glowPasses son los pases del halo de fósforo.
func glowPasses() int { return int(renderGlow.Load()) }
