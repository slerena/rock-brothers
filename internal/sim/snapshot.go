package sim

import (
	"bytes"
	"encoding/gob"
	"fmt"
)

// =====================================================================
//  Serialización del estado para el multijugador.
//
//  El servidor es la autoridad: avanza la simulación y manda a cada cliente un
//  snapshot del mundo entero (todas las entidades y todos los jugadores). El
//  cliente lo aplica con Load y pinta el frame.
//
//  Formato elegido: encoding/gob sobre structs espejo de campos EXPORTADOS.
//
//    - Sin dependencias nuevas (gob va en la librería estándar).
//    - Los campos de las entidades son privados, así que no se pueden volcar
//      directamente; un struct espejo con los mismos campos evita el error de
//      "olvidarse" de un campo a mano (queda a la vista en la conversión).
//    - gob conserva la diferencia entre nil y slice vacío, y las conversiones
//      de aquí abajo son funciones puras del contenido, así que la ida y vuelta
//      es byte a byte estable.
//
//  Cabecera: el primer byte es snapshotMagic (versión del formato). Sirve para
//  rechazar basura antes de intentar decodificar. Si algún día cambia el
//  formato, se cambia este byte.
//
//  NO se serializa nada local (no forma parte del frame compartido):
//    - Sim: muted, Best, BestLevel, OnSound, OnBest, cheatBuf, cheatLastAt, mu.
//    - Player: keys (el input es local al cliente).
//  Load tampoco los toca: al aplicar, conserva las teclas que ya tuviera un
//  jugador con el mismo ID, para no romper el input local.
// =====================================================================

// snapshotMagic identifica el formato del snapshot (versión 1).
const snapshotMagic byte = 0xA5

// ---------- structs espejo (campos exportados = mismos que las entidades) ----------

type gobTrailPt struct{ X, Y float64 }

type gobShip struct {
	X, Y, Vx, Vy, Angle float64
	Alive               bool
	Invuln              float64
	TeleCooldown        float64
	FireTimer           float64
}

type gobRock struct {
	X, Y, Vx, Vy, R float64
	Angle, Spin     float64
	Shape           []float64
	Size            int
	Hp, HpMax       int
	Fixed           bool
	Powerup         int
}

type gobBullet struct {
	X, Y, Vx, Vy, Ttl float64
	Laser, Pink       bool
	Owner             int
}

type gobCompanion struct {
	X, Y, Vx, Vy, Angle float64
	Hp                  int
	Invuln              float64
	FireCd              float64
	Think               float64
	Tx, Ty              float64
	HasTarget           bool
	Chasing             bool
}

type gobParticle struct{ X, Y, Vx, Vy, Ttl float64 }

type gobMissile struct {
	X, Y, Angle, Ttl float64
	Trail            []gobTrailPt
}

type gobMine struct{ X, Y, R, Arm, Beep, Life float64 }

type gobEnemy struct {
	X, Y, Vx, Vy, Angle float64
	ShootCd, Ttl        float64
	R                   float64
	Shape               int
	Hp, HpMax           int
}

type gobEnemyBullet struct {
	X, Y, Vx, Vy, Ttl float64
	Homing            bool
	Trail             []gobTrailPt
}

type gobBoss struct {
	X, Y, Vx, Vy, Angle float64
	Energy, MaxEnergy   float64
	MissileCd           float64
	DeployCd, HitFlash  float64
	RailCd, RailWarn    float64
	RailAngle           float64
	MissilesLeft        int
	DeployLeft          int
	R                   float64
}

type gobBossBeam struct {
	X, Y, Angle, Base, Tip, Ttl float64
	Reflect                     bool
}

type gobHyperStar struct{ Ang, Rad, Spd float64 }

type gobPowerup struct {
	X, Y, Vx, Vy, Ttl, Spin float64
	Kind                    int
}

type gobOrb struct {
	Ang, Dir, ShootCd float64
	ShotID            int
	Hp, HpMax         int
}

type gobOrbShot struct {
	X, Y, Vx, Vy, Ttl float64
	ID                int
}

type gobChain struct {
	X1, Y1, X2, Y2, Ttl float64
	Level               int
}

type gobHole struct{ X, Y, R, Age float64 }

type gobPlayer struct {
	ID   int
	Name string
	Ship gobShip

	// animación de muerte y reaparición
	DeathT   float64
	DeathX   float64
	DeathY   float64
	RespawnT float64
	SpawnX   float64
	SpawnY   float64

	Lives         int
	Score         int
	Kills         int
	Deaths        int
	PermShield    float64
	Hull          int
	RapidFire     int
	Laser         bool
	DoubleShot    bool
	ShieldT       float64
	ShieldCd      float64
	MissilesLeft  int
	MinesLeft     int
	MissileReload float64
	LethalCd      float64

	RailCharge        float64
	RailRechargeTotal float64
	RailCharging      bool
	RailHold          float64
	RailAngle         float64
	RailFlash         float64
	RailFlashBase     float64
	RailFlashHalf     float64
	ChargeBeep        float64

	Hyper      float64
	HyperStars []gobHyperStar
	Companion  *gobCompanion
	Orbs       []gobOrb
	OrbShots   []gobOrbShot
}

// gobState es el mundo entero. Nombres calcados a Sim; aquí solo van los campos
// que forman parte del frame.
type gobState struct {
	W, H float64

	// Récord: en multijugador lo lleva el servidor (que lo persiste), así que
	// viaja en el snapshot para que el HUD lo muestre.
	Best      int
	BestName  string
	BestLevel int

	Players []*gobPlayer

	Rocks      []gobRock
	Bullets    []gobBullet
	Parts      []gobParticle
	Missiles   []gobMissile
	Mines      []gobMine
	Enemies    []gobEnemy
	EnemyShots []gobEnemyBullet
	Chains     []gobChain
	BossBeams  []gobBossBeam
	Boss       *gobBoss
	BossCount  int
	FadeIn     float64
	LevelIntro float64
	Powerups   []gobPowerup
	NextShotID int

	GameTime    float64
	NextEnemyAt float64
	NextPowerup float64

	TimeLeft     float64
	TimeWarned   bool
	RockRushMult float64
	Hole         *gobHole

	EnemiesKilled int
	Level         int
	Over          bool
	Paused        bool
	PvP           bool
	Arena         string
	Started       bool
	Flash         float64
}

// ---------- conversión de slices/puteros ----------

// convSlice convierte una slice elemento a elemento preservando nil.
func convSlice[A, B any](in []A, f func(A) B) []B {
	if in == nil {
		return nil
	}
	out := make([]B, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

func copyFloats(in []float64) []float64 {
	if in == nil {
		return nil
	}
	out := make([]float64, len(in))
	copy(out, in)
	return out
}

// ---------- entidades: a gob ----------

func toGobTrailPt(t astTrailPt) gobTrailPt { return gobTrailPt{X: t.x, Y: t.y} }

func toGobShip(s astShip) gobShip {
	return gobShip{
		X: s.x, Y: s.y, Vx: s.vx, Vy: s.vy, Angle: s.angle,
		Alive: s.alive, Invuln: s.invuln,
		TeleCooldown: s.teleCooldown, FireTimer: s.fireTimer,
	}
}

func toGobRock(r astRock) gobRock {
	return gobRock{
		X: r.x, Y: r.y, Vx: r.vx, Vy: r.vy, R: r.r,
		Angle: r.angle, Spin: r.spin, Shape: copyFloats(r.shape),
		Size: r.size, Hp: r.hp, HpMax: r.hpMax,
		Fixed: r.fixed, Powerup: r.powerup,
	}
}

func toGobBullet(b astBullet) gobBullet {
	return gobBullet{X: b.x, Y: b.y, Vx: b.vx, Vy: b.vy, Ttl: b.ttl, Laser: b.laser, Pink: b.pink, Owner: b.owner}
}

func toGobCompanion(c astCompanion) gobCompanion {
	return gobCompanion{
		X: c.x, Y: c.y, Vx: c.vx, Vy: c.vy, Angle: c.angle,
		Hp: c.hp, Invuln: c.invuln, FireCd: c.fireCd, Think: c.think,
		Tx: c.tx, Ty: c.ty, HasTarget: c.hasTarget, Chasing: c.chasing,
	}
}

func toGobParticle(p astParticle) gobParticle {
	return gobParticle{X: p.x, Y: p.y, Vx: p.vx, Vy: p.vy, Ttl: p.ttl}
}

func toGobMissile(m astMissile) gobMissile {
	return gobMissile{
		X: m.x, Y: m.y, Angle: m.angle, Ttl: m.ttl,
		Trail: convSlice(m.trail, toGobTrailPt),
	}
}

func toGobMine(m astMine) gobMine {
	return gobMine{X: m.x, Y: m.y, R: m.r, Arm: m.arm, Beep: m.beep, Life: m.life}
}

func toGobEnemy(e astEnemy) gobEnemy {
	return gobEnemy{
		X: e.x, Y: e.y, Vx: e.vx, Vy: e.vy, Angle: e.angle,
		ShootCd: e.shootCd, Ttl: e.ttl, R: e.r, Shape: e.shape,
		Hp: e.hp, HpMax: e.hpMax,
	}
}

func toGobEnemyBullet(s astEnemyBullet) gobEnemyBullet {
	return gobEnemyBullet{
		X: s.x, Y: s.y, Vx: s.vx, Vy: s.vy, Ttl: s.ttl,
		Homing: s.homing, Trail: convSlice(s.trail, toGobTrailPt),
	}
}

func toGobBoss(b astBoss) gobBoss {
	return gobBoss{
		X: b.x, Y: b.y, Vx: b.vx, Vy: b.vy, Angle: b.angle,
		Energy: b.energy, MaxEnergy: b.maxEnergy, MissileCd: b.missileCd,
		DeployCd: b.deployCd, HitFlash: b.hitFlash, RailCd: b.railCd,
		RailWarn: b.railWarn, RailAngle: b.railAngle,
		MissilesLeft: b.missilesLeft, DeployLeft: b.deployLeft, R: b.r,
	}
}

func toGobBossBeam(b astBossBeam) gobBossBeam {
	return gobBossBeam{X: b.x, Y: b.y, Angle: b.angle, Base: b.base, Tip: b.tip, Ttl: b.ttl, Reflect: b.reflect}
}

func toGobHyperStar(s hyperStar) gobHyperStar {
	return gobHyperStar{Ang: s.ang, Rad: s.rad, Spd: s.spd}
}

func toGobPowerup(p astPowerup) gobPowerup {
	return gobPowerup{
		X: p.x, Y: p.y, Vx: p.vx, Vy: p.vy, Ttl: p.ttl, Spin: p.spin, Kind: p.kind,
	}
}

func toGobOrb(o astOrb) gobOrb {
	return gobOrb{
		Ang: o.ang, Dir: o.dir, ShootCd: o.shootCd,
		ShotID: o.shotID, Hp: o.hp, HpMax: o.hpMax,
	}
}

func toGobOrbShot(s astOrbShot) gobOrbShot {
	return gobOrbShot{X: s.x, Y: s.y, Vx: s.vx, Vy: s.vy, Ttl: s.ttl, ID: s.id}
}

func toGobChain(c astChain) gobChain {
	return gobChain{X1: c.x1, Y1: c.y1, X2: c.x2, Y2: c.y2, Ttl: c.ttl, Level: c.level}
}

func toGobHole(h astHole) gobHole {
	return gobHole{X: h.x, Y: h.y, R: h.r, Age: h.age}
}

func toGobPlayer(p *Player) *gobPlayer {
	if p == nil {
		return nil
	}
	gp := &gobPlayer{
		ID: p.ID, Name: p.Name, Ship: toGobShip(p.ship),

		DeathT: p.deathT, DeathX: p.deathX, DeathY: p.deathY,
		RespawnT: p.respawnT, SpawnX: p.spawnX, SpawnY: p.spawnY,

		Lives: p.lives, Score: p.score, Kills: p.kills, Deaths: p.deaths,
		PermShield: p.permShield, Hull: p.hull,
		RapidFire: p.rapidFire, Laser: p.laser,
		DoubleShot: p.doubleShot,
		ShieldT:    p.shieldT, ShieldCd: p.shieldCd,
		MissilesLeft: p.missilesLeft, MinesLeft: p.minesLeft,
		MissileReload: p.missileReload, LethalCd: p.lethalCd,

		RailCharge: p.railCharge, RailRechargeTotal: p.railRechargeTotal,
		RailCharging: p.railCharging, RailHold: p.railHold,
		RailAngle: p.railAngle, RailFlash: p.railFlash,
		RailFlashBase: p.railFlashBase, RailFlashHalf: p.railFlashHalf,
		ChargeBeep: p.chargeBeep,

		Hyper:      p.hyper,
		HyperStars: convSlice(p.hyperStars, toGobHyperStar),
		Orbs:       convSlice(p.orbs, toGobOrb),
		OrbShots:   convSlice(p.orbShots, toGobOrbShot),
	}
	if p.companion != nil {
		c := toGobCompanion(*p.companion)
		gp.Companion = &c
	}
	return gp
}

// ---------- entidades: desde gob ----------

func fromGobTrailPt(t gobTrailPt) astTrailPt { return astTrailPt{x: t.X, y: t.Y} }

func fromGobShip(s gobShip) astShip {
	return astShip{
		x: s.X, y: s.Y, vx: s.Vx, vy: s.Vy, angle: s.Angle,
		alive: s.Alive, invuln: s.Invuln,
		teleCooldown: s.TeleCooldown, fireTimer: s.FireTimer,
	}
}

func fromGobRock(r gobRock) astRock {
	return astRock{
		x: r.X, y: r.Y, vx: r.Vx, vy: r.Vy, r: r.R,
		angle: r.Angle, spin: r.Spin, shape: copyFloats(r.Shape),
		size: r.Size, hp: r.Hp, hpMax: r.HpMax,
		fixed: r.Fixed, powerup: r.Powerup,
	}
}

func fromGobBullet(b gobBullet) astBullet {
	return astBullet{x: b.X, y: b.Y, vx: b.Vx, vy: b.Vy, ttl: b.Ttl, laser: b.Laser, pink: b.Pink, owner: b.Owner}
}

func fromGobCompanion(c gobCompanion) astCompanion {
	return astCompanion{
		x: c.X, y: c.Y, vx: c.Vx, vy: c.Vy, angle: c.Angle,
		hp: c.Hp, invuln: c.Invuln, fireCd: c.FireCd, think: c.Think,
		tx: c.Tx, ty: c.Ty, hasTarget: c.HasTarget, chasing: c.Chasing,
	}
}

func fromGobParticle(p gobParticle) astParticle {
	return astParticle{x: p.X, y: p.Y, vx: p.Vx, vy: p.Vy, ttl: p.Ttl}
}

func fromGobMissile(m gobMissile) astMissile {
	return astMissile{
		x: m.X, y: m.Y, angle: m.Angle, ttl: m.Ttl,
		trail: convSlice(m.Trail, fromGobTrailPt),
	}
}

func fromGobMine(m gobMine) astMine {
	return astMine{x: m.X, y: m.Y, r: m.R, arm: m.Arm, beep: m.Beep, life: m.Life}
}

func fromGobEnemy(e gobEnemy) astEnemy {
	return astEnemy{
		x: e.X, y: e.Y, vx: e.Vx, vy: e.Vy, angle: e.Angle,
		shootCd: e.ShootCd, ttl: e.Ttl, r: e.R, shape: e.Shape,
		hp: e.Hp, hpMax: e.HpMax,
	}
}

func fromGobEnemyBullet(s gobEnemyBullet) astEnemyBullet {
	return astEnemyBullet{
		x: s.X, y: s.Y, vx: s.Vx, vy: s.Vy, ttl: s.Ttl,
		homing: s.Homing, trail: convSlice(s.Trail, fromGobTrailPt),
	}
}

func fromGobBoss(b gobBoss) astBoss {
	return astBoss{
		x: b.X, y: b.Y, vx: b.Vx, vy: b.Vy, angle: b.Angle,
		energy: b.Energy, maxEnergy: b.MaxEnergy, missileCd: b.MissileCd,
		deployCd: b.DeployCd, hitFlash: b.HitFlash, railCd: b.RailCd,
		railWarn: b.RailWarn, railAngle: b.RailAngle,
		missilesLeft: b.MissilesLeft, deployLeft: b.DeployLeft, r: b.R,
	}
}

func fromGobBossBeam(b gobBossBeam) astBossBeam {
	return astBossBeam{x: b.X, y: b.Y, angle: b.Angle, base: b.Base, tip: b.Tip, ttl: b.Ttl, reflect: b.Reflect}
}

func fromGobHyperStar(s gobHyperStar) hyperStar {
	return hyperStar{ang: s.Ang, rad: s.Rad, spd: s.Spd}
}

func fromGobPowerup(p gobPowerup) astPowerup {
	return astPowerup{
		x: p.X, y: p.Y, vx: p.Vx, vy: p.Vy, ttl: p.Ttl, spin: p.Spin, kind: p.Kind,
	}
}

func fromGobOrb(o gobOrb) astOrb {
	return astOrb{
		ang: o.Ang, dir: o.Dir, shootCd: o.ShootCd,
		shotID: o.ShotID, hp: o.Hp, hpMax: o.HpMax,
	}
}

func fromGobOrbShot(s gobOrbShot) astOrbShot {
	return astOrbShot{x: s.X, y: s.Y, vx: s.Vx, vy: s.Vy, ttl: s.Ttl, id: s.ID}
}

func fromGobChain(c gobChain) astChain {
	return astChain{x1: c.X1, y1: c.Y1, x2: c.X2, y2: c.Y2, ttl: c.Ttl, level: c.Level}
}

func fromGobHole(h gobHole) astHole {
	return astHole{x: h.X, y: h.Y, r: h.R, age: h.Age}
}

// fromGobPlayer reconstruye un jugador. keys NO viene en el snapshot: si el
// jugador ya existía (mismo ID) se conserva su mapa local de teclas; si no, se
// crea vacío.
func fromGobPlayer(gp *gobPlayer, keys map[int]map[string]bool) *Player {
	if gp == nil {
		return nil
	}
	p := &Player{
		ID: gp.ID, Name: gp.Name, ship: fromGobShip(gp.Ship),

		deathT: gp.DeathT, deathX: gp.DeathX, deathY: gp.DeathY,
		respawnT: gp.RespawnT, spawnX: gp.SpawnX, spawnY: gp.SpawnY,

		lives: gp.Lives, score: gp.Score, kills: gp.Kills, deaths: gp.Deaths,
		permShield: gp.PermShield, hull: gp.Hull,
		rapidFire: gp.RapidFire, laser: gp.Laser,
		doubleShot: gp.DoubleShot,
		shieldT:    gp.ShieldT, shieldCd: gp.ShieldCd,
		missilesLeft: gp.MissilesLeft, minesLeft: gp.MinesLeft,
		missileReload: gp.MissileReload, lethalCd: gp.LethalCd,

		railCharge: gp.RailCharge, railRechargeTotal: gp.RailRechargeTotal,
		railCharging: gp.RailCharging, railHold: gp.RailHold,
		railAngle: gp.RailAngle, railFlash: gp.RailFlash,
		railFlashBase: gp.RailFlashBase, railFlashHalf: gp.RailFlashHalf,
		chargeBeep: gp.ChargeBeep,

		hyper:      gp.Hyper,
		hyperStars: convSlice(gp.HyperStars, fromGobHyperStar),
		orbs:       convSlice(gp.Orbs, fromGobOrb),
		orbShots:   convSlice(gp.OrbShots, fromGobOrbShot),
	}
	if gp.Companion != nil {
		c := fromGobCompanion(*gp.Companion)
		p.companion = &c
	}
	p.keys = keys[gp.ID]
	if p.keys == nil {
		p.keys = map[string]bool{}
	}
	return p
}

// ---------- estado ----------

// gobStateLocked vuelca el estado bajo el lock (el llamador debe tenerlo).
func (g *Sim) gobStateLocked() gobState {
	st := gobState{
		W: g.w, H: g.h,

		Best: g.Best, BestName: g.BestName, BestLevel: g.BestLevel,

		Rocks:      convSlice(g.rocks, toGobRock),
		Bullets:    convSlice(g.bullets, toGobBullet),
		Parts:      convSlice(g.parts, toGobParticle),
		Missiles:   convSlice(g.missiles, toGobMissile),
		Mines:      convSlice(g.mines, toGobMine),
		Enemies:    convSlice(g.enemies, toGobEnemy),
		EnemyShots: convSlice(g.enemyShots, toGobEnemyBullet),
		Chains:     convSlice(g.chains, toGobChain),
		BossBeams:  convSlice(g.bossBeams, toGobBossBeam),
		BossCount:  g.bossCount,
		FadeIn:     g.fadeIn,
		LevelIntro: g.levelIntro,
		Powerups:   convSlice(g.powerups, toGobPowerup),
		NextShotID: g.nextShotID,

		GameTime:    g.gameTime,
		NextEnemyAt: g.nextEnemyAt,
		NextPowerup: g.nextPowerup,

		TimeLeft:     g.timeLeft,
		TimeWarned:   g.timeWarned,
		RockRushMult: g.rockRushMult,

		EnemiesKilled: g.enemiesKilled,
		Level:         g.level,
		Over:          g.over,
		Paused:        g.paused,
		PvP:           g.pvp,
		Arena:         g.arena,
		Started:       g.started,
		Flash:         g.flash,
	}
	st.Players = make([]*gobPlayer, len(g.players))
	for i, p := range g.players {
		st.Players[i] = toGobPlayer(p)
	}
	if g.boss != nil {
		b := toGobBoss(*g.boss)
		st.Boss = &b
	}
	if g.hole != nil {
		h := toGobHole(*g.hole)
		st.Hole = &h
	}
	return st
}

// applyGobStateLocked reemplaza mundo y jugadores. NO toca la configuración
// local (muted, Best, BestLevel, OnSound, OnBest) ni cheatBuf/cheatLastAt. Las
// teclas de los jugadores con el mismo ID se conservan (ver fromGobPlayer).
func (g *Sim) applyGobStateLocked(st *gobState) {
	keys := make(map[int]map[string]bool, len(g.players))
	for _, p := range g.players {
		keys[p.ID] = p.keys
	}

	g.w, g.h = st.W, st.H
	g.Best, g.BestName, g.BestLevel = st.Best, st.BestName, st.BestLevel

	g.players = make([]*Player, len(st.Players))
	for i, gp := range st.Players {
		g.players[i] = fromGobPlayer(gp, keys)
	}

	g.rocks = convSlice(st.Rocks, fromGobRock)
	g.bullets = convSlice(st.Bullets, fromGobBullet)
	g.parts = convSlice(st.Parts, fromGobParticle)
	g.missiles = convSlice(st.Missiles, fromGobMissile)
	g.mines = convSlice(st.Mines, fromGobMine)
	g.enemies = convSlice(st.Enemies, fromGobEnemy)
	g.enemyShots = convSlice(st.EnemyShots, fromGobEnemyBullet)
	g.chains = convSlice(st.Chains, fromGobChain)
	g.bossBeams = convSlice(st.BossBeams, fromGobBossBeam)
	if st.Boss != nil {
		b := fromGobBoss(*st.Boss)
		g.boss = &b
	} else {
		g.boss = nil
	}
	g.bossCount = st.BossCount
	g.fadeIn = st.FadeIn
	g.levelIntro = st.LevelIntro
	g.powerups = convSlice(st.Powerups, fromGobPowerup)
	g.nextShotID = st.NextShotID

	g.gameTime = st.GameTime
	g.nextEnemyAt = st.NextEnemyAt
	g.nextPowerup = st.NextPowerup

	g.timeLeft = st.TimeLeft
	g.timeWarned = st.TimeWarned
	g.rockRushMult = st.RockRushMult
	if st.Hole != nil {
		h := fromGobHole(*st.Hole)
		g.hole = &h
	} else {
		g.hole = nil
	}

	g.enemiesKilled = st.EnemiesKilled
	g.level = st.Level
	g.over = st.Over
	g.paused = st.Paused
	g.pvp = st.PvP
	g.arena = st.Arena
	g.started = st.Started
	g.flash = st.Flash
}

// Snapshot serializa el estado del juego (mundo + jugadores) para mandarlo por
// la red. Toma el lock él mismo.
func (g *Sim) Snapshot() []byte {
	g.mu.Lock()
	st := g.gobStateLocked()
	g.mu.Unlock()

	var buf bytes.Buffer
	buf.WriteByte(snapshotMagic)
	// Los tipos de gobState son simples (números, strings, slices y structs
	// concretos), así que Encode no puede fallar; si lo hiciera, devolvemos nil.
	if err := gob.NewEncoder(&buf).Encode(&st); err != nil {
		return nil
	}
	return buf.Bytes()
}

// Load aplica un snapshot creado por Snapshot: reemplaza el estado del juego
// (mundo y jugadores) pero NO la configuración local (muted, Best, BestLevel,
// OnSound, OnBest). Toma el lock él mismo. Devuelve error si el formato no
// cuadra.
func (g *Sim) Load(b []byte) error {
	if len(b) == 0 || b[0] != snapshotMagic {
		return fmt.Errorf("sim: snapshot con formato desconocido")
	}
	var st gobState
	if err := gob.NewDecoder(bytes.NewReader(b[1:])).Decode(&st); err != nil {
		return fmt.Errorf("sim: snapshot corrupto: %w", err)
	}
	g.mu.Lock()
	g.applyGobStateLocked(&st)
	g.mu.Unlock()
	return nil
}
