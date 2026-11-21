package sim

import "image/color"

// Drawer es la API de dibujo que usa la escena. La implementa la plataforma:
//
//   - ggDrawer (internal/game): fogleman/gg, rasterizado por CPU. Nativo.
//   - canvasDrawer (internal/game, wasm): canvas 2D del navegador, GPU. Web.
//
// La escena dibuja igual en las dos: líneas, círculos, polígonos y texto. Lo
// único que necesita de la plataforma es esto, así que la simulación y el dibujo
// no dependen de Fyne, ni de gg, ni del navegador.
type Drawer interface {
	SetColor(c color.Color)
	SetHexColor(hex string)
	SetLineWidth(w float64)
	SetFont(size float64)
	Clear()

	MoveTo(x, y float64)
	LineTo(x, y float64)
	ClosePath()
	DrawLine(x1, y1, x2, y2 float64)
	DrawCircle(x, y, r float64)
	DrawRectangle(x, y, w, h float64)
	Fill()
	Stroke()

	Translate(x, y float64)
	Scale(x, y float64)

	DrawStringAnchored(s string, x, y, ax, ay float64)
	MeasureString(s string) (w, h float64)
}
