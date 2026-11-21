//go:build !wasm

package game

import (
	"os"
	"strconv"

	"rock-brothers/internal/sim"
)

// Las mismas opciones de render, por entorno, para poder comparar el nativo con
// el navegador en igualdad de condiciones:
//
//	AST_RES=0.5 AST_GLOW=1 ./bin/asteroids
func init() {
	res, glow := 1.0, 3
	if v := os.Getenv("AST_RES"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			res = f
			sim.RenderExplicit = true // AST_RES manda: sin presupuesto de píxeles
		}
	}
	if v := os.Getenv("AST_GLOW"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			glow = n
		}
	}
	sim.ApplyRenderOpts(res, glow)
}
