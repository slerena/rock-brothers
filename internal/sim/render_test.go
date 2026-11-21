package sim

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"github.com/fogleman/gg"
)

// testDrawer implementa Drawer con gg, sin fuente (el texto no se dibuja). Sirve
// para verificar que la escena pinta de verdad en un raster.
type testDrawer struct{ dc *gg.Context }

func (d *testDrawer) SetColor(c color.Color)                    { d.dc.SetColor(c) }
func (d *testDrawer) SetHexColor(hex string)                    { d.dc.SetHexColor(hex) }
func (d *testDrawer) SetLineWidth(w float64)                    { d.dc.SetLineWidth(w) }
func (d *testDrawer) SetFont(float64)                           {}
func (d *testDrawer) Clear()                                    { d.dc.Clear() }
func (d *testDrawer) MoveTo(x, y float64)                       { d.dc.MoveTo(x, y) }
func (d *testDrawer) LineTo(x, y float64)                       { d.dc.LineTo(x, y) }
func (d *testDrawer) ClosePath()                                { d.dc.ClosePath() }
func (d *testDrawer) DrawLine(x1, y1, x2, y2 float64)           { d.dc.DrawLine(x1, y1, x2, y2) }
func (d *testDrawer) DrawCircle(x, y, r float64)                { d.dc.DrawCircle(x, y, r) }
func (d *testDrawer) DrawRectangle(x, y, w, h float64)          { d.dc.DrawRectangle(x, y, w, h) }
func (d *testDrawer) Fill()                                     { d.dc.Fill() }
func (d *testDrawer) Stroke()                                   { d.dc.Stroke() }
func (d *testDrawer) Translate(x, y float64)                    { d.dc.Translate(x, y) }
func (d *testDrawer) Scale(x, y float64)                        { d.dc.Scale(x, y) }
func (d *testDrawer) MeasureString(s string) (float64, float64) { return d.dc.MeasureString(s) }

func (d *testDrawer) DrawStringAnchored(s string, x, y, ax, ay float64) {
	d.dc.DrawStringAnchored(s, x, y, ax, ay)
}

// TestRenderConContenido: el frame que se dibuja tiene que tener contenido de
// verdad, no un lienzo vacío.
func TestRenderConContenido(t *testing.T) {
	ApplyRenderOpts(1, 3)
	defer ApplyRenderOpts(1, 3)

	g := &Sim{}
	g.w, g.h = ArenaW, ArenaH
	g.players = append(g.players, newPlayer(0, ""))
	g.resetShip(g.players[0])
	g.newLevel() // rocas del nivel 1

	rgba := image.NewRGBA(image.Rect(0, 0, 1280, 960))
	g.DrawFrame(&testDrawer{dc: gg.NewContextForRGBA(rgba)}, 1280, 960, ArenaW, ArenaH)

	// la nave y las rocas son verdes: contamos píxeles claramente verdes
	verdes := 0
	for i := 0; i+3 < len(rgba.Pix); i += 4 {
		if rgba.Pix[i+1] > 100 && rgba.Pix[i+1] > rgba.Pix[i] {
			verdes++
		}
	}
	if verdes < 500 {
		t.Errorf("el frame sale casi vacío: %d píxeles verdes", verdes)
	}
}

// TestRenderSinMedidas evita el caso raro de un lienzo diminuto (al arrancar).
func TestRenderSinMedidas(t *testing.T) {
	ApplyRenderOpts(1, 3)
	g := &Sim{}
	for _, n := range []int{0, 1, 2} {
		rgba := image.NewRGBA(image.Rect(0, 0, max(n, 1), max(n, 1)))
		g.DrawFrame(&testDrawer{dc: gg.NewContextForRGBA(rgba)}, n, n, ArenaW, ArenaH)
	}
}

// TestNavesConColorDistinto: en multijugador hay que distinguir las naves, así
// que cada jugador tiene su color (el 0 verde, el 1 morado). Se comprueba
// contando píxeles de cada tono en el frame.
func TestNavesConColorDistinto(t *testing.T) {
	ApplyRenderOpts(1, 3)
	g := New()
	g.w, g.h = ArenaW, ArenaH
	g.resetShip(g.Local())
	p2 := g.AddPlayer("dos")

	rgba := image.NewRGBA(image.Rect(0, 0, 1280, 960))
	g.DrawFrame(&testDrawer{dc: gg.NewContextForRGBA(rgba)}, 1280, 960, ArenaW, ArenaH)

	verdes, morados := 0, 0
	for i := 0; i+3 < len(rgba.Pix); i += 4 {
		r, gg2, b := rgba.Pix[i], rgba.Pix[i+1], rgba.Pix[i+2]
		// morado del jugador 1 (0xc0,0x7a,0xff): rojo y azul altos, verde medio
		if b > 180 && r > 120 && gg2 < r && gg2 < b {
			morados++
		}
		// verde fósforo: verde claramente dominante
		if gg2 > 150 && gg2 > r+40 && gg2 > b+40 {
			verdes++
		}
	}
	if verdes < 50 {
		t.Errorf("no se ve la nave verde del jugador 0 (%d px)", verdes)
	}
	if morados < 50 {
		t.Errorf("no se ve la nave morada del jugador 1 (%d px)", morados)
	}
	// la nave 1 existe y está viva (por si acaso el color saliera de otra cosa)
	if x, y := p2.Pos(); x == 0 && y == 0 {
		t.Errorf("el jugador 1 no está colocado: (%v,%v)", x, y)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// TestDibujoCampoFijo: las estructuras del campo fijo se dibujan como bloques
// azules (se distinguen de los asteroides verdes).
func TestDibujoCampoFijo(t *testing.T) {
	ApplyRenderOpts(1, 3)
	g := New()
	g.w, g.h = ArenaW, ArenaH
	g.SetArena(ArenaFixed)
	g.restart()

	rgba := image.NewRGBA(image.Rect(0, 0, 1280, 960))
	g.DrawFrame(&testDrawer{dc: gg.NewContextForRGBA(rgba)}, 1280, 960, ArenaW, ArenaH)

	azules := 0
	for i := 0; i+3 < len(rgba.Pix); i += 4 {
		r, gg2, b := rgba.Pix[i], rgba.Pix[i+1], rgba.Pix[i+2]
		if b > 180 && gg2 > 140 && r < 180 && b > r {
			azules++
		}
	}
	if azules < 300 {
		t.Errorf("no se ven los bloques del campo fijo (azul): %d px", azules)
	}
}

// TestPreviewMuerteYTeleport pinta la muerte (trozos volando) y la reaparición
// (teletransporte) en varios instantes y deja los PNG en el directorio temporal
// para poder revisarlos a ojo.
func TestPreviewMuerteYTeleport(t *testing.T) {
	ApplyRenderOpts(1, 3)
	g := New()
	g.w, g.h = ArenaW, ArenaH
	g.SetArena(ArenaRocks)
	g.restart()
	g.rocks = []astRock{{x: 5, y: 5, r: 1, hp: 1, hpMax: 1, shape: []float64{1}}}
	p := g.Local()
	p.ship.x, p.ship.y, p.ship.alive, p.ship.invuln = 320, 240, true, 0
	g.killShip(p)

	save := func(name string) {
		rgba := image.NewRGBA(image.Rect(0, 0, 1280, 960))
		g.DrawFrame(&testDrawer{dc: gg.NewContextForRGBA(rgba)}, 1280, 960, ArenaW, ArenaH)
		f, err := os.Create(os.TempDir() + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, rgba); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		t.Logf("%s/%s", os.TempDir(), name)
	}
	for i, d := range []float64{0.3, 0.8, 1.3} {
		p.deathT = d
		p.respawnT = 0
		save(fmt.Sprintf("ast_muerte_%d.png", i))
	}
	for i, r := range []float64{1.85, 1.0, 0.2} {
		p.deathT = 0
		p.respawnT = r
		save(fmt.Sprintf("ast_tp_%d.png", i))
	}
}

// TestPreviewTablaFinal pinta el fin de partida (la tabla de puntuaciones) para
// revisarla.
func TestPreviewTablaFinal(t *testing.T) {
	ApplyRenderOpts(1, 3)
	g := New()
	g.w, g.h = ArenaW, ArenaH
	g.SetArena(ArenaRocks)
	g.SetPvP(true)
	g.restart()
	g.rocks = []astRock{{x: 5, y: 5, r: 1, hp: 1, hpMax: 1, shape: []float64{1}}}
	a := g.Local()
	b := g.AddPlayer("mateito")
	c := g.AddPlayer("Bot 2")
	a.kills, a.deaths = 5, 2
	b.kills, b.deaths = 7, 3
	c.kills, c.deaths = 1, 6
	a.score, b.score, c.score = 1200, 1800, 300
	g.over = true

	rgba := image.NewRGBA(image.Rect(0, 0, 1280, 960))
	g.DrawFrame(&testDrawer{dc: gg.NewContextForRGBA(rgba)}, 1280, 960, ArenaW, ArenaH)
	f, err := os.Create(os.TempDir() + "/ast_tabla.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, rgba); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	t.Logf("%s/ast_tabla.png", os.TempDir())
}
