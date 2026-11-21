package game

import (
	"image"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/fogleman/gg"

	"rock-brothers/internal/sim"
)

// astWidget es el widget de Fyne que muestra y controla una partida. La
// simulación vive en sim.Sim; aquí solo está el pegamento con Fyne: el raster
// donde dibuja gg, el teclado y el foco.
type astWidget struct {
	widget.BaseWidget
	g        *sim.Sim
	buf      *image.RGBA // raster reutilizado entre frames
	stop     chan struct{}
	stopOnce sync.Once
}

// newWidget monta una partida para la app de Fyne. prefs puede ser nil (entonces
// el récord es de la sesión).
func newWidget(prefs fyne.Preferences) *astWidget {
	w := &astWidget{g: sim.New(), stop: make(chan struct{})}
	wireAudio(w.g)
	if prefs != nil {
		w.g.Best = prefs.Int("asteroids.best")
		w.g.BestLevel = prefs.Int("asteroids.bestLevel")
		w.g.OnBest = func(best, level int, who string) {
			prefs.SetInt("asteroids.best", best)
			prefs.SetInt("asteroids.bestLevel", level)
			prefs.SetString("asteroids.bestName", who)
		}
		w.g.BestName = prefs.String("asteroids.bestName")
	}
	w.ExtendBaseWidget(w)
	go runLoop(w.g, func(g *sim.Sim) { presentNative(w) }, w.stop)
	return w
}

func (w *astWidget) Close() {
	w.stopOnce.Do(func() { close(w.stop) })
}

// render pinta el frame en un raster propio y lo devuelve.
//
// Fyne pide el raster al tamaño de DISPOSITIVO (×dpr: en Retina 2×). Se pinta a
// tamaño LÓGICO para que el juego no cambie de tamaño aparente entre pantallas.
func (w *astWidget) render(pw, ph int) image.Image {
	s := sim.RenderScale(pw, ph)
	rw, rh := int(float64(pw)*s+0.5), int(float64(ph)*s+0.5)
	if rw < 2 {
		rw = 2
	}
	if rh < 2 {
		rh = 2
	}
	if w.buf == nil || w.buf.Rect.Dx() != rw || w.buf.Rect.Dy() != rh {
		w.buf = image.NewRGBA(image.Rect(0, 0, rw, rh))
	}
	// El mundo es el tamaño LÓGICO del widget (en nativo, su ventana): así las
	// naves y el HUD conservan su tamaño y lo que crece con la ventana es el
	// campo de juego.
	ww, wh := float64(w.Size().Width), float64(w.Size().Height)
	if ww < 8 || wh < 8 {
		ww, wh = sim.ArenaW, sim.ArenaH // todavía sin medir
	}
	w.g.DrawFrame(&ggDrawer{dc: gg.NewContextForRGBA(w.buf)}, rw, rh, ww, wh)
	return w.buf
}

// ---------- widget de Fyne ----------

func (w *astWidget) CreateRenderer() fyne.WidgetRenderer {
	return &astRenderer{raster: canvas.NewRaster(w.render)}
}

type astRenderer struct{ raster *canvas.Raster }

func (r *astRenderer) Layout(s fyne.Size)           { r.raster.Resize(s) }
func (r *astRenderer) MinSize() fyne.Size           { return fyne.NewSize(640, 480) }
func (r *astRenderer) Refresh()                     { r.raster.Refresh() }
func (r *astRenderer) Destroy()                     {}
func (r *astRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.raster} }

// ---------- teclado ----------

func (w *astWidget) FocusGained() {}
func (w *astWidget) FocusLost()   {}

// TypedKey/TypedRune son necesarios para ser "focusable"; el manejo real se hace
// en KeyDown/KeyUp (desktop.Keyable). TypedRune detecta los cheatcodes.
func (w *astWidget) TypedKey(*fyne.KeyEvent)  {}
func (w *astWidget) TypedRune(r rune)         { w.g.TypedRune(r) }
func (w *astWidget) KeyDown(k *fyne.KeyEvent) { w.g.KeyDown(string(k.Name)) }
func (w *astWidget) KeyUp(k *fyne.KeyEvent)   { w.g.KeyUp(string(k.Name)) }

// Open abre la ventana del juego (huevo de pascua). `icon` es opcional.
func Open(a fyne.App, icon fyne.Resource) {
	win := a.NewWindow(sim.Translate("Rock Brothers"))
	win.Resize(fyne.NewSize(585, 429))
	if icon != nil {
		win.SetIcon(icon)
	}
	w := newWidget(a.Preferences())
	// El widget ocupa todo el espacio disponible; la arena la aplica el propio
	// render escalando y centrando (así llena la ventana en vez de quedarse en un
	// cuadradito en medio).
	win.SetContent(container.NewStack(w))
	win.SetOnClosed(w.Close)
	win.Canvas().SetOnTypedKey(nil)
	win.Show()
	win.Canvas().Focus(w)
	// asegurar el foco (algunos gestores lo pierden al abrir)
	go func() {
		for i := 0; i < 6; i++ {
			time.Sleep(250 * time.Millisecond)
			closed := false
			select {
			case <-w.stop:
				closed = true
			default:
			}
			if closed {
				return
			}
			fyne.Do(func() { win.Canvas().Focus(w) })
		}
	}()
}
