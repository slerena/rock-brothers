// Command game abre el juego.
//
//   - Nativo (macOS/Linux/Windows): ventana de Fyne.
//   - Web (GOOS=js GOARCH=wasm): canvas propio dentro de la página, escalado por
//     CSS. Ver internal/game/present_wasm.go para el porqué.
package main

import "os"

func main() {
	// En el navegador no se puede ni sonar audio ni recibir teclado sin un gesto
	// del usuario, así que se espera al primer clic (pantalla "clic para jugar").
	// En nativo no hace nada.
	waitForUserGesture()

	lang := "es"
	if v := os.Getenv("AST_LANG"); v != "" {
		lang = v
	}
	runGame(lang)
}
