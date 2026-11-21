//go:build wasm

package game

import (
	"fmt"
	"image/color"
	"math"
	"syscall/js"
)

// canvasDrawer dibuja sobre el canvas 2D del navegador (rasterizado por GPU).
//
// Ventajas frente a gg en web: no hay rasterizado por CPU (el coste era ~10 ms
// por megapíxel, que saturaba el hilo del navegador), es nítido a cualquier
// resolución porque son vectores, y no hace falta traer ninguna fuente (se usa
// una del sistema).
type canvasDrawer struct {
	cx     js.Value
	w, h   int
	fontPx float64
}

func newCanvasDrawer(cx js.Value, w, h int) *canvasDrawer {
	d := &canvasDrawer{cx: cx, w: w, h: h, fontPx: 18}
	d.applyFont()
	return d
}

func (d *canvasDrawer) applyFont() {
	d.cx.Set("font", fmt.Sprintf("bold %.0fpx ui-monospace, Menlo, Consolas, monospace", d.fontPx))
}

func (d *canvasDrawer) SetColor(c color.Color) {
	r, g, b, a := c.RGBA()
	css := fmt.Sprintf("rgba(%d,%d,%d,%.3f)", r>>8, g>>8, b>>8, float64(a)/65535)
	d.cx.Set("strokeStyle", css)
	d.cx.Set("fillStyle", css)
}

func (d *canvasDrawer) SetHexColor(hex string) {
	d.cx.Set("strokeStyle", hex)
	d.cx.Set("fillStyle", hex)
}

func (d *canvasDrawer) SetLineWidth(w float64) { d.cx.Set("lineWidth", w) }

func (d *canvasDrawer) SetFont(size float64) {
	d.fontPx = size
	d.applyFont()
}

// Clear pinta todo el lienzo con el color actual ignorando la transformación,
// como hace gg. Deja la transformación como estaba: la resetea el presentador al
// empezar cada frame (la del canvas persiste entre frames).
func (d *canvasDrawer) Clear() {
	d.cx.Call("save")
	d.cx.Call("setTransform", 1, 0, 0, 1, 0, 0)
	d.cx.Call("fillRect", 0, 0, d.w, d.h)
	d.cx.Call("restore")
}

func (d *canvasDrawer) MoveTo(x, y float64)              { d.cx.Call("moveTo", x, y) }
func (d *canvasDrawer) LineTo(x, y float64)              { d.cx.Call("lineTo", x, y) }
func (d *canvasDrawer) ClosePath()                       { d.cx.Call("closePath") }
func (d *canvasDrawer) Translate(x, y float64)           { d.cx.Call("translate", x, y) }
func (d *canvasDrawer) Scale(x, y float64)               { d.cx.Call("scale", x, y) }
func (d *canvasDrawer) DrawRectangle(x, y, w, h float64) { d.cx.Call("rect", x, y, w, h) }

// Fill y Stroke vacían el path al terminar, igual que gg (Fill/Stroke llaman a
// ClearPath), así que cada bloque de dibujo empieza limpio.
func (d *canvasDrawer) Fill() {
	d.cx.Call("fill")
	d.cx.Call("beginPath")
}

func (d *canvasDrawer) Stroke() {
	d.cx.Call("stroke")
	d.cx.Call("beginPath")
}

func (d *canvasDrawer) DrawLine(x1, y1, x2, y2 float64) {
	d.cx.Call("moveTo", x1, y1)
	d.cx.Call("lineTo", x2, y2)
	d.cx.Call("stroke")
	d.cx.Call("beginPath")
}

// DrawCircle añade un círculo al path actual (como gg: luego hay que Fill o
// Stroke). El moveTo previo evita que el arco se una con el subpath anterior.
func (d *canvasDrawer) DrawCircle(x, y, r float64) {
	d.cx.Call("moveTo", x+r, y)
	d.cx.Call("arc", x, y, r, 0, 2*math.Pi)
}

// DrawStringAnchored traduce el anclaje de gg (0..1 en cada eje) a textAlign y
// textBaseline, que es como lo hace el canvas.
func (d *canvasDrawer) DrawStringAnchored(s string, x, y, ax, ay float64) {
	align := "start"
	switch {
	case ax >= 0.75:
		align = "end"
	case ax >= 0.25:
		align = "center"
	}
	base := "top"
	switch {
	case ay >= 0.75:
		base = "bottom"
	case ay >= 0.25:
		base = "middle"
	}
	d.cx.Set("textAlign", align)
	d.cx.Set("textBaseline", base)
	d.cx.Call("fillText", s, x, y)
}

func (d *canvasDrawer) MeasureString(s string) (float64, float64) {
	m := d.cx.Call("measureText", s)
	return m.Get("width").Float(), d.fontPx
}
