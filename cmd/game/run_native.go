//go:build !wasm

package main

import (
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/theme"

	"rock-brothers/internal/game"
)

// runGame abre el juego en una ventana nativa de Fyne.
func runGame(lang string) {
	a := app.NewWithID("com.pandorafms.asteroids.mp")
	a.Settings().SetTheme(theme.DarkTheme())
	game.Tr(lang)
	game.Open(a, nil)
	a.Run()
}
