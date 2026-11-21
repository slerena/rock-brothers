package sim

import (
	"image/color"
	"math"
	"math/rand"
	"time"
)

// Acceso al tamaño del mundo para la plataforma que NO dibuja (el servidor) y
// para el cliente, que necesita saber a qué campo escalar el lienzo.

// SetSize fija el tamaño del mundo. Lo usa el servidor: allí no hay ventana que
// medir, así que el campo lo decide él y es el mismo para todos los jugadores.
func (g *Sim) SetSize(w, h float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Si el mundo no estaba medido, los jugadores ya creados están colocados a
	// ojo (el marcador de "sin medidas"): se reparten bien ahora que sí lo está.
	first := g.w == 0 && g.h == 0
	g.w, g.h = w, h
	if first {
		for _, p := range g.players {
			g.placePlayerLocked(p)
		}
	}
}

// WorldSize devuelve el tamaño del mundo en unidades. El cliente web lo usa para
// escalar el lienzo al campo que manda el servidor (y no al tamaño de su
// ventana): así todos ven el mismo campo.
func (g *Sim) WorldSize() (w, h float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.w, g.h
}

// Pos devuelve la posición de la nave del jugador (para tests y para la red).
func (p *Player) Pos() (x, y float64) { return p.ship.x, p.ship.y }

// Restart reinicia la partida (nivel 1, vidas y marcadores a cero) sin tocar el
// récord. Lo usa el servidor al empezar cada partida.
func (g *Sim) Restart() { g.restart() }

// RemovePlayer saca a un jugador de la partida (se ha desconectado). Los IDs de
// los demás no cambian: solo desaparece de la lista.
func (g *Sim) RemovePlayer(p *Player) {
	if p == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, q := range g.players {
		if q == p {
			g.players = append(g.players[:i], g.players[i+1:]...)
			return
		}
	}
}

// NewRoom crea una simulación SIN jugadores: la usa el servidor, que añade uno
// por cada cliente que entra. El mundo (rocas, nivel, cronómetro) ya está
// montado, como en una partida normal.
func NewRoom() *Sim {
	g := New()
	g.mu.Lock()
	g.players = nil
	g.mu.Unlock()
	return g
}

// Over indica que la partida ha terminado (no le quedan vidas a nadie). Lo usa
// el servidor para volver a la sala de espera.
func (g *Sim) Over() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.over
}

// Muted indica si ESTE cliente tiene el sonido silenciado (tecla X). El silencio
// es local: en multijugador el servidor sigue mandando los sonidos y cada
// cliente decide si los reproduce.
func (g *Sim) Muted() bool { return g.muted.Load() }

// SetPlayerName cambia el nombre visible de un jugador (lo usa el servidor
// cuando alguien lo edita en la sala de espera).
func (g *Sim) SetPlayerName(p *Player, name string) {
	if p == nil || name == "" {
		return
	}
	g.mu.Lock()
	p.Name = name
	g.mu.Unlock()
}

// SetPvP fija el modo de la partida: en PvP (todos contra todos) las armas hacen
// daño a los demás jugadores y gana el último en pie. Lo pone el servidor según
// lo que elijan en la sala de espera.
func (g *Sim) SetPvP(v bool) {
	g.mu.Lock()
	g.pvp = v
	if v {
		// en PvP solo se lleva un misil (se recarga solo)
		for _, p := range g.players {
			if p.missilesLeft > 1 {
				p.missilesLeft = 1
			}
		}
	}
	g.mu.Unlock()
}

// PvP indica si la partida es todos contra todos.
func (g *Sim) PvP() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.pvp
}

// damagePlayer aplica el impacto de otro jugador a p: el escudo permanente lo
// absorbe gastándose, el escudo temporal lo para, y sin escudo la nave muere.
// `by` es quien lo causó (para el marcador; puede ser nil, p. ej. una bomba
// propia).
func (g *Sim) damagePlayer(p *Player, cost float64, by *Player) {
	if p == nil || !p.ship.alive || p.ship.invuln > 0 {
		return
	}
	if p.permShield > 0 {
		g.spendShield(p, cost)
		g.playSound("shield", 120*time.Millisecond)
		p.ship.invuln = math.Max(p.ship.invuln, 0.3)
		return
	}
	if p.shieldT > 0 {
		g.playSound("shield", 120*time.Millisecond)
		p.ship.invuln = math.Max(p.ship.invuln, 0.3)
		return
	}
	// marcador: la muerte se apunta a quien la causó (si fue otro jugador)
	if by != nil && by != p {
		by.kills++
	}
	g.killShip(p)
}

// matchOver indica que la partida ha terminado: en cooperativo cuando no le
// quedan vidas a nadie; en PvP cuando solo queda uno en pie (y hay al menos dos,
// para que una partida de uno solo no acabe al instante).
func (g *Sim) matchOver() bool {
	if !g.pvp {
		return g.allPlayersDead()
	}
	alive := 0
	for _, p := range g.players {
		if p.lives >= 0 {
			alive++
		}
	}
	return alive <= 1 && len(g.players) >= 2
}

// distToSegment devuelve la distancia del punto (px,py) al segmento (ax,ay)-(bx,by).
// La usan las colisiones de los proyectiles: como se mueven mucho por frame (el
// láser, ~29 px), hay que mirar todo el recorrido y no solo el punto final.
func distToSegment(ax, ay, bx, by, px, py float64) float64 {
	dx, dy := bx-ax, by-ay
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return math.Hypot(px-ax, py-ay)
	}
	t := ((px-ax)*dx + (py-ay)*dy) / l2
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}

// ---------- tipos de arena ----------

const (
	// ArenaRocks es la arena de siempre: asteroides que se mueven y se parten.
	ArenaRocks = "rocks"
	// ArenaFixed es el "campo fijo": estructuras que NO se mueven y se pueden
	// romper a disparos (abriendo túneles). Sin agujero negro.
	ArenaFixed = "fixed"
	// fixedBlockHP son los disparos que aguanta cada bloque del campo fijo.
	fixedBlockHP = 3
	// powerupSpawnEvery es cada cuánto (más un poco al azar) aparece un power-up
	// suelto en el campo fijo.
	powerupSpawnEvery = 14.0
	// powerupTTL es lo que dura un power-up en la arena antes de desvanecerse.
	powerupTTL = 36.0
	// shipHullMax son los golpes que aguanta el casco contra los bloques antes de
	// reventar: chocar ya no mata de una vez.
	shipHullMax = 3
	// hullRegenEvery es lo que tarda el casco en recuperar un punto (si no se
	// vuelve a golpear): así los roces no se van acumulando toda la partida.
	hullRegenEvery = 6.0
	// mineClearRadius es el radio que se deja libre de minas al reaparecer.
	mineClearRadius = 150.0
	// bulletMissileHitR es el radio con el que una bala revienta un misil: es
	// generoso a propósito, para que valga con pasarles cerca (no de lleno).
	bulletMissileHitR = 20.0
	// powerupBlink es el tiempo final en el que parpadea para avisar de que se va.
	powerupBlink = 10.0
	// ShipTurnRate es lo rápido que gira la nave (rad/s). Se usa también en la
	// predicción del cliente, para que gire igual.
	ShipTurnRate = 4.40 // acumulado desde el original: +30 %, +10 %, +5 %, -10 % y -10 %
)

// SetArena fija el tipo de arena (la elige el servidor en la sala de espera).
func (g *Sim) SetArena(kind string) {
	if kind != ArenaFixed {
		kind = ArenaRocks
	}
	g.mu.Lock()
	g.arena = kind
	g.mu.Unlock()
}

// Arena devuelve el tipo de arena actual.
func (g *Sim) Arena() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.arena == ArenaFixed {
		return ArenaFixed
	}
	return ArenaRocks
}

// fixedArena indica si es el campo fijo.
func (g *Sim) fixedArena() bool { return g.arena == ArenaFixed }

// buildFixedArenaLocked monta las estructuras del campo fijo. Cambian en cada
// partida, pero SIEMPRE son simétricas (se sortean y se replican en espejo), para
// que nadie salga con ventaja. Algunas esconden un power-up dentro (se ve a
// través de la estructura y sale al romperla). El llamador tiene el lock tomado.
func (g *Sim) buildFixedArenaLocked(w, h float64) {
	const step = 44.0
	const rad = 21.0 // casi pegados: hay que abrirse paso a tiros
	seen := map[[2]float64]bool{}
	block := func(x, y float64) {
		key := [2]float64{x, y}
		if seen[key] {
			return
		}
		seen[key] = true
		pu := -1
		if rand.Float64() < 0.18 {
			pu = g.randomPowerupKind()
		}
		g.rocks = append(g.rocks, astRock{
			x: x, y: y, r: rad, fixed: true,
			hp: fixedBlockHP, hpMax: fixedBlockHP,
			shape:   []float64{1, 1, 1, 1},
			powerup: pu,
		})
	}
	cx, cy := w/2, h/2
	// cruz central (el centro siempre queda libre, como hueco de paso)
	for k := 1; k <= 5; k++ {
		if rand.Float64() < 0.8 {
			block(cx, cy+float64(k)*step)
			block(cx, cy-float64(k)*step)
		}
	}
	for k := 1; k <= 7; k++ {
		if rand.Float64() < 0.7 {
			block(cx+float64(k)*step, cy)
			block(cx-float64(k)*step, cy)
		}
	}
	// diagonales: un par de bloques por cuadrante, en espejo
	for k := 0; k < 3; k++ {
		dx := float64(2+rand.Intn(5)) * step
		dy := float64(2+rand.Intn(4)) * step
		block(cx+dx, cy+dy)
		block(cx-dx, cy+dy)
		block(cx+dx, cy-dy)
		block(cx-dx, cy-dy)
	}
}

// randomPowerupKind sortea qué power-up esconder o soltar en el campo fijo.
// Solo salen los que le sirven a ALGUIEN: si no, quedarían ahí sin poder cogerse
// (por ejemplo la ametralladora cuando nadie lleva láser).
func (g *Sim) randomPowerupKind() int {
	kinds := []int{
		powerupRapid, powerupOrb, powerupMissiles, powerupMines,
		powerupLaser, powerupGun, powerupDouble, powerupShield,
	}
	useful := make([]int, 0, len(kinds))
	for _, k := range kinds {
		for _, p := range g.players {
			if g.powerupUseful(p, k) {
				useful = append(useful, k)
				break
			}
		}
	}
	if len(useful) == 0 {
		return powerupRapid
	}
	return useful[rand.Intn(len(useful))]
}

// spawnSpotLocked busca el mejor sitio para que aparezca la nave de p. Se
// prueban anclas repartidas por el borde de la arena y se elige la que más
// espacio libre tiene (lo más lejos posible de los bloques del campo fijo y de
// los demás jugadores). Así arrancan separados y nunca dentro de una estructura.
// El llamador tiene el lock tomado.
func (g *Sim) spawnSpotLocked(p *Player) (float64, float64) {
	w, h := g.w, g.h
	if w == 0 || h == 0 {
		return 400, 300
	}
	anchors := [][2]float64{
		{w * 0.13, h * 0.13}, {w * 0.50, h * 0.11}, {w * 0.87, h * 0.13},
		{w * 0.13, h * 0.50}, {w * 0.87, h * 0.50},
		{w * 0.13, h * 0.87}, {w * 0.50, h * 0.89}, {w * 0.87, h * 0.87},
	}
	const minClear = 45.0 // radio de la nave + margen para no salir pegada
	bestX, bestY, bestScore := w/2, h/2, -1.0
	consider := func(x, y float64) {
		if c := g.clearanceLocked(x, y, p); c > bestScore {
			bestScore, bestX, bestY = c, x, y
		}
	}
	for _, a := range anchors {
		consider(a[0], a[1])
	}
	// Si ninguna ancla está despejada (arena pequeña con muchas estructuras), se
	// busca en toda la arena: mejor perder un poco de separación que aparecer
	// dentro de un bloque.
	if bestScore < minClear {
		for x := 40.0; x <= w-40; x += 24 {
			for y := 40.0; y <= h-40; y += 24 {
				consider(x, y)
			}
		}
	}
	return bestX, bestY
}

// normAngle deja un ángulo en (-pi, pi].
func normAngle(a float64) float64 {
	for a > math.Pi {
		a -= 2 * math.Pi
	}
	for a <= -math.Pi {
		a += 2 * math.Pi
	}
	return a
}

// spawnFreePowerupLocked suelta un power-up en un sitio libre (que no esté dentro
// de una estructura). El llamador tiene el lock tomado.
func (g *Sim) spawnFreePowerupLocked() {
	if g.w == 0 || g.h == 0 {
		return
	}
	for tries := 0; tries < 20; tries++ {
		x := 40 + rand.Float64()*(g.w-80)
		y := 40 + rand.Float64()*(g.h-80)
		ok := true
		for i := range g.rocks {
			if g.rocks[i].fixed && math.Hypot(x-g.rocks[i].x, y-g.rocks[i].y) < g.rocks[i].r+20 {
				ok = false
				break
			}
		}
		if ok {
			g.spawnPowerup(x, y, g.randomPowerupKind())
			return
		}
	}
}

// playerByIDLocked busca un jugador por ID sin tomar el lock (para usarlo desde
// dentro de Step). Devuelve nil si no existe.
func (g *Sim) playerByIDLocked(id int) *Player {
	for _, p := range g.players {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// KillPlayer revienta la nave de un jugador (como si le dieran). Lo usa el
// servidor para pruebas y para expulsar a alguien si hiciera falta.
func (g *Sim) KillPlayer(p *Player) {
	if p == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.killShip(p)
}

// clearanceLocked es cuánto espacio libre hay en (x,y): la distancia al bloque
// fijo más cercano y al jugador vivo más cercano (sin contar a p).
func (g *Sim) clearanceLocked(x, y float64, p *Player) float64 {
	best := math.MaxFloat64
	for i := range g.rocks {
		r := &g.rocks[i]
		if !r.fixed {
			continue
		}
		if d := math.Hypot(x-r.x, y-r.y) - r.r; d < best {
			best = d
		}
	}
	for _, o := range g.players {
		if o == p || !o.ship.alive {
			continue
		}
		if d := math.Hypot(x-o.ship.x, y-o.ship.y); d < best {
			best = d
		}
	}
	for i := range g.mines {
		// se resta el radio de limpieza: el sitio vale si la mina queda FUERA
		if d := math.Hypot(x-g.mines[i].x, y-g.mines[i].y) - mineClearRadius; d < best {
			best = d
		}
	}
	if best == math.MaxFloat64 {
		best = 1000
	}
	return best
}

// clearMinesNearLocked quita las minas que haya a menos de r del punto: se usa
// al reaparecer, para no materializarse encima de una mina y morir al instante.
func (g *Sim) clearMinesNearLocked(x, y, r float64) {
	live := g.mines[:0]
	for _, m := range g.mines {
		if math.Hypot(m.x-x, m.y-y) < r {
			continue
		}
		live = append(live, m)
	}
	g.mines = live
}

// spawnPowerupIfUseful suelta un power-up solo si le sirve a alguien: si no,
// quedaría tirado en la arena sin poder cogerse (pasaba con los asistentes
// cuando todos los jugadores ya llevaban los suyos).
func (g *Sim) spawnPowerupIfUseful(x, y float64, kind int) {
	for _, p := range g.players {
		if g.powerupUseful(p, kind) {
			g.spawnPowerup(x, y, kind)
			return
		}
	}
}

// ---------- pose de puntería (multijugador) ----------

// aimPt es la pose desde la que dispara un jugador según SU cliente.
type aimPt struct{ x, y, a float64 }

// SetAimPose guarda la pose que manda el cliente de un jugador: así las balas
// salen de la nave que él ve (que va un poco por delante de la del servidor).
// Es cosa del servidor: no se serializa ni viaja en los snapshots.
func (g *Sim) SetAimPose(p *Player, x, y, a float64) {
	if p == nil {
		return
	}
	g.mu.Lock()
	if g.aimPose == nil {
		g.aimPose = map[int]aimPt{}
	}
	g.aimPose[p.ID] = aimPt{x: x, y: y, a: a}
	g.mu.Unlock()
}

// aimFor devuelve la pose de disparo del jugador: la que manda su cliente, pero
// recortada para que no se aleje más de maxAimLead de la nave del servidor (así
// ni se puede hacer trampa ni, si el cliente se desmadra en un tirón de red, el
// disparo se va lejos de la nave: se acerca como mucho y sigue apuntando).
func (g *Sim) aimFor(p *Player) (float64, float64, float64) {
	const maxAimLead = 120.0
	// El cliente manda su pose, y entre que la manda y el servidor procesa el
	// disparo pasa un pico. Se adelanta la pose ese pico para que la bala salga
	// donde la nave está de verdad: si no, con la nave girando se nota el desfase.
	// El pico lo mide el servidor (mitad del RTT de este jugador) y lo deja en
	// p.aimLead.
	lead := p.aimLead
	if lead < 0 {
		lead = 0
	}
	s := p.ship
	pt, ok := g.aimPose[p.ID]
	if !ok {
		return s.x, s.y, s.angle
	}
	px, py, pa := pt.x, pt.y, pt.a
	// giro: según la tecla que lleve (el cliente predice igual)
	turn := 0.0
	if p.keys[KeyA] || p.keys[KeyLeft] {
		turn -= 1
	}
	if p.keys[KeyD] || p.keys[KeyRight] {
		turn += 1
	}
	pa += turn * ShipTurnRate * lead
	// avance: la nave sigue moviéndose en ese pico
	px += p.ship.vx * lead
	py += p.ship.vy * lead

	dx, dy := px-s.x, py-s.y
	d := math.Hypot(dx, dy)
	if d > maxAimLead {
		k := maxAimLead / d
		return s.x + dx*k, s.y + dy*k, pa
	}
	return px, py, pa
}

// SetKey pone o quita una tecla de un jugador sin efectos inmediatos (solo el
// estado). Lo usa el servidor para pilotar los bots.
func (g *Sim) SetKey(p *Player, name string, down bool) {
	if p == nil {
		return
	}
	g.mu.Lock()
	if down {
		p.keys[name] = true
	} else {
		delete(p.keys, name)
	}
	g.mu.Unlock()
}

// shipShape devuelve la silueta de la nave según el jugador: además del color,
// cada uno tiene una forma distinta (triángulo clásico, flecha, diamante y ala).
func shipShape(id int, x, y, angle float64) [][2]float64 {
	type pt struct{ fx, fy float64 }
	var tpl []pt
	switch ((id % 4) + 4) % 4 {
	case 1: // flecha: morro largo y cola abierta en V
		tpl = []pt{{18, 0}, {2, 7}, {-9, 8}, {-5, 0}, {-9, -8}, {2, -7}}
	case 2: // cometa: morro afilado y cola ancha
		tpl = []pt{{17, 0}, {1, 4}, {-9, 11}, {-4, 0}, {-9, -11}, {1, -4}}
	case 3: // ala: morro fino y alas barridas hacia atrás
		tpl = []pt{{16, 0}, {0, 5}, {-9, 12}, {-4, 0}, {-9, -12}, {0, -5}}
	default: // clásica: morro largo y popa plana con muesca (se ve hacia dónde va)
		tpl = []pt{{18, 0}, {3, 6}, {-8, 11}, {-4, 0}, {-8, -11}, {3, -6}}
	}
	out := make([][2]float64, len(tpl))
	ca, sa := math.Cos(angle), math.Sin(angle)
	for i, q := range tpl {
		out[i] = [2]float64{
			x + q.fx*ca - q.fy*sa,
			y + q.fx*sa + q.fy*ca,
		}
	}
	return out
}

// ---------- adelanto de las balas propias (solo dibujo) ----------

// SetOwnBulletLead guarda cuánto va por delante la nave predicha del cliente
// respecto a la copia del servidor. Las balas de ese jugador se dibujan con ese
// mismo adelanto: así parecen salir de la nave que él ve, aunque la bala la haya
// creado el servidor un poco antes. Es solo presentación: el daño no cambia.
func (g *Sim) SetOwnBulletLead(id int, dx, dy float64) {
	g.mu.Lock()
	if g.bulletLead == nil {
		g.bulletLead = map[int][2]float64{}
	}
	g.bulletLead[id] = [2]float64{dx, dy}
	g.mu.Unlock()
}

// bulletLeadOf devuelve el adelanto de dibujo de las balas de un jugador.
func (g *Sim) bulletLeadOf(id int) (float64, float64) {
	if g.bulletLead == nil {
		return 0, 0
	}
	l, ok := g.bulletLead[id]
	if !ok {
		return 0, 0
	}
	return l[0], l[1]
}

// rotateAround gira el punto (px,py) alrededor de (cx,cy) un ángulo dado.
func rotateAround(cx, cy, px, py, ang float64) (float64, float64) {
	ca, sa := math.Cos(ang), math.Sin(ang)
	dx, dy := px-cx, py-cy
	return cx + dx*ca - dy*sa, cy + dx*sa + dy*ca
}

// drawShipWreck dibuja la nave hecha trozos: cada trozo (un triángulo de la
// silueta) sale volando, gira sobre sí mismo y se apaga. Todo se deriva del
// tiempo que lleva muerta, así que no hace falta guardar nada más.
func drawShipWreck(d Drawer, pr *playerRender, cr, cg, cb uint8) {
	f := 1 - pr.deathT/shipDeathAnim
	if f < 0 {
		f = 0
	} else if f > 1 {
		f = 1
	}
	el := f * shipDeathAnim
	alpha := uint8(240 * (1 - f))
	cx, cy := pr.deathX, pr.deathY
	sh := shipShape(pr.id, cx, cy, 0)
	n := len(sh)

	// chispas del impacto
	d.SetColor(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: uint8(float64(alpha) * 0.7)})
	for k := 0; k < 6; k++ {
		a := float64(k) * math.Pi / 3
		rr := 5 + 26*f
		d.DrawCircle(cx+math.Cos(a)*rr, cy+math.Sin(a)*rr, 1.6*(1-f)+0.4)
		d.Fill()
	}

	for i := 0; i < n; i++ {
		a := sh[i]
		b := sh[(i+1)%n]
		// centro del trozo (nave, vértice y siguiente)
		fx := (a[0] + b[0] + cx) / 3
		fy := (a[1] + b[1] + cy) / 3
		dirx, diry := fx-cx, fy-cy
		if l := math.Hypot(dirx, diry); l > 0.01 {
			dirx, diry = dirx/l, diry/l
		}
		sp := 50 + float64(i)*14
		ox, oy := dirx*sp*el, diry*sp*el
		rot := el * (2.0 + float64(i)*0.5)
		px, py := fx+ox, fy+oy
		x1, y1 := rotateAround(px, py, a[0]+ox, a[1]+oy, rot)
		x2, y2 := rotateAround(px, py, b[0]+ox, b[1]+oy, rot)
		x3, y3 := rotateAround(px, py, cx+ox, cy+oy, rot)
		d.SetColor(color.NRGBA{R: cr, G: cg, B: cb, A: alpha})
		d.MoveTo(x1, y1)
		d.LineTo(x2, y2)
		d.LineTo(x3, y3)
		d.ClosePath()
		d.Fill()
	}
}

// drawTeleport dibuja el efecto de teletransporte (tipo Star Trek) en el sitio
// donde se va a recomponer la nave: unos haces verticales que van tomando cuerpo
// y un núcleo que se enciende justo antes de aparecer.
func drawTeleport(d Drawer, pr *playerRender, cr, cg, cb uint8) {
	done := 1 - pr.respawnT/respawnDelay // 0 (acaba de morir) → 1 (a punto de salir)
	if done < 0 {
		done = 0
	} else if done > 1 {
		done = 1
	}
	alpha := uint8(60 + 190*done)
	x, y := pr.spawnX, pr.spawnY
	// Todo el efecto va teñido con el COLOR DEL JUGADOR que va a aparecer (antes
	// el destello y el núcleo eran blancos y el efecto parecía no tener dueño).
	// Se aclara hacia el blanco para que brille pero se siga reconociendo el color.
	light := func(c uint8) uint8 { return uint8((int(c) + 255) / 2) }
	// destello inicial: nace unas 4 veces más grande que el efecto y se encoge
	if done < 0.75 {
		f := 1 - done/0.75
		sz := 18 + 58*f
		a := uint8(230 * f)
		d.SetColor(color.NRGBA{R: light(cr), G: light(cg), B: light(cb), A: a})
		d.SetLineWidth(1 + 3*f)
		d.DrawCircle(x, y, sz)
		d.Stroke()
		d.MoveTo(x-sz, y)
		d.LineTo(x+sz, y)
		d.Stroke()
		d.MoveTo(x, y-sz)
		d.LineTo(x, y+sz)
		d.Stroke()
		d.SetColor(color.NRGBA{R: cr, G: cg, B: cb, A: a})
		d.DrawCircle(x, y, sz*0.7)
		d.Stroke()
	}
	// círculo tenue: marca el sitio aunque los haces parpadeen
	d.SetColor(color.NRGBA{R: cr, G: cg, B: cb, A: uint8(30 + 90*done)})
	d.SetLineWidth(1.6)
	d.DrawCircle(x, y, 19)
	d.Stroke()
	for i := -3; i <= 3; i++ {
		off := float64(i) * 7.5
		wob := 0.5 + 0.5*math.Sin(float64(i)*1.7+done*24)
		h := (12 + 30*wob) * (0.35 + 0.65*done)
		d.SetColor(color.NRGBA{R: cr, G: cg, B: cb, A: alpha})
		d.SetLineWidth(3)
		d.MoveTo(x+off, y+h)
		d.LineTo(x+off, y-h)
		d.Stroke()
	}
	// núcleo brillante que se enciende al final (del color del jugador, aclarado)
	d.SetColor(color.NRGBA{R: light(cr), G: light(cg), B: light(cb), A: uint8(float64(alpha) * done)})
	d.SetLineWidth(4)
	d.MoveTo(x, y+26)
	d.LineTo(x, y-26)
	d.Stroke()
}

// ---------- escudo reflectante ----------

// reflectRadius es el radio del escudo manual (y del reflectante).
const reflectRadius = 22.0

// reflectiveShield indica si el escudo manual de p está rebotando disparos: hay
// que llevar escudo de power-up (permShield) y tener el escudo manual activo.
func (g *Sim) reflectiveShield(p *Player) bool {
	return p != nil && p.ship.alive && p.shieldT > 0 && p.permShield > 0
}

// reflectShotsLocked devuelve los disparos que entran en un escudo reflectante:
// las balas de otros jugadores vuelven por donde vinieron y los disparos enemigos
// se convierten en balas nuestras que vuelven al enemigo. El llamador tiene el
// lock tomado.
func (g *Sim) reflectShotsLocked() {
	for _, p := range g.players {
		if !g.reflectiveShield(p) {
			continue
		}
		s := &p.ship
		bounce := func(dst *astBullet, src astBullet) {
			// se saca justo fuera del escudo para que no rebote en bucle
			dx, dy := src.x-s.x, src.y-s.y
			d := math.Hypot(dx, dy)
			if d < 0.01 {
				dx, dy, d = 1, 0, 1
			}
			*dst = astBullet{
				x:     s.x + dx/d*(reflectRadius+3),
				y:     s.y + dy/d*(reflectRadius+3),
				vx:    -src.vx,
				vy:    -src.vy,
				ttl:   math.Max(src.ttl, 0.8),
				laser: src.laser,
				owner: p.ID,
			}
		}
		// balas de otros jugadores
		for i := range g.bullets {
			b := &g.bullets[i]
			if b.owner == p.ID || b.pink {
				continue
			}
			if math.Hypot(b.x-s.x, b.y-s.y) < reflectRadius {
				old := *b
				bounce(b, old)
				g.playSound("reflect", 80*time.Millisecond)
			}
		}
		// disparos enemigos: vuelven como balas nuestras
		live := g.enemyShots[:0]
		for _, e := range g.enemyShots {
			if math.Hypot(e.x-s.x, e.y-s.y) < reflectRadius {
				var nb astBullet
				bounce(&nb, astBullet{x: e.x, y: e.y, vx: e.vx, vy: e.vy, ttl: 1.2})
				g.bullets = append(g.bullets, nb)
				g.playSound("reflect", 80*time.Millisecond)
				continue
			}
			live = append(live, e)
		}
		g.enemyShots = live
	}
}

// reflectRailLocked devuelve el haz del railgun al que lo disparó: sale del
// escudo reflectante, con el triple de grosor, y le pega de lleno. El llamador
// tiene el lock tomado.
func (g *Sim) reflectRailLocked(from, shooter *Player) {
	if from == nil || shooter == nil {
		return
	}
	ang := math.Atan2(shooter.ship.y-from.ship.y, shooter.ship.x-from.ship.x)
	w := 3 * railReflectBase
	g.bossBeams = append(g.bossBeams, astBossBeam{
		x: from.ship.x, y: from.ship.y, angle: ang,
		base: w, tip: w * 1.4, ttl: 0.35, reflect: true,
	})
	g.playSound("reflectRail", 0)
	// el triple de daño: se lo come el escudo del que disparó (o muere)
	g.damagePlayer(shooter, shieldEnemyHitCost*3, from)
}

// missileStockLocked es la munición de misiles: en PvP solo uno (se recarga solo)
// y en cooperativo los de siempre. El llamador tiene el lock tomado.
func (g *Sim) missileStockLocked() int {
	// Igual en los dos modos: se empieza con un misil y no se recarga.
	return missileStock
}

// missileSpeedLocked es la velocidad de los misiles del modo actual: en PvP van
// más lentos (da tiempo a esquivarlos y a reventarlos a tiros).
func (g *Sim) missileSpeedLocked() float64 {
	if g.pvp {
		return missileSpeedPvP
	}
	return missileSpeed
}

// SetLocalID dice qué jugador maneja este cliente (para el HUD). En el servidor
// no se usa: allí el HUD no importa.
func (g *Sim) SetLocalID(id int) {
	g.mu.Lock()
	g.localID = id
	g.mu.Unlock()
}

// SetAimLead fija cuánto se adelanta la pose de un jugador al disparar. Lo llama
// el servidor con la mitad del RTT medido: así el adelanto se ajusta a la red de
// cada jugador (en localhost el retardo casi no existe y adelantar de más
// desalineaba el disparo).
func (g *Sim) SetAimLead(p *Player, secs float64) {
	if p == nil {
		return
	}
	if secs < 0 {
		secs = 0
	}
	if secs > 0.2 {
		secs = 0.2
	}
	g.mu.Lock()
	p.aimLead = secs
	g.mu.Unlock()
}

// nearestEnemyFor busca a qué apunta un misil: jugadores vivos (en PvP), naves
// enemigas, el jefe y los asteroides que se mueven. NO va a por las estructuras
// fijas del campo fijo (esas no son un objetivo).
func (g *Sim) nearestEnemyFor(x, y float64, owner int) (float64, float64, bool) {
	best := math.Inf(1)
	var bx, by float64
	ok := false
	consider := func(px, py float64) {
		if d := math.Hypot(px-x, py-y); d < best {
			best, bx, by, ok = d, px, py, true
		}
	}
	if g.pvp {
		// En 'vs' el misil va SIEMPRE a por las naves: nunca a asteroides ni a
		// las estructuras del campo fijo.
		for _, o := range g.players {
			if o.ID == owner || !o.ship.alive {
				continue
			}
			consider(o.ship.x, o.ship.y)
		}
		return bx, by, ok
	}
	// cooperativo: naves enemigas, asteroides (las estructuras fijas no) y jefe
	for i := range g.enemies {
		consider(g.enemies[i].x, g.enemies[i].y)
	}
	for i := range g.rocks {
		if g.rocks[i].fixed {
			continue // las estructuras fijas no se persiguen
		}
		consider(g.rocks[i].x, g.rocks[i].y)
	}
	if g.boss != nil {
		consider(g.boss.x, g.boss.y)
	}
	return bx, by, ok
}
