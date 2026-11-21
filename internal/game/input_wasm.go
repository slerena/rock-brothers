//go:build wasm

package game

import (
	"strings"
	"syscall/js"
)

// El driver web de Fyne no traduce las LETRAS: su keyToName() devuelve
// KeyUnknown para A-Z, así que M, A, L, R, S, P y B no llegaban nunca al juego.
// Las flechas, espacio y enter sí están mapeadas.
//
// Aquí se escucha el teclado directamente del DOM. Ojo: NO se corta la
// propagación de los eventos (nada de stopImmediatePropagation) porque eso se
// comía el keyup que oto necesita para reanudar el AudioContext: el juego se
// quedaba mudo. Como en el cliente web no hay ventana de Fyne, tampoco hay nadie
// más escuchando, así que no hay duplicados.
//
// Es, además, el sitio natural del input del multijugador: aquí es donde se
// traduce a comandos que viajan al servidor.
func init() {
	doc := js.Global().Get("document")
	if doc.Type() != js.TypeObject {
		return
	}
	listen := func(event string, down bool) {
		doc.Call("addEventListener", event, js.FuncOf(func(this js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			ev := args[0]
			if name := keyNameFor(ev); name != "" {
				ev.Call("preventDefault") // que las flechas no hagan scroll
				// OJO: NO se llama a stopImmediatePropagation. Aunque parezca
				// inofensivo, se comía el keyup que oto necesita para reanudar el
				// AudioContext (el navegador no deja sonar sin un gesto), y el
				// juego se quedaba mudo. Aquí ya no hay ventana de Fyne que pueda
				// recibir la tecla por duplicado, así que no hace falta.
				dispatchKey(name, down)
			}
			return nil
		}))
	}
	listen("keydown", true)
	listen("keyup", false)
}

// keyNameFor traduce un KeyboardEvent al nombre de tecla de Fyne. Se usa
// event.code (posición física) para que no dependa del idioma del teclado.
func keyNameFor(ev js.Value) string {
	code := ev.Get("code").String()
	switch code {
	case "ArrowLeft":
		return "Left"
	case "ArrowRight":
		return "Right"
	case "ArrowUp":
		return "Up"
	case "ArrowDown":
		return "Down"
	case "Space":
		return "Space"
	case "Enter", "NumpadEnter":
		return "Return"
	case "Escape":
		return "Escape"
	}
	if len(code) == 4 && strings.HasPrefix(code, "Key") {
		return code[3:] // "KeyM" -> "M"
	}
	return ""
}
