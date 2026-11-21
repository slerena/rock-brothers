//go:build wasm

package main

import "syscall/js"

// waitForUserGesture espera al primer clic antes de arrancar el juego.
//
// Hace falta porque en el navegador no se puede crear un AudioContext en
// funcionamiento sin un gesto del usuario: hasta entonces `oto.NewContext` se
// queda esperando y el juego arranca mudo. Además el lienzo necesita un clic
// para recibir el teclado. Así el primer contacto es una pantalla de "clic para
// jugar", que es lo normal en web.
//
// Con ?autostart=1 se salta (lo usan las pruebas automáticas).
func waitForUserGesture() {
	if v := js.Global().Get("__astAutostart"); v.Type() == js.TypeBoolean && v.Bool() {
		return
	}
	done := make(chan struct{})
	var cb js.Func
	cb = js.FuncOf(func(this js.Value, args []js.Value) any {
		close(done)
		cb.Release()
		return nil
	})
	js.Global().Call("addEventListener", "pointerdown", cb)
	<-done
}
