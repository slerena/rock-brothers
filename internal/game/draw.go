package game

import (
	"image/color"

	"github.com/fogleman/gg"
)

// ggDrawer implementa sim.Drawer con fogleman/gg (rasterizado por CPU). Es la
// vía nativa (macOS/Linux/Windows). En web se usa el canvas del navegador
// (draw_canvas_wasm.go), que rasteriza la GPU.
type ggDrawer struct{ dc *gg.Context }

func (d *ggDrawer) SetColor(c color.Color)                    { d.dc.SetColor(c) }
func (d *ggDrawer) SetHexColor(hex string)                    { d.dc.SetHexColor(hex) }
func (d *ggDrawer) SetLineWidth(w float64)                    { d.dc.SetLineWidth(w) }
func (d *ggDrawer) Clear()                                    { d.dc.Clear() }
func (d *ggDrawer) MoveTo(x, y float64)                       { d.dc.MoveTo(x, y) }
func (d *ggDrawer) LineTo(x, y float64)                       { d.dc.LineTo(x, y) }
func (d *ggDrawer) ClosePath()                                { d.dc.ClosePath() }
func (d *ggDrawer) DrawLine(x1, y1, x2, y2 float64)           { d.dc.DrawLine(x1, y1, x2, y2) }
func (d *ggDrawer) DrawCircle(x, y, r float64)                { d.dc.DrawCircle(x, y, r) }
func (d *ggDrawer) DrawRectangle(x, y, w, h float64)          { d.dc.DrawRectangle(x, y, w, h) }
func (d *ggDrawer) Fill()                                     { d.dc.Fill() }
func (d *ggDrawer) Stroke()                                   { d.dc.Stroke() }
func (d *ggDrawer) Translate(x, y float64)                    { d.dc.Translate(x, y) }
func (d *ggDrawer) Scale(x, y float64)                        { d.dc.Scale(x, y) }
func (d *ggDrawer) MeasureString(s string) (float64, float64) { return d.dc.MeasureString(s) }

func (d *ggDrawer) DrawStringAnchored(s string, x, y, ax, ay float64) {
	d.dc.DrawStringAnchored(s, x, y, ax, ay)
}

// SetFont carga (y cachea) la fuente en negrita del tema de Fyne al tamaño dado.
// Si no hubiera fuente disponible, el texto simplemente no se dibuja.
func (d *ggDrawer) SetFont(size float64) {
	if p := ggBoldFont(); p != "" {
		_ = ggSetFont(d.dc, p, size)
	}
}
