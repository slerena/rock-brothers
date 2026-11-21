package sim

import (
	"math"
	"testing"
)

// TestBotPersigue: un bot en PvP apunta y se acerca a su objetivo (el otro
// jugador), y acaba disparándole.
func TestBotPersigue(t *testing.T) {
	g := New()
	g.w, g.h = 1280, 960
	g.SetArena(ArenaRocks)
	g.SetPvP(true)
	g.restart()
	// una roca lejana (vaciar la lista daría por superado el nivel)
	g.rocks = []astRock{{x: 5, y: 5, r: 1, hp: 1, hpMax: 1, shape: []float64{1}}}

	bot := g.Local()
	g.SetBotLevel(bot, BotSharp)
	rival := g.AddPlayer("rival")
	bot.ship.x, bot.ship.y, bot.ship.angle = 200, 200, 0
	bot.ship.alive, bot.ship.invuln = true, 0
	rival.ship.x, rival.ship.y = 900, 200
	rival.ship.alive, rival.ship.invuln = true, 0

	d0 := math.Hypot(rival.ship.x-bot.ship.x, rival.ship.y-bot.ship.y)
	fired := false
	for i := 0; i < 600; i++ {
		left, right, thrust, fire := g.BotInput(bot)
		g.SetKey(bot, KeyA, left)
		g.SetKey(bot, KeyD, right)
		g.SetKey(bot, KeyW, thrust)
		g.SetKey(bot, KeySpace, fire)
		if fire {
			fired = true
		}
		g.Step(0.016)
		if !rival.ship.alive || rival.deaths > 0 {
			break
		}
	}
	d1 := math.Hypot(rival.ship.x-bot.ship.x, rival.ship.y-bot.ship.y)
	if !fired {
		t.Error("el bot debería haber disparado al tener el objetivo a tiro")
	}
	if d1 >= d0 && rival.deaths == 0 {
		t.Errorf("el bot debería acercarse al rival: %.0f → %.0f", d0, d1)
	}
}

// TestBotEsquiva: con una roca justo delante, el bot gira en vez de empotrarse.
func TestBotEsquiva(t *testing.T) {
	g := New()
	g.w, g.h = 1280, 960
	g.SetArena(ArenaRocks)
	g.restart()
	bot := g.Local()
	bot.ship.x, bot.ship.y, bot.ship.angle = 200, 480, 0
	bot.ship.alive, bot.ship.invuln = true, 0
	g.SetBotLevel(bot, BotSharp)
	// roca grande en la trayectoria (hacia +x)
	g.rocks = []astRock{{x: 260, y: 480, r: 24, hp: 1, hpMax: 1, shape: []float64{1}}}

	left, right, thrust, fire := g.BotInput(bot)
	if !thrust || fire {
		t.Errorf("ante un obstáculo debe empujar para esquivar y no disparar (thrust=%v fire=%v)", thrust, fire)
	}
	if !left && !right {
		t.Error("ante un obstáculo debe girar")
	}
}

// TestNivelesDeBot: el nivel cambia lo fino que apunta. Con el objetivo poco
// alineado, el torpe dispara y el bueno no.
func TestNivelesDeBot(t *testing.T) {
	g := New()
	g.w, g.h = 1280, 960
	g.SetArena(ArenaRocks)
	g.SetPvP(true)
	g.restart()
	g.rocks = []astRock{{x: 5, y: 5, r: 1, hp: 1, hpMax: 1, shape: []float64{1}}}

	bot := g.Local()
	rival := g.AddPlayer("rival")
	rival.ship.alive, rival.ship.invuln = true, 0
	bot.ship.alive, bot.ship.invuln = true, 0
	bot.ship.x, bot.ship.y, bot.ship.angle = 300, 480, 0
	// objetivo a 250 px pero desviado ~0.45 rad
	rival.ship.x = 300 + 250*math.Cos(0.45)
	rival.ship.y = 480 + 250*math.Sin(0.45)

	fires := func(level int) bool {
		g.SetBotLevel(bot, level)
		bot.botThink = 0
		_, _, _, fire := g.BotInput(bot)
		return fire
	}
	if !fires(BotDumb) {
		t.Error("el bot torpe debería disparar con ese desvío (su margen es amplio)")
	}
	if fires(BotSharp) {
		t.Error("el bot bueno no debería disparar con el objetivo tan desviado")
	}
}

// TestSiluetasDistintas: cada jugador tiene una forma de nave distinta (además
// del color), para distinguirlos de un vistazo.
func TestSiluetasDistintas(t *testing.T) {
	shapes := make([][][2]float64, 4)
	for id := 0; id < 4; id++ {
		shapes[id] = shipShape(id, 100, 100, 0)
	}
	for i := 0; i < 4; i++ {
		if len(shapes[i]) < 3 {
			t.Errorf("la silueta %d tiene muy pocos puntos: %d", i, len(shapes[i]))
		}
		for j := i + 1; j < 4; j++ {
			same := len(shapes[i]) == len(shapes[j])
			if same {
				for k := range shapes[i] {
					if math.Abs(shapes[i][k][0]-shapes[j][k][0]) > 0.01 ||
						math.Abs(shapes[i][k][1]-shapes[j][k][1]) > 0.01 {
						same = false
						break
					}
				}
			}
			if same {
				t.Errorf("las siluetas %d y %d son iguales", i, j)
			}
		}
	}
	// el morro tiene que verse claro: bien adelante, y con popa detrás
	for id := 0; id < 4; id++ {
		sh := shipShape(id, 0, 0, 0)
		if sh[0][0] < 15 {
			t.Errorf("la silueta %d no tiene el morro claro (x=%.0f)", id, sh[0][0])
		}
		behind := 0
		for _, q := range sh[1:] {
			if q[0] < -3 {
				behind++
			}
		}
		if behind < 2 {
			t.Errorf("la silueta %d no tiene popa clara (%d vértices detrás)", id, behind)
		}
	}

	// y la silueta gira con la nave
	a := shipShape(0, 100, 100, 0)
	b := shipShape(0, 100, 100, math.Pi/2)
	if math.Abs(a[0][0]-b[0][0]) < 1 && math.Abs(a[0][1]-b[0][1]) < 1 {
		t.Error("la silueta debería girar con la nave")
	}
}

// TestBotsUsanSusTrastos: los bots deben usar minas, misiles, railgun y escudo
// de vez en cuando (y los buenos, bastante). Antes solo movían y disparaban.
func TestBotsUsanSusTrastos(t *testing.T) {
	for _, level := range []int{BotDumb, BotNormal, BotSharp} {
		g := New()
		g.w, g.h = 1280, 960
		g.SetArena(ArenaRocks)
		g.SetPvP(true)
		g.restart()
		g.rocks = []astRock{{x: 5, y: 5, r: 1, hp: 1, hpMax: 1, shape: []float64{1}}}

		bot := g.Local()
		g.SetBotLevel(bot, level)
		rival := g.AddPlayer("rival")
		bot.ship.x, bot.ship.y, bot.ship.alive, bot.ship.invuln = 300, 480, true, 0
		rival.ship.x, rival.ship.y = 900, 480
		rival.ship.alive, rival.ship.invuln = true, 0
		bot.minesLeft, bot.missilesLeft = 50, 50
		bot.permShield = permShieldCap // para que pueda levantar el escudo
		// el rival dispara hacia el bot: le da motivo para levantar el escudo
		rival.ship.angle = math.Pi
		rival.ship.fireTimer = 0
		g.SetKey(rival, KeySpace, true)

		used := map[string]bool{}
		railGone := false
		for i := 0; i < 1500; i++ { // ~24 s
			l, r, th, fi := g.BotInput(bot)
			g.SetKey(bot, KeyA, l)
			g.SetKey(bot, KeyD, r)
			g.SetKey(bot, KeyW, th)
			g.SetKey(bot, KeySpace, fi)
			for _, a := range g.BotActions(bot) {
				if a[0] == '-' {
					g.KeyUpFor(bot, a[1:])
					continue
				}
				g.KeyDownFor(bot, a)
			}
			g.Step(0.016)
			if bot.minesLeft < 50 {
				used["minas"] = true
			}
			if bot.missilesLeft < 50 {
				used["misiles"] = true
			}
			if bot.shieldT > 0 {
				used["escudo"] = true
			}
			if bot.railCharging || bot.railFlash > 0 {
				railGone = true
			}
		}
		if railGone {
			used["railgun"] = true
		}
		t.Logf("nivel %d usó: %v", level, used)
		if level == BotSharp && len(used) < 2 {
			t.Errorf("el bot bueno debería usar casi todo, usó %v", used)
		}
		if level == BotDumb && len(used) == 0 {
			t.Errorf("hasta el torpe debería usar algo de vez en cuando")
		}
	}
}
