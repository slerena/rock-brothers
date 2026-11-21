//go:build wasm

package main

import (
	"time"

	"rock-brothers/internal/game"
)

// runGame arranca el juego con presentación propia (canvas del navegador).
//
// main se queda esperando con un Sleep largo y NO con `select {}`: en WebAssembly
// un select vacío deja la goroutine bloqueada *sin fuente de despertar* y el
// planificador del runtime se queda girando (renderer al 100% y pestaña colgada).
func runGame(lang string) {
	game.Tr(lang)
	game.RunWeb()
	for {
		time.Sleep(time.Hour)
	}
}
