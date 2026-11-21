package sim

import (
	"math"
	"testing"
)

// TestMuerteDejaAnimacion: al morir la nave saltan trozos, suena y arranca el
// retardo de reaparición; la nave se recompone luego en el sitio elegido (donde
// se ve el teletransporte) y sale invulnerable.
func TestMuerteDejaAnimacion(t *testing.T) {
	g := New()
	g.w, g.h = ArenaW, ArenaH
	p := g.Local()
	p.ship.x, p.ship.y = 123, 77
	lives := p.lives

	g.killShip(p)

	if p.deathT <= 0 {
		t.Fatalf("al morir debe arrancar la animación (deathT=%v)", p.deathT)
	}
	if p.deathX != 123 || p.deathY != 77 {
		t.Errorf("los trozos salen de donde cayó la nave: (%v,%v)", p.deathX, p.deathY)
	}
	if len(g.parts) == 0 {
		t.Error("al morir deben saltar chispas")
	}
	if p.lives != lives-1 {
		t.Errorf("debe perder una vida: %d -> %d", lives, p.lives)
	}
	// ya NO reaparece al instante: se queda muerta mientras se ve el teletransporte
	if p.ship.alive {
		t.Fatal("no debe reaparecer en el sitio")
	}
	if p.respawnT <= 0 {
		t.Fatal("debe arrancar el retardo de reaparición")
	}
	if p.spawnX == 0 && p.spawnY == 0 {
		t.Error("el sitio de reaparición debe elegirse al morir (para el teletransporte)")
	}

	// y tras el retardo se recompone ahí, con invulnerabilidad de salida
	for i := 0; i < 300 && !p.ship.alive; i++ {
		g.Step(0.016)
	}
	if !p.ship.alive {
		t.Fatal("debería haberse recompuesto tras el retardo")
	}
	if math.Hypot(p.ship.x-p.spawnX, p.ship.y-p.spawnY) > 1 {
		t.Errorf("debe aparecer en el sitio elegido: (%.0f,%.0f) vs (%.0f,%.0f)",
			p.ship.x, p.ship.y, p.spawnX, p.spawnY)
	}
	if p.ship.invuln < shipInvulnSpawn-0.2 {
		t.Errorf("al salir debe ser invulnerable %v s, es %v", shipInvulnSpawn, p.ship.invuln)
	}
	// y la animación de los trozos acaba sola (se deja a la nave invulnerable
	// para que no la maten otra vez y reinicie la animación)
	p.ship.invuln = 999
	for i := 0; i < 300; i++ {
		g.Step(0.016)
	}
	if p.deathT > 0 {
		t.Errorf("la animación debe acabar sola, queda %v", p.deathT)
	}
}

// TestMuerteViajaEnElSnapshot: la animación de muerte es estado del jugador, así
// que tiene que llegar al cliente.
func TestMuerteViajaEnElSnapshot(t *testing.T) {
	g := New()
	g.w, g.h = ArenaW, ArenaH
	g.Local().ship.x, g.Local().ship.y = 200, 150
	g.killShip(g.Local())

	b := g.Snapshot()
	other := New()
	if err := other.Load(b); err != nil {
		t.Fatalf("load: %v", err)
	}
	p := other.Local()
	if p.deathT != g.Local().deathT || p.deathX != 200 || p.deathY != 150 {
		t.Errorf("la animación no viajó: deathT=%v death=(%v,%v)",
			p.deathT, p.deathX, p.deathY)
	}
}

// TestExtrapolateMueveSoloPosiciones: extrapolar avanza lo que se mueve, sin
// inventarse entidades ni tocar la nave que lleva el cliente.
func TestExtrapolateMueveSoloPosiciones(t *testing.T) {
	g := New()
	g.w, g.h = ArenaW, ArenaH
	p := g.Local()
	p.ship.vx, p.ship.vy = 100, -50
	p.ship.x, p.ship.y = 300, 300

	rocks := len(g.rocks)
	x0, y0 := p.ship.x, p.ship.y
	g.Extrapolate(0.1, p) // p es el jugador local: su nave NO se extrapola
	if p.ship.x != x0 || p.ship.y != y0 {
		t.Errorf("la nave local no debe extrapolarse: (%v,%v) -> (%v,%v)", x0, y0, p.ship.x, p.ship.y)
	}
	if len(g.rocks) != rocks {
		t.Errorf("extrapolar no debe crear entidades: %d -> %d", rocks, len(g.rocks))
	}

	// sin jugador local, la nave sí se mueve
	g.Extrapolate(0.1, nil)
	if p.ship.x == x0 && p.ship.y == y0 {
		t.Error("sin jugador local la nave debe extrapolarse")
	}
	if math.Abs(p.ship.x-(x0+100*0.1)) > 1e-9 {
		t.Errorf("la extrapolación debe ser x+vx*dt: %v, esperado %v", p.ship.x, x0+10)
	}
}

// TestStepLocalShipSoloEseJugador: la predicción mueve la nave del jugador
// indicado y solo esa.
func TestStepLocalShipSoloEseJugador(t *testing.T) {
	g := New()
	g.w, g.h = ArenaW, ArenaH
	p0 := g.Local()
	p1 := g.AddPlayer("dos")
	x0, y0 := p0.ship.x, p0.ship.y
	x1, y1 := p1.ship.x, p1.ship.y

	g.KeyDownFor(p1, KeyUp) // empuja al jugador 1
	for i := 0; i < 5; i++ {
		g.StepLocalShip(p1, 0.016)
	}
	if p0.ship.x != x0 || p0.ship.y != y0 {
		t.Errorf("el jugador 0 no debe moverse: (%v,%v) -> (%v,%v)", x0, y0, p0.ship.x, p0.ship.y)
	}
	if p1.ship.x == x1 && p1.ship.y == y1 {
		t.Errorf("el jugador 1 debería moverse: sigue en (%v,%v)", x1, y1)
	}
}

// TestEscudoReflectante: con escudo de power-up y el escudo manual activo, los
// disparos se devuelven; sin escudo de power-up no (rebote solo si ya lo llevas).
func TestEscudoReflectante(t *testing.T) {
	for _, conEscudo := range []bool{false, true} {
		g := New()
		g.w, g.h = 1280, 960
		g.SetArena(ArenaRocks)
		g.SetPvP(true)
		g.restart()
		g.rocks = []astRock{{x: 5, y: 5, r: 1, hp: 1, hpMax: 1, shape: []float64{1}}}

		a := g.Local() // el que dispara
		b := g.AddPlayer("escudo")
		a.ship.x, a.ship.y, a.ship.angle = 300, 480, 0
		a.ship.alive, a.ship.invuln, a.ship.fireTimer = true, 0, 0
		b.ship.x, b.ship.y = 460, 480
		b.ship.alive, b.ship.invuln = true, 0
		if conEscudo {
			b.permShield = 100
			b.shieldT = 1.2 // escudo manual activo
		}

		g.KeyDownFor(a, KeySpace) // dispara hacia el escudo
		reflected := false
		for i := 0; i < 80 && !reflected; i++ {
			g.Step(0.016)
			for _, bl := range g.bullets {
				if bl.owner == b.ID && bl.vx < 0 {
					reflected = true
				}
			}
		}
		if conEscudo && !reflected {
			t.Error("con escudo reflectante la bala debería volver")
		}
		if !conEscudo && reflected {
			t.Error("sin escudo de power-up no debe rebotar nada")
		}
	}
}

// TestReboteDelRailgun: si el haz del railgun da a un escudo reflectante, vuelve
// al que lo disparó con el triple de grosor, suena y le pega.
func TestReboteDelRailgun(t *testing.T) {
	g := New()
	g.w, g.h = 1280, 960
	g.SetArena(ArenaRocks)
	g.SetPvP(true)
	g.restart()
	g.rocks = []astRock{{x: 5, y: 5, r: 1, hp: 1, hpMax: 1, shape: []float64{1}}}

	a := g.Local()
	b := g.AddPlayer("escudo")
	a.ship.x, a.ship.y, a.ship.angle = 300, 480, 0
	a.ship.alive, a.ship.invuln = true, 0
	b.ship.x, b.ship.y = 500, 480
	b.ship.alive, b.ship.invuln = true, 0
	b.permShield = 100
	b.shieldT = 1.5

	g.fireRail(a)
	g.releaseRail(a)

	found := false
	for _, beam := range g.bossBeams {
		if beam.reflect {
			found = true
			if beam.base < railReflectBase {
				t.Errorf("el haz rebotado debe ser más ancho: %v", beam.base)
			}
		}
	}
	if !found {
		t.Fatal("el haz debería haber rebotado al que disparó")
	}
	if a.deaths == 0 {
		t.Error("el rebote debería haberle dado al que disparó (no llevaba escudo)")
	}
}
