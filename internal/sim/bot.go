package sim

import (
	"math"
	"math/rand"
)

// Bots: un piloto automático sencillo para poder probar el multijugador sin
// gente. El servidor llama a BotInput (movimiento/disparo) y a BotActions
// (minas, misiles, railgun, escudo) en cada tick; la simulación no distingue a
// un bot de un jugador.

// Niveles de bot: cambian lo fino que apuntan, lo que tardan en reaccionar, lo
// que usan sus armas y lo que se apartan de los obstáculos.
const (
	BotDumb   = 0 // muy torpe
	BotNormal = 1 // medio
	BotSharp  = 2 // bueno
)

// botCfg son los ajustes de cada nivel.
type botCfg struct {
	think   float64 // cada cuánto revisa su decisión (s)
	aimTol  float64 // margen de apuntado para disparar (rad)
	rangeF  float64 // alcance de tiro (px)
	turnTol float64 // margen para girar (rad)
	dodge   float64 // a qué distancia empieza a esquivar (px)
	farGoal float64 // a qué distancia empuja para acercarse (px)
	items   float64 // probabilidad de usar minas/misiles/railgun/escudo
	seek    float64 // probabilidad de ir a por un power-up cercano
	shields float64 // probabilidad de levantar el escudo si le disparan cerca
}

var botConf = [3]botCfg{
	{think: 0.55, aimTol: 0.60, rangeF: 280, turnTol: 0.35, dodge: 60, farGoal: 150,
		items: 0.15, seek: 0.25, shields: 0.10},
	{think: 0.22, aimTol: 0.28, rangeF: 430, turnTol: 0.06, dodge: 100, farGoal: 80,
		items: 0.45, seek: 0.60, shields: 0.35},
	{think: 0.08, aimTol: 0.13, rangeF: 560, turnTol: 0.02, dodge: 135, farGoal: 70,
		items: 0.80, seek: 0.90, shields: 0.70},
}

// SetBotLevel fija el nivel de un bot (0 torpe, 1 medio, 2 bueno).
func (g *Sim) SetBotLevel(p *Player, level int) {
	if p == nil {
		return
	}
	if level < 0 || level > 2 {
		level = BotNormal
	}
	g.mu.Lock()
	p.botLevel = level
	g.mu.Unlock()
}

// BotInput decide qué haría un bot en este instante: girar hacia su objetivo (o
// hacia un power-up que le sirva), empujar si lo tiene lejos y disparar si lo
// tiene a tiro, apartándose de lo que tenga justo delante.
func (g *Sim) BotInput(p *Player) (left, right, thrust, fire bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := p.ship
	if !s.alive || g.w == 0 {
		return
	}
	cfg := botConf[p.botLevel]
	// reacción: los torpes tardan en volver a decidir (mantienen lo anterior)
	if p.botThink > 0 {
		p.botThink -= 0.016
		return p.botL, p.botR, p.botT, p.botF
	}
	p.botThink = cfg.think
	defer func() {
		p.botL, p.botR, p.botT, p.botF = left, right, thrust, fire
	}()

	// lo primero, no estrellarse: si tiene un obstáculo delante, gira y empuja
	if g.botBlockedAheadLocked(s, cfg.dodge) {
		if g.botAvoidSideLocked(s, cfg.dodge) > 0 {
			return false, true, true, false
		}
		return true, false, true, false
	}
	// si hay un power-up que le sirve cerca, va a por él
	if rand.Float64() < cfg.seek {
		if px, py, ok := g.botPowerupLocked(p); ok {
			return g.botSteerLocked(s, px, py, cfg, false)
		}
	}
	tx, ty, ok := g.botTargetLocked(p)
	if !ok {
		return false, false, true, false // sin objetivo: avanza
	}
	return g.botSteerLocked(s, tx, ty, cfg, true)
}

// BotActions devuelve las teclas de un solo uso que el bot pulsa ahora mismo:
// minas (K), misiles (M), rayo letal (L), railgun (G para cargar, "-G" para
// soltar) y escudo (S). El servidor las entrega con KeyDownFor/KeyUpFor.
func (g *Sim) BotActions(p *Player) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := p.ship
	if !s.alive {
		return nil
	}
	cfg := botConf[p.botLevel]
	var out []string

	// railgun: si lo tiene cargando, lo suelta cuando ya lleva un rato
	if p.botRailHold > 0 {
		p.botRailHold--
		out = append(out, "-G")
		return out
	}
	tx, ty, has := g.botTargetLocked(p)
	dist, aligned := 0.0, false
	if has {
		dist = math.Hypot(tx-s.x, ty-s.y)
		ang := math.Atan2(ty-s.y, tx-s.x)
		aligned = math.Abs(normAngle(ang-s.angle)) < cfg.aimTol*1.6
	}

	// escudo: si le están disparando cerca y tiene escudo manual disponible
	if p.shieldT <= 0 && p.shieldCd <= 0 && rand.Float64() < cfg.shields {
		for i := range g.enemyShots {
			if math.Hypot(g.enemyShots[i].x-s.x, g.enemyShots[i].y-s.y) < 120 {
				out = append(out, KeyS)
				break
			}
		}
		for i := range g.bullets { // balas de otros (PvP)
			if g.bullets[i].owner == p.ID || g.bullets[i].pink {
				continue
			}
			if math.Hypot(g.bullets[i].x-s.x, g.bullets[i].y-s.y) < 120 {
				out = append(out, KeyS)
				break
			}
		}
	}

	// trastos: solo de vez en cuando (y más a menudo si el bot es bueno). Se
	// sortea entre los que tenga a mano, para que use todos y no siempre el mismo.
	if has && rand.Float64() < cfg.items {
		var opts []string
		if p.railCharge >= p.railRechargeTotal*0.3 && dist < 800 {
			opts = append(opts, "rail")
		}
		if p.minesLeft > 0 && dist < 300 {
			opts = append(opts, "minas")
		}
		if p.missilesLeft > 0 && aligned && dist > 150 && dist < 700 {
			opts = append(opts, "misil")
		}
		if !g.pvp && p.lethalCd <= 0 {
			opts = append(opts, "letal") // el rayo letal es solo para asteroides
		}
		if len(opts) > 0 {
			switch opts[rand.Intn(len(opts))] {
			case "rail":
				out = append(out, "G") // empieza a cargar; lo suelta a los 3 ticks
				p.botRailHold = 3
			case "minas":
				out = append(out, "K")
			case "misil":
				out = append(out, "M")
			case "letal":
				out = append(out, "L")
			}
		}
	}
	return out
}

// botSteerLocked decide giro, empuje y disparo hacia un punto. shoot dice si
// puede disparar (cuando va a por un power-up, no).
func (g *Sim) botSteerLocked(s astShip, tx, ty float64, cfg botCfg, shoot bool) (left, right, thrust, fire bool) {
	ang := math.Atan2(ty-s.y, tx-s.x)
	d := normAngle(ang - s.angle)
	dist := math.Hypot(tx-s.x, ty-s.y)
	if d < -cfg.turnTol {
		left = true
	}
	if d > cfg.turnTol {
		right = true
	}
	if math.Abs(d) < 1.3 && dist > cfg.farGoal {
		thrust = true
	}
	if dist < 45 {
		thrust = false
	}
	if shoot && math.Abs(d) < cfg.aimTol && dist < cfg.rangeF {
		fire = true
	}
	return
}

// botPowerupLocked busca un power-up que le sirva y esté cerca.
func (g *Sim) botPowerupLocked(p *Player) (float64, float64, bool) {
	s := p.ship
	best := 420.0
	var bx, by float64
	ok := false
	for _, pu := range g.powerups {
		if !g.powerupUseful(p, pu.kind) {
			continue
		}
		if d := math.Hypot(pu.x-s.x, pu.y-s.y); d < best {
			best, bx, by, ok = d, pu.x, pu.y, true
		}
	}
	return bx, by, ok
}

// botTargetLocked elige el objetivo: en PvP el jugador vivo más cercano y, si no
// hay (o en cooperativo), la roca o nave enemiga más cercana.
func (g *Sim) botTargetLocked(p *Player) (float64, float64, bool) {
	s := p.ship
	best := math.MaxFloat64
	var bx, by float64
	ok := false
	consider := func(x, y float64) {
		if d := math.Hypot(x-s.x, y-s.y); d < best {
			best, bx, by, ok = d, x, y, true
		}
	}
	if g.pvp {
		for _, o := range g.players {
			if o == p || !o.ship.alive {
				continue
			}
			consider(o.ship.x, o.ship.y)
		}
		if ok {
			return bx, by, true
		}
	}
	for i := range g.rocks {
		consider(g.rocks[i].x, g.rocks[i].y)
	}
	for i := range g.enemies {
		consider(g.enemies[i].x, g.enemies[i].y)
	}
	if g.boss != nil {
		consider(g.boss.x, g.boss.y)
	}
	return bx, by, ok
}

// botBlockedAheadLocked indica si hay una roca (o una nave enemiga) en la
// trayectoria de la nave, a menos de `reach` px.
func (g *Sim) botBlockedAheadLocked(s astShip, reach float64) bool {
	ax, ay := math.Cos(s.angle), math.Sin(s.angle)
	near := func(x, y, r float64) bool {
		fwd := (x-s.x)*ax + (y-s.y)*ay
		if fwd < 0 || fwd > reach {
			return false
		}
		side := math.Abs((x-s.x)*(-ay) + (y-s.y)*ax)
		return side < r+18
	}
	for i := range g.rocks {
		if near(g.rocks[i].x, g.rocks[i].y, g.rocks[i].r) {
			return true
		}
	}
	for i := range g.enemies {
		if near(g.enemies[i].x, g.enemies[i].y, g.enemies[i].r) {
			return true
		}
	}
	return false
}

// botAvoidSideLocked dice hacia qué lado le conviene girar para esquivar: +1 a la
// derecha, -1 a la izquierda (según de qué lado está el obstáculo más cercano).
func (g *Sim) botAvoidSideLocked(s astShip, reach float64) float64 {
	ax, ay := math.Cos(s.angle), math.Sin(s.angle)
	best := math.MaxFloat64
	side := 1.0
	check := func(x, y, r float64) {
		fwd := (x-s.x)*ax + (y-s.y)*ay
		if fwd < 0 || fwd > reach {
			return
		}
		lat := (x-s.x)*(-ay) + (y-s.y)*ax
		if math.Abs(lat) > r+18 {
			return
		}
		if d := math.Hypot(x-s.x, y-s.y); d < best {
			best = d
			if lat > 0 {
				side = -1 // está a la izquierda: giro a la derecha
			} else {
				side = 1
			}
		}
	}
	for i := range g.rocks {
		check(g.rocks[i].x, g.rocks[i].y, g.rocks[i].r)
	}
	for i := range g.enemies {
		check(g.enemies[i].x, g.enemies[i].y, g.enemies[i].r)
	}
	return side
}
