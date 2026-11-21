package game

import (
	"testing"

	"rock-brothers/internal/sim"
)

// TestRenderEscalas comprueba el raster reutilizado y la escala del widget
// nativo: el render no debe entrar en pánico y la imagen devuelta tiene que
// medir lo que toca (Fyne la estira después).
func TestRenderEscalas(t *testing.T) {
	casos := []struct {
		res  float64
		glow int
	}{{1, 3}, {1, 1}, {0.5, 3}, {0.5, 1}, {0.75, 2}}

	for _, c := range casos {
		sim.ApplyRenderOpts(c.res, c.glow)
		w := &astWidget{g: sim.New()}

		img := w.render(585, 429)
		if img == nil {
			t.Fatalf("res=%v glow=%d: render devolvió nil", c.res, c.glow)
		}
		want := int(585*c.res + 0.5)
		if got := img.Bounds().Dx(); got != want {
			t.Errorf("res=%v: ancho %d, esperado %d", c.res, got, want)
		}
		// el buffer se reutiliza entre frames (no debe cambiar de tamaño)
		img2 := w.render(585, 429)
		if img2.Bounds() != img.Bounds() {
			t.Errorf("res=%v: el raster cambió de tamaño entre frames", c.res)
		}
	}

	// restaurar los valores por defecto para no afectar a otros tests
	sim.ApplyRenderOpts(1, 3)
}
