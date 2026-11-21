package game

import "fyne.io/fyne/v2"

// presentNative entrega el frame ya dibujado repintando el widget de Fyne. Es la
// vía nativa (macOS/Linux/Windows).
//
// En el navegador se sustituye por el volcado a un canvas propio
// (present_wasm.go): el lienzo de Fyne en web es SIEMPRE la ventana por
// devicePixelRatio —4,3 MPix en un portátil Retina— y no se puede elegir, así
// que como superficie de juego no hay forma de tener a la vez nitidez y
// velocidad. Con canvas propio la resolución la elegimos nosotros y el CSS
// escala el resultado.
var presentNative = func(w *astWidget) { fyne.Do(w.Refresh) }
