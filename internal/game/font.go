package game

import (
	"os"
	"strconv"
	"sync"

	"fyne.io/fyne/v2/theme"
	"github.com/fogleman/gg"
	"golang.org/x/image/font"
)

var (
	ggBoldOnce sync.Once
	ggBoldPath string
)

// ggBoldFont extrae la fuente en negrita de Fyne a un fichero temporal (una vez).
func ggBoldFont() string {
	ggBoldOnce.Do(func() {
		res := theme.DefaultTextBoldFont()
		if res == nil {
			return
		}
		f, err := os.CreateTemp("", "pmini-ast-bold-*.ttf")
		if err != nil {
			return
		}
		defer f.Close()
		if _, err := f.Write(res.Content()); err != nil {
			return
		}
		ggBoldPath = f.Name()
	})
	return ggBoldPath
}

var (
	ggFaceMu    sync.Mutex
	ggFaceCache = map[string]font.Face{}
)

// ggSetFont carga `path` al tamaño `points` reutilizando la cara ya construida
// (gg.LoadFontFace lee y parsea el TTF en cada llamada, muy caro a 60 fps).
func ggSetFont(dc *gg.Context, path string, points float64) error {
	if path == "" {
		return nil
	}
	key := path + "|" + strconv.FormatFloat(points, 'f', -1, 64)
	ggFaceMu.Lock()
	face := ggFaceCache[key]
	ggFaceMu.Unlock()
	if face == nil {
		f, err := gg.LoadFontFace(path, points)
		if err != nil {
			return err
		}
		face = f
		ggFaceMu.Lock()
		ggFaceCache[key] = face
		ggFaceMu.Unlock()
	}
	dc.SetFontFace(face)
	return nil
}
