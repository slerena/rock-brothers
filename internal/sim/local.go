package sim

import "math"

// Suavizado del cliente multijugador: el servidor manda snapshots a 60 Hz y el
// cliente pinta a la frecuencia de la pantalla, así que entre dos snapshots hay
// que "seguir moviendo" el mundo (extrapolar) y responder al instante al input
// propio (predecir la nave). El servidor sigue siendo la verdad: cada snapshot
// nuevo reinicia el estado y corrige.

// Extrapolate avanza SOLO las posiciones dt segundos con las velocidades que ya
// hay en el estado. NO simula: no toca colisiones, disparos, IA, cronómetros ni
// marcadores. La nave de `local` (si no es nil) se salta, porque el cliente la
// lleva él con StepLocalShip.
func (g *Sim) Extrapolate(dt float64, local *Player) {
	if dt <= 0 {
		return
	}
	if dt > 0.1 {
		dt = 0.1 // tras un parón largo no exageramos el salto
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	w, h := g.w, g.h
	rush := g.rockRushMult
	wrapAll := w > 0 && h > 0

	for _, p := range g.players {
		if p != local {
			p.ship.x += p.ship.vx * dt
			p.ship.y += p.ship.vy * dt
			if wrapAll {
				wrap(&p.ship.x, &p.ship.y, w, h)
			}
		}
		if c := p.companion; c != nil {
			c.x += c.vx * dt
			c.y += c.vy * dt
			if wrapAll {
				wrap(&c.x, &c.y, w, h)
			}
		}
		for i := range p.orbs { // los asistentes giran alrededor de la nave
			p.orbs[i].ang += p.orbs[i].dir * dt
		}
		for i := range p.orbShots {
			s := &p.orbShots[i]
			s.x += s.vx * dt
			s.y += s.vy * dt
			if wrapAll {
				wrap(&s.x, &s.y, w, h)
			}
		}
	}
	for i := range g.rocks {
		r := &g.rocks[i]
		r.x += r.vx * rush * dt
		r.y += r.vy * rush * dt
		r.angle += r.spin * dt
		if wrapAll {
			wrap(&r.x, &r.y, w, h)
		}
	}
	if len(g.bullets) > 0 {
		live := g.bullets[:0]
		for i := range g.bullets {
			b := g.bullets[i]
			b.x += b.vx * dt
			b.y += b.vy * dt
			b.ttl -= dt
			if wrapAll {
				wrap(&b.x, &b.y, w, h)
			}
			if b.ttl > 0 {
				live = append(live, b)
			}
		}
		g.bullets = live
	}
	for i := range g.parts {
		p := &g.parts[i]
		p.x += p.vx * dt
		p.y += p.vy * dt
	}
	missileSp := g.missileSpeedLocked() // la del modo: no es la misma en coop que en 'vs'
	for i := range g.missiles {         // los misiles van por su ángulo (ver updateMissiles)
		m := &g.missiles[i]
		m.x += math.Cos(m.angle) * missileSp * dt
		m.y += math.Sin(m.angle) * missileSp * dt
		if wrapAll {
			wrap(&m.x, &m.y, w, h)
		}
	}
	for i := range g.enemies {
		e := &g.enemies[i]
		e.x += e.vx * dt
		e.y += e.vy * dt
		if wrapAll {
			wrap(&e.x, &e.y, w, h)
		}
	}
	for i := range g.enemyShots {
		s := &g.enemyShots[i]
		s.x += s.vx * dt
		s.y += s.vy * dt
		if wrapAll {
			wrap(&s.x, &s.y, w, h)
		}
	}
	for i := range g.powerups {
		pu := &g.powerups[i]
		pu.x += pu.vx * dt
		pu.y += pu.vy * dt
	}
	if b := g.boss; b != nil {
		b.x += b.vx * dt
		b.y += b.vy * dt
	}
}

// StepLocalShip avanza SOLO el movimiento lineal de la nave del jugador p
// (empuje, fricción, gravedad del agujero y posición) con el input que ya tenga.
//
// El GIRO también se predice (si no, girar se nota perezoso): para que las balas
// salgan de la nave dibujada y apuntadas, el cliente manda su pose con SetAimPose
// y el servidor dispara desde ahí.
//
// Las BALAS no se predicen: las crea solo el servidor. Predecirlas daba ráfagas
// (el cliente disparaba por su cuenta y luego chocaba con el tope de balas) y
// disparos que no hacían daño. Una pulsación = un disparo del servidor.
func (g *Sim) StepLocalShip(p *Player, dt float64) {
	if p == nil || dt <= 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	s := &p.ship
	if !s.alive || g.w == 0 || g.h == 0 {
		return
	}
	// La invulnerabilidad NO se toca aquí: la lleva el servidor (si la
	// descontáramos también en local, se gastaría al doble).
	// giro (A/D, y las flechas) para que responda al instante
	if p.keys[KeyA] || p.keys[KeyLeft] {
		s.angle -= ShipTurnRate * dt
	}
	if p.keys[KeyD] || p.keys[KeyRight] {
		s.angle += ShipTurnRate * dt
	}
	if p.keys[KeyUp] {
		s.vx += math.Cos(s.angle) * shipThrust * dt
		s.vy += math.Sin(s.angle) * shipThrust * dt
	}
	// El rozamiento del juego es POR PASO de 16 ms. Aquí se avanza con el dt real
	// del frame (para que el movimiento sea continuo, sin tramos), así que se
	// compensa: shipFriction^(dt/16ms). Así la nave va igual que la del servidor
	// sin depender de los Hz de la pantalla.
	fric := math.Pow(shipFriction, dt/0.016)
	s.vx *= fric
	s.vy *= fric
	if sp := math.Hypot(s.vx, s.vy); sp > shipSpeedMax {
		s.vx, s.vy = s.vx/sp*shipSpeedMax, s.vy/sp*shipSpeedMax
	}
	if ax, ay := g.holeAccel(s.x, s.y); ax != 0 || ay != 0 {
		s.vx += ax * dt
		s.vy += ay * dt
	}
	s.x += s.vx * dt
	s.y += s.vy * dt
	wrap(&s.x, &s.y, g.w, g.h)
	// también las colisiones: si no, la nave predicha atravesaría rocas y
	// estructuras, se separaría de la del servidor y la corrección la frenaría
	g.collideShipRocksLocked(p, s)
}

// PlayerByID busca un jugador por su ID (el cliente multijugador lo usa para
// encontrar el suyo: los IDs son estables aunque alguien se vaya).
func (g *Sim) PlayerByID(id int) *Player {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, p := range g.players {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// ShipState devuelve el estado de la nave (para guardarla antes de aplicar un
// snapshot y restaurarla después, si la predicción iba bien).
func (p *Player) ShipState() (x, y, vx, vy, angle, invuln float64, alive bool) {
	return p.ship.x, p.ship.y, p.ship.vx, p.ship.vy, p.ship.angle, p.ship.invuln, p.ship.alive
}

// SetShip restaura el estado de la nave.
func (p *Player) SetShip(x, y, vx, vy, angle, invuln float64, alive bool) {
	p.ship.x, p.ship.y = x, y
	p.ship.vx, p.ship.vy = vx, vy
	p.ship.angle = angle
	p.ship.invuln = invuln
	p.ship.alive = alive
}
