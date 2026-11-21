package sim

import (
	"math"
	"testing"
)

func TestShieldHardMult(t *testing.T) {
	cases := []struct {
		hpMax int
		want  float64
	}{
		{0, 1}, {1, 1}, {2, 2}, {3, 3}, {4, 3}, {10, 3},
	}
	for _, c := range cases {
		if got := shieldHardMult(c.hpMax); got != c.want {
			t.Errorf("shieldHardMult(%d) = %v, want %v", c.hpMax, got, c.want)
		}
	}
}

func TestShieldCostHardness(t *testing.T) {
	normal := shieldCost(46, 1)
	hard2 := shieldCost(46, 2)
	hard3 := shieldCost(46, 3)

	if normal <= 0 {
		t.Fatalf("el coste de una roca normal debe ser positivo, es %v", normal)
	}
	if hard2 < 2*normal {
		t.Errorf("una roca dura debe costar al menos el doble: %v vs %v", hard2, normal)
	}
	if hard3 < 3*normal {
		t.Errorf("una roca dura nivel 3 debe costar el triple: %v vs %v", hard3, normal)
	}
	// ninguna colisión puede vaciar el escudo de una vez
	if hard3 >= permShieldCap {
		t.Errorf("el coste (%v) no debe superar el tope del escudo (%v)", hard3, permShieldCap)
	}
	if small := shieldCost(1, 1); small != 25 {
		t.Errorf("el coste mínimo debe ser 25, es %v", small)
	}
}

func TestFormatClock(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{180, "3:00"},
		{179.4, "3:00"},
		{30.2, "0:31"},
		{1.0, "0:01"},
		{0.0, "0:00"},
		{-0.5, "-0:01"},
		{-65, "-1:05"},
	}
	for _, c := range cases {
		if got := formatClock(c.in); got != c.want {
			t.Errorf("formatClock(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPowerupUseful(t *testing.T) {
	g := &Sim{}
	p := &Player{}
	p.permShield = permShieldCap
	if g.powerupUseful(p, powerupShield) {
		t.Error("con el escudo al máximo, otro escudo no sirve")
	}
	p.permShield = 0
	p.rapidFire = maxRapidFire
	if g.powerupUseful(p, powerupRapid) {
		t.Error("con la cadencia al máximo, más cadencia no sirve")
	}
	p.rapidFire = 0
	p.orbs = make([]astOrb, orbMax)
	if g.powerupUseful(p, powerupOrb) {
		t.Error("con todos los asistentes, otro asistente no sirve")
	}
	p.orbs = nil
	for _, k := range []int{powerupLife, powerupMissiles, powerupMines, powerupCompanion} {
		if !g.powerupUseful(p, k) {
			t.Errorf("el power-up %d debería servir siempre", k)
		}
	}
}

// Los power-ups de arma solo valen si cambian el arma actual: con balas sirve
// el láser y con láser sirve la ametralladora (y solo entonces).
func TestWeaponPowerupsAreExclusive(t *testing.T) {
	g := &Sim{}
	p := &Player{laser: false}
	if !g.powerupUseful(p, powerupLaser) || g.powerupUseful(p, powerupGun) {
		t.Error("con balas: debe servir el láser y no la ametralladora")
	}
	p.laser = true
	if g.powerupUseful(p, powerupLaser) || !g.powerupUseful(p, powerupGun) {
		t.Error("con láser: debe servir la ametralladora y no el láser")
	}
}

func TestNearestPowerupSkipsUseless(t *testing.T) {
	g := &Sim{}
	p := &Player{permShield: permShieldCap}
	g.powerups = []astPowerup{
		{x: 10, y: 0, kind: powerupShield}, // no sirve: escudo al máximo
		{x: 200, y: 0, kind: powerupMissiles},
	}
	x, _, ok := g.nearestPowerup(p, 0, 0, 500)
	if !ok || x != 200 {
		t.Errorf("debe ir a por el power-up útil más cercano (200), fue %v (ok=%v)", x, ok)
	}
	if _, _, ok := g.nearestPowerup(p, 0, 0, 100); ok {
		t.Error("el power-up inútil no debe contar aunque esté cerca")
	}
}

func TestEnemyHP(t *testing.T) {
	cases := []struct{ level, want int }{
		{1, 1}, {2, 2}, {3, 3}, {5, 5}, {8, 8}, {20, 8},
	}
	for _, c := range cases {
		if got := enemyHP(c.level); got != c.want {
			t.Errorf("enemyHP(%d) = %d, want %d", c.level, got, c.want)
		}
	}
}

func TestRamDamageScalesWithSpeed(t *testing.T) {
	slow := ramDamage(0)
	fast := ramDamage(shipSpeedMax)
	if slow <= 0 {
		t.Fatalf("el daño de embestida debe ser positivo, es %d", slow)
	}
	if fast != 2*slow {
		t.Errorf("a tope debe hacer el doble que parada: %d vs %d", fast, slow)
	}
	// nunca baja del daño base ni supera el doble, aunque la velocidad se salga
	if ramDamage(-50) != slow {
		t.Errorf("velocidad negativa debe dar el daño base")
	}
	if ramDamage(shipSpeedMax*3) != fast {
		t.Errorf("por encima del tope no debe subir más")
	}
}

func TestCadenceAndLevelClock(t *testing.T) {
	// 0,66 s era la cadencia original; ahora un 15% más rápida
	if math.Abs(fireIntervalBase-0.66/1.15/1.1) > 1e-9 {
		t.Errorf("la cadencia inicial debe ser 0.66/1.15/1.1: es %v", fireIntervalBase)
	}
	if levelTimeTotal != 90 {
		t.Errorf("el nivel debe durar minuto y medio, dura %v s", levelTimeTotal)
	}
}

func TestLaserRangeMatchesBullet(t *testing.T) {
	gunRange := bulletSpeed * bulletTTL
	laserRange := laserSpeed * laserTTL
	if math.Abs(gunRange-laserRange) > 1e-9 {
		t.Errorf("el láser debe tener el mismo alcance: %v vs %v", laserRange, gunRange)
	}
	if laserSpeed <= bulletSpeed {
		t.Errorf("el láser debe ir más rápido que la bala: %v vs %v", laserSpeed, bulletSpeed)
	}
}

func TestLaserLongerAndFaster(t *testing.T) {
	// el rayo dibujado es más largo que el de la primera versión (9+17 = 26 px)
	if dash := laserDashBack + laserDashFront; dash <= 26 {
		t.Errorf("el rayo láser debe ser más largo que antes: %v px", dash)
	}
	if laserSpeed < 4*bulletSpeed {
		t.Errorf("el láser debe ir mucho más rápido que la bala: %v vs %v", laserSpeed, bulletSpeed)
	}
}

func TestOrbsTakeTripleDamage(t *testing.T) {
	if orbHP < 3 {
		t.Errorf("un asistente debe aguantar al menos el triple que antes (1): %d", orbHP)
	}
}

func TestMaxRapidFireIsX8(t *testing.T) {
	if got := 1 << uint(maxRapidFire); got != 8 {
		t.Errorf("el tope de cadencia debe ser x8, es x%d", got)
	}
}

func TestCompanionDropCadence(t *testing.T) {
	if companionDropEvery != 10 {
		t.Errorf("el compañero debe salir 1 de cada 10 enemigos, es %d", companionDropEvery)
	}
}

func TestHoleAccel(t *testing.T) {
	g := &Sim{}
	if ax, ay := g.holeAccel(10, 10); ax != 0 || ay != 0 {
		t.Errorf("sin agujero no debe haber atracción: %v,%v", ax, ay)
	}

	g.hole = &astHole{x: 100, y: 100, r: 20}
	ax, ay := g.holeAccel(0, 100) // a la izquierda del agujero
	if ax <= 0 || math.Abs(ay) > 1e-9 {
		t.Errorf("la atracción debe apuntar al agujero: %v,%v", ax, ay)
	}

	// a más radio, más fuerza a la misma distancia
	near := &astHole{x: 100, y: 100, r: 20}
	far := &astHole{x: 100, y: 100, r: 100}
	g.hole = near
	a1, _ := g.holeAccel(0, 100)
	g.hole = far
	a2, _ := g.holeAccel(0, 100)
	if a2 <= a1 {
		t.Errorf("el agujero grande debe atraer más: %v vs %v", a2, a1)
	}
	if a2 <= 230 {
		t.Errorf("un agujero de 100 px debe superar el empuje de la nave (230): %v", a2)
	}
}

// Multijugador: New crea un jugador, AddPlayer añade un segundo vivo y en otra
// posición (sin solaparse), y KeyDownFor mueve solo al jugador indicado.
func TestAddPlayerAndInput(t *testing.T) {
	g := New()
	g.w, g.h = ArenaW, ArenaH // el juego aún no se ha medido

	if len(g.Players()) != 1 {
		t.Fatalf("New debe crear 1 jugador, hay %d", len(g.Players()))
	}
	p0 := g.Local()
	p1 := g.AddPlayer("dos")
	if len(g.Players()) != 2 {
		t.Fatalf("AddPlayer debe dejar 2 jugadores, hay %d", len(g.Players()))
	}
	if !p0.ship.alive || !p1.ship.alive {
		t.Fatalf("los dos jugadores deben empezar con la nave viva")
	}
	if d := math.Hypot(p0.ship.x-p1.ship.x, p0.ship.y-p1.ship.y); d < 20 {
		t.Fatalf("los jugadores deben aparecer separados, distancia %v", d)
	}

	// KeyDownFor("Up") empuja solo al jugador indicado
	x0, y0 := p0.ship.x, p0.ship.y
	x1, y1 := p1.ship.x, p1.ship.y
	g.KeyDownFor(p1, KeyUp)
	g.Step(0.016)

	if p0.ship.x != x0 || p0.ship.y != y0 {
		t.Errorf("el jugador 0 no debería moverse: (%v,%v) -> (%v,%v)",
			x0, y0, p0.ship.x, p0.ship.y)
	}
	if p1.ship.x == x1 && p1.ship.y == y1 {
		t.Errorf("el jugador 1 debería moverse con \"Up\": sigue en (%v,%v)", x1, y1)
	}
	if p0.ship.alive != true || p1.ship.alive != true {
		t.Fatal("ninguno de los dos jugadores debería morir en un paso")
	}
}

// TestDisparoDoble: con el power-up cada disparo lanza DOS proyectiles en
// paralelo (y si llevas láser, dos láseres); sin él, uno.
func TestDisparoDoble(t *testing.T) {
	g := New()
	g.w, g.h = ArenaW, ArenaH
	p := g.Local()
	p.ship.alive = true
	p.ship.fireTimer = 0

	before := len(g.bullets)
	g.fireLocked(p)
	if n := len(g.bullets) - before; n != 1 {
		t.Errorf("sin el doble debe salir 1 proyectil, salieron %d", n)
	}

	p.doubleShot = true
	p.ship.fireTimer = 0
	before = len(g.bullets)
	g.fireLocked(p)
	if n := len(g.bullets) - before; n != 2 {
		t.Fatalf("con el doble deben salir 2 proyectiles, salieron %d", n)
	}
	b1, b2 := g.bullets[len(g.bullets)-2], g.bullets[len(g.bullets)-1]
	if math.Abs(b1.vx-b2.vx) > 1e-9 || math.Abs(b1.vy-b2.vy) > 1e-9 {
		t.Error("los dos disparos deben ir en paralelo (misma velocidad)")
	}
	if d := math.Hypot(b1.x-b2.x, b1.y-b2.y); d < 12 || d > 16 {
		t.Errorf("deben salir separados 14 px, separación %v", d)
	}

	// con láser, dos láseres
	p.laser = true
	p.ship.fireTimer = 0
	before = len(g.bullets)
	g.fireLocked(p)
	if n := len(g.bullets) - before; n != 2 {
		t.Fatalf("con el doble y láser deben salir 2 láseres, salieron %d", n)
	}
	for _, b := range g.bullets[len(g.bullets)-2:] {
		if !b.laser {
			t.Error("con láser, los dos proyectiles deben ser láser")
		}
	}
	if g.powerupUseful(p, powerupDouble) {
		t.Error("con el doble ya cogido, otro doble no debería servir")
	}
}

// TestPvPFuegoAmigo: en "todos contra todos" una bala de un jugador mata a otro;
// en cooperativo las balas no hacen daño a los jugadores.
func TestPvPFuegoAmigo(t *testing.T) {
	for _, pvp := range []bool{false, true} {
		g := New()
		g.w, g.h = ArenaW, ArenaH
		g.SetPvP(pvp)
		// una roca lejana (dejar la lista vacía daría por superado el nivel y la
		// simulación se iría a la transición de nivel)
		g.rocks = []astRock{{x: 5, y: 5, r: 1, hp: 1, hpMax: 1, shape: []float64{1}}}
		a := g.Local()
		b := g.AddPlayer("dos")

		a.ship.x, a.ship.y, a.ship.angle = 200, 200, 0
		a.ship.alive, a.ship.invuln, a.ship.fireTimer = true, 0, 0
		b.ship.x, b.ship.y = 260, 200
		b.ship.alive, b.ship.invuln = true, 0
		lives := b.lives

		g.KeyDownFor(a, KeySpace) // dispara hacia la derecha
		for i := 0; i < 90 && b.lives == lives; i++ {
			b.ship.invuln = 0 // que no lo salve un margen de invulnerabilidad
			g.Step(0.016)
		}

		// al morir la nave reaparece al instante (si le quedan vidas), así que lo
		// que se mira es la vida perdida
		if pvp && b.lives == lives {
			t.Error("en PvP la bala debería haberle quitado una vida al otro")
		}
		if !pvp && b.lives != lives {
			t.Error("en cooperativo las balas no deben dañar a los jugadores")
		}
	}
}

// TestPvPUltimoEnPie: en PvP la partida termina cuando solo queda uno en pie.
func TestPvPUltimoEnPie(t *testing.T) {
	g := New()
	g.w, g.h = ArenaW, ArenaH
	g.SetPvP(true)
	a := g.Local()
	b := g.AddPlayer("dos")
	c := g.AddPlayer("tres")
	for _, p := range []*Player{a, b, c} {
		p.lives = 0
	}
	g.killShip(a)
	if g.Over() {
		t.Error("con dos jugadores vivos la partida no debe acabar")
	}
	g.killShip(b)
	if !g.Over() {
		t.Error("en PvP la partida debe acabar cuando queda uno solo")
	}
	_ = c
}

// TestProyectilNoAtraviesa: el láser avanza ~29 px por frame y la bala ~7, así
// que con una roca pequeña el proyectil podía "saltársela" (tunnelling). Ahora se
// comprueba todo el recorrido del frame, así que acierta en los dos casos.
func TestProyectilNoAtraviesa(t *testing.T) {
	for _, laser := range []bool{false, true} {
		g := New()
		g.w, g.h = ArenaW, ArenaH
		p := g.Local()
		p.ship.x, p.ship.y, p.ship.angle = 100, 240, 0
		p.ship.alive, p.ship.invuln, p.ship.fireTimer = true, 0, 0
		p.laser = laser
		// roca pequeña (r=3) justo en la línea de tiro
		g.rocks = []astRock{{x: 300, y: 240, r: 3, hp: 1, hpMax: 1, shape: []float64{1}}}

		g.KeyDownFor(p, KeySpace)
		hit := false
		for i := 0; i < 30; i++ {
			g.Step(0.016)
			if len(g.bullets) == 0 { // el proyectil se ha consumido: dio en la roca
				hit = true
				break
			}
		}
		if !hit {
			t.Errorf("laser=%v: el proyectil debería haber dado en la roca del camino", laser)
		}
	}
}

// TestCampoFijo: en el campo fijo las estructuras no se mueven, se rompen a
// tiros (sin partirse en trozos, para poder abrir túneles) y no hay agujero
// negro por mucho que pase el tiempo.
func TestCampoFijo(t *testing.T) {
	g := New()
	g.w, g.h = ArenaW, ArenaH
	g.SetArena(ArenaFixed)
	g.restart() // monta la arena

	if len(g.rocks) == 0 {
		t.Fatal("el campo fijo debe tener estructuras")
	}
	for _, r := range g.rocks {
		if !r.fixed {
			t.Error("todas las estructuras del campo fijo deben ser fijas")
		}
		if r.vx != 0 || r.vy != 0 {
			t.Error("las estructuras no deben moverse")
		}
	}

	x0, y0 := g.rocks[0].x, g.rocks[0].y
	for i := 0; i < 200; i++ { // más de 3 s de juego
		g.Step(0.016)
	}
	if g.rocks[0].x != x0 || g.rocks[0].y != y0 {
		t.Errorf("la estructura se ha movido: (%v,%v) → (%v,%v)", x0, y0, g.rocks[0].x, g.rocks[0].y)
	}
	if g.hole != nil {
		t.Error("en el campo fijo no debe aparecer el agujero negro")
	}
	if g.rockRushMult != 1 {
		t.Errorf("en el campo fijo no hay prisa de rocas: mult=%v", g.rockRushMult)
	}

	// romper un bloque lo elimina (no se parte en trozos): se abre un hueco
	n := len(g.rocks)
	for i := 0; i < fixedBlockHP; i++ {
		g.rockDamage(0, 1)
	}
	if len(g.rocks) != n-1 {
		t.Errorf("al romper una estructura debe desaparecer sin partirse: %d → %d", n, len(g.rocks))
	}
}

// TestMarcador: en PvP se cuentan las bajas del que dispara y las muertes del
// alcanzado (para la tabla del final).
func TestMarcador(t *testing.T) {
	g := New()
	g.w, g.h = ArenaW, ArenaH
	g.SetPvP(true)
	g.rocks = []astRock{{x: 5, y: 5, r: 1, hp: 1, hpMax: 1, shape: []float64{1}}}
	a := g.Local()
	b := g.AddPlayer("dos")
	a.ship.x, a.ship.y, a.ship.angle = 200, 200, 0
	a.ship.alive, a.ship.invuln, a.ship.fireTimer = true, 0, 0
	b.ship.x, b.ship.y = 260, 200
	b.ship.alive, b.ship.invuln = true, 0

	g.KeyDownFor(a, KeySpace)
	for i := 0; i < 90 && b.deaths == 0; i++ {
		b.ship.invuln = 0
		g.Step(0.016)
	}
	if a.kills != 1 {
		t.Errorf("el que dispara debería sumar una baja, tiene %d", a.kills)
	}
	if b.deaths != 1 {
		t.Errorf("el alcanzado debería sumar una muerte, tiene %d", b.deaths)
	}
}

// TestCampoFijoArena: el campo fijo no trae enemigos ni jefe, es simétrico y las
// estructuras que esconden un power-up lo sueltan al romperse.
func TestCampoFijoArena(t *testing.T) {
	g := New()
	g.w, g.h = ArenaW, ArenaH
	g.SetArena(ArenaFixed)
	g.restart()

	for i := 0; i < 400; i++ { // más de 6 s
		g.Step(0.016)
	}
	if len(g.enemies) != 0 {
		t.Errorf("en el campo fijo no debe haber enemigos, hay %d", len(g.enemies))
	}
	if g.boss != nil {
		t.Error("en el campo fijo no debe aparecer el jefe")
	}

	cx, cy := g.w/2, g.h/2
	has := func(x, y float64) bool {
		for i := range g.rocks {
			if math.Abs(g.rocks[i].x-x) < 0.01 && math.Abs(g.rocks[i].y-y) < 0.01 {
				return true
			}
		}
		return false
	}
	for i := range g.rocks {
		r := g.rocks[i]
		if !has(2*cx-r.x, r.y) || !has(r.x, 2*cy-r.y) {
			t.Errorf("la arena debe ser simétrica; falta el espejo de (%.0f,%.0f)", r.x, r.y)
		}
	}

	// romper una estructura con power-up lo suelta
	idx := -1
	for i := range g.rocks {
		if g.rocks[i].powerup >= 0 {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Skip("en esta arena no salió ningún power-up escondido")
	}
	n := len(g.powerups)
	for i := 0; i < fixedBlockHP; i++ {
		g.rockDamage(idx, 1)
	}
	if len(g.powerups) <= n {
		t.Error("al romper la estructura debería aparecer su power-up")
	}

	// y de vez en cuando van apareciendo power-ups sueltos por la arena
	spawned := false
	for i := 0; i < 2500; i++ { // hasta 40 s de juego
		g.Step(0.016)
		if len(g.powerups) > 0 {
			spawned = true
			break
		}
	}
	if !spawned {
		t.Error("en el campo fijo deberían ir apareciendo power-ups sueltos")
	}
}

// TestAparicionLejos: las naves aparecen fuera de los bloques y separadas entre
// sí (se elige el hueco más despejado).
func TestAparicionLejos(t *testing.T) {
	g := New()
	// el mundo del servidor (el doble del clásico): es el tamaño real de juego
	g.w, g.h = 1280, 960
	g.SetArena(ArenaFixed)
	g.restart()
	g.AddPlayer("dos")
	g.AddPlayer("tres")

	ps := g.Players()
	for _, p := range ps {
		for i := range g.rocks {
			r := &g.rocks[i]
			if r.fixed && math.Hypot(p.ship.x-r.x, p.ship.y-r.y) < r.r+8 {
				t.Errorf("la nave %d aparece dentro o pegada a un bloque", p.ID)
			}
		}
	}
	for i := 0; i < len(ps); i++ {
		for j := i + 1; j < len(ps); j++ {
			d := math.Hypot(ps[i].ship.x-ps[j].ship.x, ps[i].ship.y-ps[j].ship.y)
			if d < math.Min(g.w, g.h)*0.28 {
				t.Errorf("las naves %d y %d aparecen demasiado juntas: %.0f px", ps[i].ID, ps[j].ID, d)
			}
		}
	}
}

// TestPowerupUtil: no se sueltan power-ups que nadie pueda coger (la
// ametralladora solo sale si alguien lleva láser).
func TestPowerupUtil(t *testing.T) {
	g := New()
	g.w, g.h = ArenaW, ArenaH
	p := g.Local()
	p.laser = false
	for i := 0; i < 300; i++ {
		if k := g.randomPowerupKind(); k == powerupGun {
			t.Fatal("sin láser no debe salir la ametralladora: nadie podría cogerla")
		}
	}
	p.laser = true
	seen := false
	for i := 0; i < 300; i++ {
		if g.randomPowerupKind() == powerupGun {
			seen = true
			break
		}
	}
	if !seen {
		t.Error("con alguien llevando láser sí debe poder salir la ametralladora")
	}
}

// TestNoSueltaPowerupsInutiles: al reventar naves no deben quedar power-ups
// tirados que nadie pueda coger (el caso típico: asistentes cuando ya están
// todos los huecos ocupados).
func TestNoSueltaPowerupsInutiles(t *testing.T) {
	g := New()
	g.w, g.h = ArenaW, ArenaH
	p := g.Local()
	// el jugador ya lleva el máximo de asistentes y el escudo al tope
	p.orbs = make([]astOrb, orbMax)
	p.permShield = permShieldCap
	p.rapidFire = maxRapidFire
	p.laser = true
	p.doubleShot = true
	p.missilesLeft = missileStock
	p.minesLeft = mineStock

	// con todos los jugadores igual, ningún power-up de los de "recoger" sirve
	// (la ametralladora sí sirve: llevando láser te devuelve las balas)
	for _, k := range []int{powerupOrb, powerupShield, powerupRapid, powerupDouble, powerupLaser} {
		if g.powerupUseful(p, k) {
			t.Fatalf("el power-up %d no debería ser útil con todo al máximo", k)
		}
	}
	// y por tanto no se suelta
	n := len(g.powerups)
	for i := 0; i < 200; i++ {
		g.spawnPowerupIfUseful(100, 100, powerupOrb)
	}
	if len(g.powerups) != n {
		t.Errorf("no debería soltarse ni un asistente más: %d -> %d", n, len(g.powerups))
	}
	// al perder los asistentes, vuelven a soltarse
	p.orbs = nil
	g.spawnPowerupIfUseful(100, 100, powerupOrb)
	if len(g.powerups) != n+1 {
		t.Errorf("sin asistentes sí debe soltarse: %d", len(g.powerups))
	}
}

// TestRocasSinPowerup: romper un asteroide en la arena de asteroides NO debe
// soltar power-ups (el campo "powerup" a cero significaba "asistente", y cada
// roca rota dejaba una bolita).
func TestRocasSinPowerup(t *testing.T) {
	g := New()
	g.w, g.h = ArenaW, ArenaH
	g.SetArena(ArenaRocks)
	g.restart()
	for _, r := range g.rocks {
		if r.powerup >= 0 {
			t.Fatalf("un asteroide nace con power-up dentro (%d)", r.powerup)
		}
	}
	// romper las rocas gordas tampoco: no debe aparecer ningún power-up
	n := len(g.powerups)
	for i := len(g.rocks) - 1; i >= 0; i-- {
		for hits := 0; hits < 10 && i < len(g.rocks); hits++ {
			g.rockDamage(i, 1)
		}
	}
	if len(g.powerups) != n {
		t.Errorf("romper asteroides no debe soltar power-ups: %d → %d", n, len(g.powerups))
	}
	// y en el campo fijo sí los esconde (eso es a propósito)
	g2 := New()
	g2.w, g2.h = ArenaW, ArenaH
	g2.SetArena(ArenaFixed)
	g2.restart()
	found := false
	for _, r := range g2.rocks {
		if r.powerup >= 0 {
			found = true
		}
	}
	if !found {
		t.Error("el campo fijo sí debe esconder power-ups en algunas estructuras")
	}
}

// TestMisilesPvP: en 'vs' se lleva UN misil (y no se recarga) y persigue
// SIEMPRE a las naves: nunca a los asteroides ni a las estructuras fijas. En
// cooperativo sí persigue asteroides.
func TestMisilesPvP(t *testing.T) {
	g := New()
	g.SetSize(1280, 960)
	g.SetArena(ArenaFixed)
	g.restart()
	g.SetSize(1280, 960)
	g.players = nil // fuera el jugador local que crea New(): solo ana y el rival
	a := g.AddPlayer("ana")
	a.ship.alive, a.ship.invuln = true, 0
	a.ship.x, a.ship.y = 200, 240
	if a.missilesLeft != 1 {
		t.Errorf("debe empezar con 1 misil, lleva %d", a.missilesLeft)
	}
	g.SetPvP(true)
	if n := g.missileStockLocked(); n != 1 {
		t.Errorf("la munición de misiles debe ser 1 en los dos modos, es %d", n)
	}
	b := g.AddPlayer("rival")
	b.ship.alive, b.ship.invuln = true, 0
	b.ship.x, b.ship.y = 1000, 240

	// en 'vs' el objetivo es la NAVE aunque haya un asteroide mucho más cerca
	g.rocks = append(g.rocks, astRock{x: 240, y: 240, r: 30, hp: 3, hpMax: 3, shape: []float64{1}})
	tx, ty, ok := g.nearestEnemyFor(a.ship.x, a.ship.y, a.ID)
	if !ok {
		t.Fatal("debe encontrar al rival")
	}
	if math.Hypot(tx-1000, ty-240) > 1e-9 {
		t.Errorf("en 'vs' el misil debe ir a por la nave (1000,240), va a (%.0f,%.0f): las rocas no son objetivo", tx, ty)
	}

	// sin naves, en 'vs' no hay objetivo (ni estructuras ni asteroides)
	g2 := New()
	g2.SetSize(1280, 960)
	g2.SetArena(ArenaFixed)
	g2.restart()
	g2.SetSize(1280, 960)
	g2.SetPvP(true)
	g2.players = nil
	p := newPlayer(99, "test")
	p.ship.alive = true
	if _, _, ok := g2.nearestEnemyFor(p.ship.x, p.ship.y, p.ID); ok {
		t.Error("en 'vs' sin naves el misil no debe perseguir estructuras ni asteroides")
	}

	// en cooperativo sí persigue asteroides
	g3 := New()
	g3.SetSize(1280, 960)
	g3.SetArena(ArenaRocks)
	g3.restart()
	g3.SetSize(1280, 960)
	g3.players = nil
	g3.rocks = []astRock{{x: 500, y: 500, r: 20, hp: 1, hpMax: 1, shape: []float64{1}}}
	p3 := newPlayer(99, "test")
	p3.ship.alive = true
	tx3, ty3, ok3 := g3.nearestEnemyFor(100, 100, p3.ID)
	if !ok3 {
		t.Fatal("en cooperativo el asteroide debe ser objetivo")
	}
	if math.Hypot(tx3-500, ty3-500) > 1e-9 {
		t.Errorf("en cooperativo el misil debe ir al asteroide, va a (%.0f,%.0f)", tx3, ty3)
	}
}

// TestMisiles: se empieza con UN misil en los dos modos, no se recarga y el
// power-up de misiles da 2 (en coop y en 'vs').
func TestMisiles(t *testing.T) {
	g := New()
	g.SetSize(1280, 960)
	g.SetArena(ArenaRocks)
	g.restart()
	g.SetSize(1280, 960)
	b := g.AddPlayer("rival")
	if b.missilesLeft != 1 {
		t.Errorf("en cooperativo debería empezar con 1 misil, lleva %d", b.missilesLeft)
	}
	g.SetPvP(true)
	if b.missilesLeft != 1 {
		t.Errorf("en PvP debería empezar con 1 misil, lleva %d", b.missilesLeft)
	}
	// no se recarga: por mucho tiempo que pase, sigue con lo que tenga
	b.missilesLeft = 0
	b.missileReload = 1000
	g.Step(0.016)
	if b.missilesLeft != 0 {
		t.Errorf("los misiles no se recargan: debería seguir en 0, está en %d", b.missilesLeft)
	}
	// el power-up de misiles da DOS (en los dos modos)
	b.missilesLeft = 1
	g.applyPowerupLocked(b, powerupMissiles)
	if b.missilesLeft != 1+missilePowerupAmmo {
		t.Errorf("el power-up debe dar %d misiles: 1 + %d = %d, quedó %d",
			missilePowerupAmmo, missilePowerupAmmo, 1+missilePowerupAmmo, b.missilesLeft)
	}
	if missilePowerupAmmo != 2 {
		t.Errorf("el power-up de misiles debe dar 2, da %d", missilePowerupAmmo)
	}
	// y en cooperativo igual
	g.SetPvP(false)
	b.missilesLeft = 1
	g.applyPowerupLocked(b, powerupMissiles)
	if b.missilesLeft != 3 {
		t.Errorf("en cooperativo el power-up debe dar 2 misiles, quedó %d", b.missilesLeft)
	}
}

// TestMisilVelocidades: en cooperativo los misiles van más lentos que en 'vs'
// (allí persiguen asteroides; en 'vs' van a por naves que huyen).
func TestMisilVelocidades(t *testing.T) {
	g := New()
	g.SetSize(1280, 960)
	g.SetArena(ArenaRocks)
	g.restart()
	coop := g.missileSpeedLocked()
	g.SetPvP(true)
	pvp := g.missileSpeedLocked()
	if coop >= pvp {
		t.Errorf("en cooperativo deben ir más lentos que en 'vs': coop=%v vs=%v", coop, pvp)
	}
	if coop <= 0 || pvp <= 0 {
		t.Errorf("ninguna velocidad puede ser cero: coop=%v vs=%v", coop, pvp)
	}
}

// TestPowerupsSobrevivenALaMuerte: los power-ups que flotan en la arena son de
// todos y NO se borran al morir nadie. Antes, cada muerte los vaciaba: con bots
// muriendo todo el rato, desaparecían en segundos en vez de durar ~36 s.
func TestPowerupsSobrevivenALaMuerte(t *testing.T) {
	g := New()
	g.w, g.h = ArenaW, ArenaH
	g.SetArena(ArenaRocks)
	g.restart()
	g.rocks = []astRock{{x: 5, y: 5, r: 1, hp: 1, hpMax: 1, shape: []float64{1}}}
	a := g.AddPlayer("ana")
	b := g.AddPlayer("rival")
	a.ship.alive, a.ship.invuln = true, 0
	b.ship.alive, b.ship.invuln = true, 0
	a.ship.x, a.ship.y = 300, 300
	b.ship.x, b.ship.y = 900, 700
	g.spawnPowerup(640, 480, 0)
	g.spawnPowerup(200, 700, 1)
	if len(g.powerups) != 2 {
		t.Fatalf("debería haber 2 power-ups, hay %d", len(g.powerups))
	}
	g.killShip(a)
	if len(g.powerups) != 2 {
		t.Errorf("morir no debe borrar los power-ups: quedan %d de 2", len(g.powerups))
	}
}

// TestReaparecerLimpiaMinas: al materializarse, el sitio de reaparición se queda
// sin minas (si no, apareces encima de una y mueres sin poder hacer nada; las
// minas matan aunque estés invulnerable).
func TestReaparecerLimpiaMinas(t *testing.T) {
	g := New()
	g.w, g.h = 1280, 960
	g.SetArena(ArenaRocks)
	g.restart()
	g.rocks = []astRock{{x: 5, y: 5, r: 1, hp: 1, hpMax: 1, fixed: true, shape: []float64{1}}}
	a := g.AddPlayer("ana")
	b := g.AddPlayer("rival")
	a.ship.alive, a.ship.invuln = true, 0
	b.ship.alive, b.ship.invuln = true, 0

	g.killShip(a)
	// minas sembradas en el sitio donde va a reaparecer (y una lejos)
	g.mines = []astMine{
		{x: a.spawnX, y: a.spawnY, arm: 0, r: 11, life: 30},
		{x: a.spawnX + 60, y: a.spawnY, arm: 0, r: 11, life: 30},
		{x: a.spawnX + 400, y: a.spawnY, arm: 0, r: 11, life: 30},
	}
	for i := 0; i < 200 && !a.ship.alive; i++ {
		b.ship.x, b.ship.y = 1200, 900 // el rival, lejos de todo
		g.Step(0.016)
	}
	if !a.ship.alive {
		t.Fatal("debería haber reaparecido")
	}
	for _, m := range g.mines {
		if d := math.Hypot(m.x-a.spawnX, m.y-a.spawnY); d < mineClearRadius {
			t.Errorf("queda una mina a %.0f px del sitio de reaparición", d)
		}
	}
	if len(g.mines) != 1 {
		t.Errorf("la mina lejana no se debe tocar: quedan %d minas (se esperaba 1)", len(g.mines))
	}
}

// TestDisparoAlineadoConLaNave: la bala sale del morro y en la MISMA dirección
// en la que apunta la nave, aunque la nave vaya derivando a tope. Antes heredaba
// la velocidad de la nave, así que la trayectoria se torcía (hasta 30° yendo
// rápido): salía del morro pero se iba de lado y se veía desalineada.
func TestDisparoAlineadoConLaNave(t *testing.T) {
	g := New()
	g.w, g.h = 1280, 960
	g.SetArena(ArenaRocks)
	g.restart()
	g.rocks = []astRock{{x: 5, y: 5, r: 1, hp: 1, hpMax: 1, fixed: true, shape: []float64{1}}}
	a := g.AddPlayer("ana")
	a.ship.alive, a.ship.invuln = true, 0
	a.ship.x, a.ship.y = 400, 400
	g.SetAimLead(a, 0)

	for _, ang := range []float64{0, math.Pi / 2, -1.1, 2.7} {
		a.ship.angle = ang
		a.ship.vx, a.ship.vy = 260, -180 // derivando fuerte
		a.ship.fireTimer = 0
		g.SetAimPose(a, a.ship.x, a.ship.y, ang)
		g.bullets = nil
		g.fireLocked(a)
		if len(g.bullets) != 1 {
			t.Fatalf("debería salir 1 bala, salieron %d", len(g.bullets))
		}
		b := g.bullets[0]
		if got := math.Atan2(b.vy, b.vx); math.Abs(got-ang) > 1e-9 {
			t.Errorf("apuntando a %.3f rad la bala va a %.3f (desviada %.1f°)",
				ang, got, (got-ang)*180/math.Pi)
		}
	}
}

// TestDerivaMasCorta: al soltar el motor la nave se frena antes (~20 % menos de
// deriva) pero la velocidad punta y lo que tarda en alcanzarla no cambian: solo
// cambia lo que resbala.
func TestDerivaMasCorta(t *testing.T) {
	g := New()
	g.SetSize(1280, 960) // como el servidor (ArenaW/ArenaH son la mitad)
	g.SetArena(ArenaRocks)
	g.restart()
	g.SetSize(1280, 960)
	g.rocks = []astRock{{x: 5, y: 5, r: 1, hp: 1, hpMax: 1, fixed: true, shape: []float64{1}}}
	g.hole = nil // sin agujero: su gravedad curva la deriva y descuadra la medida
	p := g.AddPlayer("ana")
	p.ship.alive, p.ship.invuln = true, 0
	p.ship.angle = 0
	p.ship.x, p.ship.y = 120, 480

	// aceleramos hasta el tope
	g.SetKey(p, KeyUp, true)
	reach := -1
	for i := 0; i < 900; i++ {
		g.Step(0.016)
		if reach < 0 && math.Hypot(p.ship.vx, p.ship.vy) >= shipSpeedMax-1 {
			reach = i
		}
	}
	v := math.Hypot(p.ship.vx, p.ship.vy)
	if math.Abs(v-shipSpeedMax) > 2 {
		t.Errorf("la velocidad punta debe ser ~%.0f, es %.0f", shipSpeedMax, v)
	}

	// soltamos y medimos cuánto resbala hasta casi pararse. Se suma la velocidad
	// por el paso (el avance real de cada frame): la distancia recta no vale
	// porque la nave da la vuelta por los bordes del campo.
	g.SetKey(p, KeyUp, false)
	steps := 0
	d := 0.0
	for math.Hypot(p.ship.vx, p.ship.vy) > 20 && steps < 4000 {
		g.Step(0.016)
		steps++
		d += math.Hypot(p.ship.vx, p.ship.vy) * 0.016
	}
	t.Logf("punta %.0f px/s (en %.2f s) · deriva %.0f px en %.2f s", v, float64(reach)*0.016, d, float64(steps)*0.016)
	if reach <= 0 || float64(reach)*0.016 > 3.0 {
		t.Errorf("debería llegar al tope en ~2,4 s, tardó %.2f s", float64(reach)*0.016)
	}
	// Deriva esperada: sumando la serie geométrica del rozamiento por paso,
	// desde shipSpeedMax hasta los 20 px/s en los que cortamos la medida.
	// (Con el rozamiento viejo, 0.992, esto daba ~600 px: ahora ~480, un 20 %
	// menos.)
	want := (shipSpeedMax - 20) * 0.016 / (1 - shipFriction)
	if math.Abs(d-want) > want*0.06 {
		t.Errorf("la deriva debería ser ~%.0f px, es %.0f px", want, d)
	}
}

// TestCascoContraBloques: chocar contra un bloque ya no mata de una vez. Quita un
// punto de casco (3 golpes y la nave revienta) y te saca del bloque.
func TestCascoContraBloques(t *testing.T) {
	g := New()
	g.SetSize(1280, 960)
	g.SetArena(ArenaFixed)
	g.restart()
	g.SetSize(1280, 960)
	g.SetPvP(true)
	p := g.AddPlayer("ana")
	p.ship.alive, p.ship.invuln = true, 0

	var r *astRock
	for i := range g.rocks {
		if g.rocks[i].fixed {
			r = &g.rocks[i]
			break
		}
	}
	if r == nil {
		t.Fatal("la arena fija debería tener bloques")
	}
	if p.hull != shipHullMax {
		t.Fatalf("el casco debe empezar a %d, es %d", shipHullMax, p.hull)
	}

	for golpe := 1; golpe <= shipHullMax; golpe++ {
		p.ship.x, p.ship.y = r.x, r.y // dentro del bloque: golpe limpio
		p.ship.vx, p.ship.vy = 0, 0
		p.ship.invuln = 0
		g.Step(0.016)

		if golpe < shipHullMax {
			if p.deaths > 0 {
				t.Fatalf("no debería reventar al golpe %d de %d", golpe, shipHullMax)
			}
			want := shipHullMax - golpe
			if p.hull != want {
				t.Errorf("tras %d golpes el casco debería ser %d, es %d", golpe, want, p.hull)
			}
			if d := math.Hypot(p.ship.x-r.x, p.ship.y-r.y); d <= r.r {
				t.Errorf("la nave se quedó dentro del bloque (a %.0f px, radio %.0f)", d, r.r)
			}
		}
	}
	if p.deaths == 0 {
		t.Errorf("al golpe %d la nave debería reventar (casco %d)", shipHullMax, p.hull)
	}
}

// TestCascoLlegaAlCliente: el casco va en el snapshot (el HUD del cliente lo
// enseña, y si no viajara se vería siempre lleno).
func TestCascoLlegaAlCliente(t *testing.T) {
	srv := New()
	srv.SetSize(1280, 960)
	srv.SetArena(ArenaRocks)
	srv.restart()
	srv.SetSize(1280, 960)
	p := srv.AddPlayer("ana")
	p.hull = 1

	cli := New()
	if err := cli.Load(srv.Snapshot()); err != nil {
		t.Fatal(err)
	}
	cp := cli.PlayerByID(p.ID)
	if cp == nil {
		t.Fatal("el cliente no encuentra al jugador")
	}
	if cp.hull != 1 {
		t.Errorf("el cliente debería ver el casco a 1, ve %d", cp.hull)
	}
}
