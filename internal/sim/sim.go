package sim

import (
	"fmt"
	"image/color"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// =====================================================================
//  Simulación pura: estado y reglas del juego. NO dibuja, no suena y no
//  depende de ninguna plataforma (ni Fyne, ni gg, ni el navegador).
//
//  La usan tres sitios:
//    - el cliente nativo (internal/game, con Fyne),
//    - el cliente web (internal/game, con el canvas del navegador),
//    - el servidor autoritativo (cmd/server), que solo llama a Step.
//
//  Todo lo que ocurre aquí es determinista salvo el RNG, que en multijugador
//  controla el servidor.
// =====================================================================

// SoundFunc reproduce un sonido (lo inyecta la plataforma). En el servidor es
// nil: la simulación no suena.
type SoundFunc func(name string, gap time.Duration)

// Nombres de tecla. Son EXACTAMENTE los que usa Fyne, para que el cliente
// nativo no tenga que traducir nada y el cliente web mande lo mismo por la red.
const (
	KeyLeft   = "Left"
	KeyRight  = "Right"
	KeyUp     = "Up"
	KeyDown   = "Down"
	KeySpace  = "Space"
	KeyReturn = "Return"
	KeyEnter  = "KP_Enter"
	KeyEscape = "Escape"

	// Controles del juego: WASD para moverse (S = escudo), espacio dispara,
	// M misil, K minas, L rayo letal, G railgun (se carga manteniendo), enter
	// teletransporte. Las flechas siguen valiendo para moverse.
	KeyW    = "W"
	KeyA    = "A"
	KeyS    = "S"
	KeyD    = "D"
	KeyK    = "K"
	KeyG    = "G"
	KeyMute = "X"
)

// =====================================================================
//  Rock Brothers (gráficos fósforo verde, sonido incluido)
//  Flechas: girar/empuje · Espacio: disparar · Enter: teletransporte
//  Flecha abajo: escudo
// =====================================================================

const (
	astRate     = 22050
	astChunk    = 512
	astMaxVoic  = 10
	astGreen    = "#39ff6a"
	astGreenDim = "#0f4d22"
)

// ---------- entidades ----------

type astShip struct {
	x, y, vx, vy, angle float64
	alive               bool
	invuln              float64
	teleCooldown        float64
	fireTimer           float64 // cadencia de disparo (lo reduce el power-up de cadencia)
}

type astRock struct {
	x, y, vx, vy, r float64
	angle, spin     float64
	shape           []float64
	size            int
	hp, hpMax       int  // golpes restantes / dureza original (>1 = roca dura)
	fixed           bool // estructura del campo fijo: no se mueve ni se parte
	powerup         int  // power-up escondido dentro (-1 = ninguno)
}

type astBullet struct {
	x, y, vx, vy, ttl float64
	laser             bool // disparo láser (raya corta y rápida)
	pink              bool // disparo del compañero (no nos afecta)
	owner             int  // quién lo disparó (para el fuego amigo en PvP)
}

// astCompanion: nave aliada azul que suelta un power-up muy raro. Vuela libre,
// dispara a enemigos y rocas, recoge power-ups y puede morir. Es un poco torpe:
// reacciona tarde, gira despacio y dispara con dispersión.
type astCompanion struct {
	x, y, vx, vy, angle float64
	hp                  int
	invuln              float64
	fireCd              float64
	think               float64 // temporizador de decisión (reacciona tarde)
	tx, ty              float64 // objetivo recordado hasta la próxima decisión
	hasTarget           bool
	chasing             bool // va a por un power-up
}

type astParticle struct {
	x, y, vx, vy, ttl float64
}

// astMissile: misil teledirigido con estela curva que se desvanece.
type astMissile struct {
	x, y, angle, ttl float64
	trail            []astTrailPt
	owner            int // quién lo lanzó (para el marcador en PvP)
}

type astTrailPt struct{ x, y float64 }

// astMine: mina que se deja caer; primero pita/fluctúa (armándose) y luego
// explota si algo la roza.
type astMine struct {
	x, y, r float64
	arm     float64 // >0 = armándose (segundos restantes); 0 = activa
	beep    float64
	life    float64
	owner   int // quién la soltó (para el marcador en PvP)
}

// astEnemy: nave enemiga que aparece a los 20 s y dispara al jugador.
type astEnemy struct {
	x, y, vx, vy, angle float64
	shootCd, ttl        float64
	r                   float64
	shape               int // silueta (naves enemigas tipo Star Trek)
	hp, hpMax           int // resistencia (1 = muere de un disparo)
}

type astEnemyBullet struct {
	x, y, vx, vy, ttl float64
	homing            bool // misil que persigue al jugador
	trail             []astTrailPt
}

// astBoss: nave enemiga grande con barra de energía. Aparece en cada nivel,
// despliega naves pequeñas, lanza misiles y destruye asteroides que se acercan.
type astBoss struct {
	x, y, vx, vy, angle float64
	energy, maxEnergy   float64
	missileCd           float64
	deployCd, hitFlash  float64
	railCd, railWarn    float64
	railAngle           float64
	missilesLeft        int
	deployLeft          int
	r                   float64
}

// astBossBeam: haz del railgun de la nodriza (blanco/rojo) ya disparado.
type astBossBeam struct {
	x, y, angle, base, tip, ttl float64
	reflect                     bool // rebote del escudo reflectante (dorado)
}

// hyperStar: estrella del efecto hiperespacio (al pasar de nivel).
type hyperStar struct {
	ang, rad, spd float64
}

// cheatKey: tecla escrita para detectar cheatcodes (p.ej. "666", "zzz").
type cheatKey struct {
	r  rune
	at time.Time
}

// astPowerup: bolita que sueltan las naves enemigas al morir.
// kind 0 = asistente rotatorio, kind 1 = vida extra.
type astPowerup struct {
	x, y, vx, vy, ttl, spin float64
	kind                    int
}

// astOrb: asistente rotatorio que gira alrededor de la nave y dispara solo.
// Aguanta varios impactos antes de sacrificarse.
type astOrb struct {
	ang, dir, shootCd float64
	shotID            int
	hp, hpMax         int
}

// astOrbShot: bala disparada por un asistente.
type astOrbShot struct {
	x, y, vx, vy, ttl float64
	id                int
}

// astChain: segmento del rayo letal (blanco) que se ramifica entre objetivos.
type astChain struct {
	x1, y1, x2, y2, ttl float64
	level               int
}

// astHole: agujero negro que aparece en el centro cuando se agota el tiempo del
// nivel. Crece con el tiempo y atrae todo con su gravedad.
type astHole struct {
	x, y, r float64
	age     float64 // segundos desde que apareció
}

// ---------- el juego ----------

// Player es el estado de UN jugador dentro de la arena compartida. Los campos
// que antes vivían mezclados en Sim (nave, vidas, puntuación, armas, escudo,
// railgun, hiperespacio, compañero, asistentes y teclas) están aquí, con los
// MISMOS nombres, y la lógica por jugador los usa a través del puntero.
type Player struct {
	ID   int    // índice estable dentro de Sim.players (0 = jugador local)
	Name string // nombre visible (vacío en el jugador local/de un solo jugador)

	ship astShip

	// animación de muerte: tiempo restante de la onda expansiva y el sitio donde
	// cayó la nave (la nave reaparece en el centro, así que hay que recordarlo)
	deathT         float64
	deathX, deathY float64
	// reaparición: cuenta atrás y sitio donde se recompone (elegido al morir,
	// para poder enseñar allí el efecto de teletransporte)
	respawnT       float64
	spawnX, spawnY float64

	lives         int
	score         int
	kills         int // en PvP: naves de otros jugadores destruidas
	deaths        int // veces que le han reventado la nave
	permShield    float64
	hull          int     // golpes de casco que le quedan (0 = se destroza)
	hullRegen     float64 // tiempo acumulado para recuperar un punto de casco
	rapidFire     int
	laser         bool
	doubleShot    bool    // cada disparo lanza dos proyectiles (power-up)
	shieldT       float64 // escudo manual activo restante (máx 3s)
	shieldCd      float64 // recarga restante del escudo manual (10s)
	missilesLeft  int
	minesLeft     int
	missileReload float64 // acumulador para reponer misiles
	lethalCd      float64 // recarga del rayo letal

	railCharge        float64 // recarga acumulada
	railRechargeTotal float64 // recarga necesaria del último disparo (30..120 s)
	railCharging      bool    // cargando mientras se mantiene R
	railHold          float64 // tiempo manteniendo R (0..railHoldMax → más fuerte)
	railAngle         float64
	railFlash         float64 // destello del disparo ya soltado
	railFlashBase     float64 // grosor del haz en la base (cónico)
	railFlashHalf     float64 // semiángulo del cono del haz
	chargeBeep        float64 // temporizador del pitido de carga

	hyper      float64 // efecto hiperespacio (transición de nivel) en curso
	hyperStars []hyperStar
	companion  *astCompanion // nave aliada azul (power-up raro)
	orbs       []astOrb
	orbShots   []astOrbShot

	// piloto automático (solo lo usa el servidor con los bots)
	botLevel    int     // 0 torpe, 1 medio, 2 bueno
	botThink    float64 // tiempo hasta revisar la decisión
	botRailHold int     // si está cargando el railgun, ticks que le quedan
	botL        bool
	botR        bool
	botT        bool
	botF        bool

	// keys es el estado de las teclas mantenidas de ESTE jugador, por nombre
	// (los mismos nombres que usa Fyne: "Left", "Space", "R"...). El servidor
	// recibe exactamente esto por la red, así que la simulación no depende de la
	// plataforma.
	keys map[string]bool

	// aimLead es cuánto se adelanta la pose al disparar (compensación de lag).
	// Lo ajusta el servidor con el RTT medido de este jugador: en localhost
	// sobra casi todo el adelanto y en LAN hace falta más. Sin él, el disparo
	// sale desalineado de la nave unas veces y otras no (según el viaje).
	aimLead float64
}

type Sim struct {
	mu   sync.Mutex
	w, h float64
	// players son los jugadores de la arena compartida. Siempre hay al menos uno
	// (índice 0 = jugador local); AddPlayer añade más sin solaparlos.
	players []*Player
	rocks   []astRock
	bullets []astBullet
	parts   []astParticle

	missiles    []astMissile
	mines       []astMine
	enemies     []astEnemy
	enemyShots  []astEnemyBullet
	chains      []astChain
	bossBeams   []astBossBeam
	boss        *astBoss
	bossCount   int
	fadeIn      float64
	levelIntro  float64
	powerups    []astPowerup
	nextShotID  int
	cheatBuf    []cheatKey
	cheatLastAt time.Time

	gameTime    float64
	nextEnemyAt float64
	nextPowerup float64 // cuándo suelta un power-up suelto (campo fijo)

	// cronómetro del nivel y agujero negro
	timeLeft     float64 // segundos restantes (puede ser negativo)
	timeWarned   bool    // ya avisó de los últimos segundos
	rockRushMult float64 // multiplicador de velocidad de las rocas (1 = normal)
	hole         *astHole

	enemiesKilled int
	muted         atomic.Bool

	Best      int    // récord de puntuación (lo persiste la plataforma)
	BestName  string // quién lo consiguió (en multijugador, el nombre del jugador)
	BestLevel int    // nivel máximo alcanzado en la partida del récord
	level     int
	over      bool
	paused    bool
	pvp       bool          // todos contra todos: las armas hacen daño a los demás jugadores
	arena     string        // tipo de arena: "rocks" (asteroides) o "fixed" (campo fijo)
	aimPose   map[int]aimPt // pose de disparo por jugador (solo en el servidor)
	// localID es el jugador que maneja este cliente: el HUD enseña SUS armas (en
	// multijugador no tiene por qué ser el 0). Lo pone el cliente al conectarse.
	localID    int
	bulletLead map[int][2]float64 // adelanto de dibujo de las balas propias (cliente)
	shots      int                // disparos hechos (para que el cliente de su predicción suene)
	lastLaser  bool               // el último disparo fue láser (para el mismo fin)
	started    bool
	flash      float64

	// Hooks que pone la plataforma. En el servidor quedan a nil.
	OnSound SoundFunc                         // reproducir un sonido
	OnBest  func(best, level int, who string) // se ha batido el récord (persistir)
}

// playerRender es una copia inmutable del estado de un jugador, para dibujar
// sin el lock tomado.
type playerRender struct {
	id             int // para el color de la nave
	name           string
	kills          int
	deaths         int
	deathT         float64
	deathX, deathY float64
	// reaparición: cuenta atrás y sitio donde se recompone (elegido al morir,
	// para poder enseñar allí el efecto de teletransporte)
	respawnT       float64
	spawnX, spawnY float64
	ship           astShip
	shieldT        float64
	shieldCd       float64
	permShield     float64
	hull           int
	rapidFire      int
	laser          bool
	doubleShot     bool
	lethalCd       float64

	railCharge        float64
	railRechargeTotal float64
	railCharging      bool
	railHold          float64
	railAngle         float64
	railFlash         float64
	railFlashBase     float64
	railFlashHalf     float64

	missilesLeft int
	minesLeft    int
	score        int
	lives        int

	hyper      float64
	hyperStars []hyperStar

	orbs     []astOrb
	orbShots []astOrbShot

	comp    astCompanion
	hasComp bool
}

// Players devuelve todos los jugadores de la partida.
func (g *Sim) Players() []*Player { return g.players }

// Player devuelve el jugador i, o nil si no existe.
func (g *Sim) Player(i int) *Player {
	if i < 0 || i >= len(g.players) {
		return nil
	}
	return g.players[i]
}

// Local devuelve el jugador local (índice 0). Nunca es nil tras New.
func (g *Sim) Local() *Player {
	if len(g.players) == 0 {
		return nil
	}
	return g.players[0]
}

// AddPlayer añade un jugador nuevo a la misma arena, repartido alrededor del
// centro para que no aparezca encima de los demás.
func (g *Sim) AddPlayer(name string) *Player {
	g.mu.Lock()
	defer g.mu.Unlock()
	p := newPlayer(len(g.players), name)
	p.missilesLeft = g.missileStockLocked() // 1 en PvP
	g.players = append(g.players, p)
	g.placePlayerLocked(p)
	// Un jugador que entra reanuda la partida: si el mundo estaba en GAME OVER
	// (porque se murieron todos), empieza una ronda nueva.
	g.over = false
	return p
}

// newPlayer crea un jugador con el equipamiento inicial de una partida nueva.
func newPlayer(id int, name string) *Player {
	return &Player{
		ID:                id,
		Name:              name,
		lives:             livesPerMatch,
		missilesLeft:      missileStock,
		minesLeft:         mineStock,
		hull:              shipHullMax,
		aimLead:           0.03,            // el servidor lo ajusta con el RTT medido
		railCharge:        railRechargeMin, // el railgun empieza listo
		railRechargeTotal: railRechargeMin,
		keys:              map[string]bool{},
	}
}

// placePlayerLocked coloca al jugador repartido alrededor del centro de la
// arena (el llamador tiene el lock tomado). Reparte a todos en un círculo para
// que no se solapen.
func (g *Sim) placePlayerLocked(p *Player) {
	w, h := g.w, g.h
	if w == 0 || h == 0 {
		w, h = 800, 600
	}
	// el mejor sitio libre: lo más lejos posible de los bloques y de los demás
	p.ship.x, p.ship.y = g.spawnSpotLocked(p)
	p.ship.vx, p.ship.vy = 0, 0
	p.ship.angle = -math.Pi / 2
	p.ship.alive = true
	p.ship.invuln = 2
}

// allPlayersDead indica que ningún jugador tiene vidas: fin de partida. Una sala
// vacía NO está "acabada" (si no, al entrar alguien se encontraría el mundo
// congelado).
func (g *Sim) allPlayersDead() bool {
	if len(g.players) == 0 {
		return false
	}
	for _, p := range g.players {
		if p.lives >= 0 {
			return false
		}
	}
	return true
}

// nearestAlivePlayer devuelve el jugador vivo más cercano a (x,y), o nil si no
// queda ninguno. Lo usan los enemigos y el jefe para elegir objetivo.
func (g *Sim) nearestAlivePlayer(x, y float64) *Player {
	var best *Player
	bd := math.Inf(1)
	for _, p := range g.players {
		if !p.ship.alive {
			continue
		}
		if d := math.Hypot(p.ship.x-x, p.ship.y-y); d < bd {
			bd, best = d, p
		}
	}
	return best
}

// nearestAlivePlayerWithin es nearestAlivePlayer pero solo si está dentro de r.
func (g *Sim) nearestAlivePlayerWithin(x, y, r float64) *Player {
	var best *Player
	bd := r
	for _, p := range g.players {
		if !p.ship.alive {
			continue
		}
		if d := math.Hypot(p.ship.x-x, p.ship.y-y); d < bd {
			bd, best = d, p
		}
	}
	return best
}

// Tipos de power-up.
const (
	powerupOrb       = 0 // asistente rotatorio
	powerupLife      = 1 // vida extra
	powerupShield    = 2 // escudo permanente (+100 puntos)
	powerupRapid     = 3 // cadencia x2
	powerupMissiles  = 4 // +10 misiles
	powerupMines     = 5 // +10 minas
	powerupLaser     = 6 // cambia el disparo a láser
	powerupCompanion = 7 // nave aliada azul
	powerupGun       = 8 // vuelve a las balas (ametralladora)
	powerupDouble    = 9 // disparo doble: cada disparo lanza dos proyectiles
)

// bossDieSounds es cuántos sonidos de muerte de jefe distintos hay (rotan).
const bossDieSounds = 6

const (
	missileStock = 1 // se empieza con UN misil (y no se recarga)
	mineStock    = 5
	// livesPerMatch son las vidas con las que se empieza cada partida.
	livesPerMatch = 5
	// shieldEnemyHitCost es lo que gasta el escudo permanente al encajar un
	// disparo de otro jugador (PvP).
	shieldEnemyHitCost = 60.0
	// Al reaparecer (o al empezar) la nave es invulnerable unos segundos: tiempo
	// para colocarse sin que te revienten nada más salir.
	shipInvulnSpawn = 4.5
	// Duración de la animación de muerte (trozos volando).
	shipDeathAnim = 1.6
	// railReflectBase es el grosor base del haz rebotado (el triple del normal).
	railReflectBase = 14.0
	// respawnDelay es lo que tarda la nave en recomponerse tras morir (mientras,
	// se ve el efecto de teletransporte en el sitio de reaparición).
	respawnDelay = 1.9
	// Velocidad de los misiles del jugador. En cooperativo van MÁS LENTOS que en
	// 'vs': allí persiguen asteroides (blancos grandes y lentos, no hace falta
	// correr) y así da tiempo a reventarlos a tiros; en 'vs' van algo más rápidos
	// porque persiguen naves que huyen (y matan).
	missileSpeed     = 150.0
	missileSpeedPvP  = 206.0
	shieldOnTime     = 3.0
	shieldReload     = 10.0
	railHoldMax      = 10.0              // máximo tiempo cargando con R
	railRechargeMin  = 30.0              // recarga del disparo más débil
	railRechargeMax  = 120.0             // recarga del disparo a plena carga (10 s)
	fireIntervalBase = 0.66 / 1.15 / 1.1 // cadencia normal (s): +15% y luego +10% más
	shipSpeedMax     = 340.0             // tope de velocidad de la nave
	// Movimiento de la nave. El rozamiento es POR PASO de 16 ms y el cliente usa
	// los mismos valores (redondeando con Pow), o la nave iría distinta en cada
	// pantalla. shipThrust se ajusta junto con shipFriction para que el motor
	// empuje igual (la nave acelera igual hacia el tope) y lo único que cambie
	// sea lo que resbala al soltarlo: deriva ≈ (shipSpeedMax*0.016)/(1-f).
	shipThrust      = 489.0
	shipFriction    = 0.983
	ramDamageBase   = 3.0   // daño de embestida a velocidad cero (a tope: el doble)
	blastDamage     = 5     // daño de una explosión (misil/mina) a una nave enemiga
	fireIntervalMin = 0.04  // cadencia mínima con power-ups de cadencia
	maxRapidFire    = 3     // stacks máximos de cadencia (x2 cada uno → tope x8)
	bulletSpeed     = 430.0 // velocidad y alcance de la bala normal
	bulletTTL       = 1.1
	laserSpeed      = 1800.0 // el láser va mucho más rápido, con el mismo alcance
	laserTTL        = bulletSpeed * bulletTTL / laserSpeed
	laserDashBack   = 44.0  // cola del rayo (px): larga, para que el haz salga pegado a la nave
	laserDashFront  = 30.0  // punta del rayo (px) por delante
	laserFireFactor = 0.75  // el láser también dispara algo más rápido
	permShieldCap   = 500.0 // nivel de escudo máximo (5 acumulaciones de 100)
	permShieldPer   = 100.0 // puntos que da cada power-up de escudo
	enemyFirstAt    = 20.0
	enemyRespawn    = 18.0

	lethalCooldown   = 3.0    // recarga del rayo letal
	lethalMaxLevel   = 4      // niveles de ramificación
	lethalFirstRange = 1040.0 // alcance del primer rayo (el doble)
	lethalBaseRadius = 150.0  // radio de los objetivos hijos (se va dividiendo a la mitad)

	bossEnergyBase = 80.0  // energía del jefe en su 1ª aparición (nivel 2, más asequible)
	bossEnergyPer  = 130.0 // energía extra por cada aparición

	orbMax        = 3     // máximo de asistentes rotatorios
	orbHP         = 3     // impactos que aguanta un asistente (el triple que antes)
	orbRadius     = 34.0  // radio de giro alrededor de la nave
	orbRotSpeed   = 2.4   // rad/s
	orbShootRange = 270.0 // alcance de disparo automático
	orbShotSpeed  = 430.0
	hyperTime     = 3.0 // duración del efecto hiperespacio

	// cronómetro del nivel
	levelTimeTotal = 90.0 // minuto y medio por nivel
	levelTimeWarn  = 30.0 // últimos 30 s: en rojo y las rocas aceleran

	// empuje de las rocas en la fase final (crece poco a poco, con tope)
	rockRushRate = 0.07 // +7 % de velocidad por segundo durante el aviso
	rockRushMax  = 2.5  // tope del multiplicador de velocidad de las rocas
	rockSpeedMax = 420.0

	// agujero negro (aparece al agotarse el cronómetro)
	holeGrowRate    = 0.5   // px/s de crecimiento del radio
	holeRMax        = 130.0 // radio máximo
	holePullK       = 600.0 // aceleración de referencia a la distancia del radio
	holeUnstoppable = 180.0 // s tras aparecer: ni los motores ni el teleport sirven

	// daño al escudo permanente: +100 % por cada nivel de dureza (tope x3)
	shieldHardStep = 1.0
	shieldHardMax  = 3.0

	// power-ups de munición
	ammoPowerup = 10 // minas que da cada power-up de minas
	// missilePowerupAmmo son los misiles que da el power-up de misiles. Son pocos
	// a propósito: se empieza con uno y no se recarga, así que un misil es algo
	// que se dosifica.
	missilePowerupAmmo = 2

	// compañero (nave aliada rosa): power-up raro, 1 de cada N enemigos
	companionDropEvery = 10    // 1 de cada 10 enemigos batidos
	companionHP        = 3     // impactos que aguanta antes de morir
	companionThrust    = 210.0 // empuje de sus motores (flojo)
	companionSpeedMax  = 250.0
	companionStandoff  = 150.0 // distancia a la que se queda del objetivo
	companionOrbit     = 90.0  // distancia a la que ronda a la nave si no hay nada
	companionRange     = 430.0 // alcance de sus disparos
	companionFireCd    = 0.50  // cadencia de disparo (lenta)
	companionHitInvuln = 1.2   // invulnerabilidad tras recibir un impacto
	companionThink     = 0.6   // cada cuánto reevalúa el objetivo (reacciona tarde)
	companionTurn      = 1.7   // rad/s de giro: gira despacio y se pasa de largo
	companionSpread    = 0.18  // dispersión de sus disparos (rad, ±0.09)
	companionAimTol    = 0.35  // margen de encare para disparar
	companionPickup    = 320.0 // si hay un power-up a tiro, va a por él
)

// New crea una partida nueva. La plataforma rellena después Best/BestLevel y los
// hooks OnSound/OnBest si le interesan.
func New() *Sim {
	g := &Sim{
		level:       1,
		nextEnemyAt: enemyFirstAt,
	}
	g.players = append(g.players, newPlayer(0, ""))
	g.resetShip(g.players[0])
	g.newLevel()
	g.started = true
	g.playSound("start", 0)
	return g
}

func (g *Sim) resetShip(p *Player) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p.shieldT, p.shieldCd = 0, 0
	sx, sy := g.spawnSpotLocked(p) // el hueco más despejado (lejos de bloques y jugadores)
	p.ship = astShip{
		x: sx, y: sy,
		angle: -math.Pi / 2,
		alive: true, invuln: shipInvulnSpawn,
	}
}

// resetPlayerLocked deja al jugador como recién empezada la partida (sin tocar
// la nave, que la coloca resetShip). El llamador tiene el lock tomado.
func (g *Sim) resetPlayerLocked(p *Player) {
	p.lives = livesPerMatch
	p.score = 0
	p.permShield = 0
	p.rapidFire = 0
	p.laser = false
	p.doubleShot = false
	p.shieldT = 0
	p.shieldCd = 0
	p.missilesLeft = g.missileStockLocked()
	p.minesLeft = mineStock
	p.missileReload = 0
	p.lethalCd = 0
	p.railCharge = railRechargeMin
	p.railRechargeTotal = railRechargeMin
	p.railCharging = false
	p.railHold = 0
	p.railFlash = 0
	p.chargeBeep = 0
	p.hyper = 0
	p.hyperStars = nil
	p.companion = nil
	p.orbs = nil
	p.orbShots = nil
	if p.keys == nil {
		p.keys = map[string]bool{}
	} else {
		for k := range p.keys {
			delete(p.keys, k)
		}
	}
}

func (g *Sim) newLevel() {
	g.mu.Lock()
	g.newLevelLocked()
	g.mu.Unlock()
}

// newLevelLocked añade las rocas del nivel y hace aparecer al jefe. El llamador
// debe tener g.mu tomado.
func (g *Sim) newLevelLocked() {
	w, h := g.w, g.h
	if w == 0 {
		w, h = 800, 600
	}
	// cada nivel arranca con el cronómetro a 3 min, sin agujero y a velocidad normal
	g.timeLeft = levelTimeTotal
	g.timeWarned = false
	g.rockRushMult = 1
	g.hole = nil
	if g.fixedArena() {
		// campo fijo: estructuras que no se mueven, sin agujero negro ni prisa
		g.buildFixedArenaLocked(w, h)
		g.nextPowerup = powerupSpawnEvery
		return
	}
	// +20% de asteroides iniciales en todos los niveles
	n := int(math.Ceil(float64(3+g.level) * 1.2))
	// a partir del nivel 2 hay rocas duras, y más en cada nivel
	hard := 0
	if g.level >= 2 {
		hard = g.level - 1
		if hard > n-1 {
			hard = n - 1 // siempre queda alguna roca normal
		}
	}
	for i := 0; i < n; i++ {
		r := g.makeRock(w, h, 3)
		if i < hard {
			// cada "nivel de grosor" añade 3 disparos: 1, 4, 7, 10…
			r.hpMax = g.level
			r.hp = 1 + 3*(g.level-1)
		}
		g.rocks = append(g.rocks, r)
	}
	// la nave grande (jefe) aparece a partir del nivel 2
	if g.level >= 2 {
		g.spawnBoss()
	}
}

// shieldHardMult es el multiplicador de daño al escudo según la dureza de la
// roca (hpMax): las normales no cambian, las duras cuestan el doble y las más
// duras el triple.
func shieldHardMult(hpMax int) float64 {
	if hpMax <= 1 {
		return 1
	}
	m := 1 + shieldHardStep*float64(hpMax-1)
	if m > shieldHardMax {
		m = shieldHardMax
	}
	return m
}

// shieldCost es la energía de escudo que cuesta embestir algo de radio r
// (el choque no sale gratis: proporcional al tamaño) y de dureza `hard`
// (hpMax de la roca; 1 para naves y jefe).
func shieldCost(r float64, hard int) float64 {
	c := r / 24 * 50 // ~96 puntos una roca grande (r=46) sobre un tope de 500
	if c < 25 {
		c = 25
	}
	return c * shieldHardMult(hard)
}

// ramDamage es el daño que hace la nave al embestir una roca (con escudo):
// crece con la velocidad, de modo que a tope (shipSpeedMax) hace el doble que
// parada.
func ramDamage(speed float64) int {
	if speed < 0 {
		speed = 0
	}
	if speed > shipSpeedMax {
		speed = shipSpeedMax
	}
	return int(math.Round(ramDamageBase * (1 + speed/shipSpeedMax)))
}

// spendShield gasta energía del escudo permanente (entre 0 y permShieldCap).
func (g *Sim) spendShield(p *Player, cost float64) {
	p.permShield -= cost
	if p.permShield < 0 {
		p.permShield = 0
	}
}

// awardScore suma puntos al jugador local. Los subsistemas del mundo (rocas,
// naves, jefe) no saben qué jugador causó cada baja; en esta fase el mérito va
// al jugador local (con un jugador, idéntico a antes).
func (g *Sim) awardScore(n int) {
	if lp := g.Local(); lp != nil {
		lp.score += n
	}
}

// rockDamage aplica daño a una roca; si agota su hp, se rompe.
func (g *Sim) rockDamage(i, amt int) {
	if i < 0 || i >= len(g.rocks) {
		return
	}
	r := &g.rocks[i]
	r.hp -= amt
	if r.hp <= 0 {
		g.destroyRock(i)
		return
	}
	// chispas del rebote (las rocas duras aguantan varios golpes)
	for k := 0; k < 6; k++ {
		a := rand.Float64() * 2 * math.Pi
		sp := 40 + rand.Float64()*90
		g.parts = append(g.parts, astParticle{
			x: r.x, y: r.y, vx: math.Cos(a) * sp, vy: math.Sin(a) * sp,
			ttl: 0.2 + rand.Float64()*0.2,
		})
	}
	g.playSound("bangSmall", 40*time.Millisecond)
}

// sparks suelta un puñado de chispas: el impacto tiene que VERSE.
func (g *Sim) sparks(x, y float64, n int, speed float64) {
	for k := 0; k < n; k++ {
		a := rand.Float64() * 2 * math.Pi
		sp := speed * (0.4 + rand.Float64()*0.6)
		g.parts = append(g.parts, astParticle{
			x: x, y: y,
			vx: math.Cos(a) * sp, vy: math.Sin(a) * sp,
			ttl: 0.12 + rand.Float64()*0.22,
		})
	}
}

// pushOutOf devuelve el punto justo fuera de un bloque (para no dejar la nave
// dentro después del golpe).
func pushOutOf(r *astRock, x, y float64) (float64, float64) {
	dx, dy := x-r.x, y-r.y
	d := math.Hypot(dx, dy)
	if d < 0.001 {
		dx, dy, d = 1, 0, 1
	}
	return r.x + dx/d*(r.r+14), r.y + dy/d*(r.r+14)
}

// bounceOff quita el impulso que metía la nave en el bloque y le da un rebote
// suave hacia fuera.
func (g *Sim) bounceOff(r *astRock, s *astShip) {
	dx, dy := s.x-r.x, s.y-r.y
	d := math.Hypot(dx, dy)
	if d < 0.001 {
		dx, dy, d = 1, 0, 1
	}
	nx, ny := dx/d, dy/d
	if vn := s.vx*nx + s.vy*ny; vn < 0 {
		s.vx -= vn * nx * 1.3 // 1.0 = solo quitarlo; 1.3 = rebote suave
		s.vy -= vn * ny * 1.3
	}
}

// hitRock aplica un impacto simple (1 disparo).
func (g *Sim) hitRock(i int) {
	g.rockDamage(i, 1)
}

func (g *Sim) makeRock(w, h float64, size int) astRock {
	radii := map[int]float64{3: 46, 2: 26, 1: 14}
	r := radii[size]
	// nace en el borde, lejos de la nave
	var x, y float64
	switch rand.Intn(4) {
	case 0:
		x, y = rand.Float64()*w, -r
	case 1:
		x, y = rand.Float64()*w, h+r
	case 2:
		x, y = -r, rand.Float64()*h
	default:
		x, y = w+r, rand.Float64()*h
	}
	// dificultad: mezcla de rocas lentas y rápidas (y sube con el nivel)
	sp := 28 + rand.Float64()*70
	if rand.Float64() < 0.45 { // ~45% mucho más rápidas
		sp += 70 + rand.Float64()*120
	}
	sp += float64(3-size)*12 + float64(g.level-1)*6
	ang := rand.Float64() * 2 * math.Pi
	verts := 8 + rand.Intn(5)
	shape := make([]float64, verts)
	for i := range shape {
		shape[i] = 0.72 + rand.Float64()*0.5
	}
	return astRock{
		x: x, y: y,
		vx: math.Cos(ang) * sp, vy: math.Sin(ang) * sp,
		r: r, size: size,
		angle: rand.Float64() * 6, spin: (rand.Float64() - 0.5) * 2,
		shape: shape, hp: 1, hpMax: 1,
		// OJO: -1 = sin power-up dentro. Si se dejara el cero (que es el valor
		// por defecto) sería powerupOrb: cada asteroide roto soltaría una bolita.
		powerup: -1,
	}
}

// (el bucle de juego y el repintado viven en la plataforma: aquí solo hay Step)

// Step avanza la simulación dt segundos. Es la única puerta de entrada al
// estado: la plataforma la llama desde su bucle (cliente) o desde el tick
// (servidor). El estado se toca siempre bajo g.mu.
func (g *Sim) Step(dt float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.commitCheats()
	if g.over || g.paused {
		return
	}
	// hiperespacio: sólo estrellas, todavía sin escena. Es una transición de
	// nivel compartida, así que la lleva el jugador local (con un jugador queda
	// idéntico a antes).
	if lp := g.Local(); lp != nil && lp.hyper > 0 {
		lp.hyper -= dt
		for i := range lp.hyperStars {
			lp.hyperStars[i].spd += 900 * dt
			lp.hyperStars[i].rad += lp.hyperStars[i].spd * dt
		}
		if lp.hyper <= 0 {
			lp.hyper = 0
			lp.hyperStars = nil
			g.newLevelLocked() // aparece la escena nueva…
			g.fadeIn = 0.9     // …con fundido y en pausa
		}
		return
	}
	// la escena nueva aparece suavemente y en pausa
	if g.fadeIn > 0 {
		g.fadeIn -= dt
		if g.fadeIn <= 0 {
			g.fadeIn = 0
			g.levelIntro = 2.5 // "Nivel XXX" al empezar
		}
		return
	}
	if g.levelIntro > 0 {
		g.levelIntro -= dt
	}
	w, h := g.w, g.h
	if w == 0 {
		return
	}
	if g.flash > 0 {
		g.flash -= dt * 3
	}

	// --- por jugador: input, física de la nave y subsistemas propios ---
	for _, p := range g.players {
		g.stepPlayerLocked(p, dt, w, h)
	}

	// --- subsistemas del mundo (se ejecutan UNA vez para toda la arena) ---

	// balas (la gravedad del agujero las curva y se las traga)
	live := g.bullets[:0]
	for _, b := range g.bullets {
		if ax, ay := g.holeAccel(b.x, b.y); ax != 0 || ay != 0 {
			b.vx += ax * dt
			b.vy += ay * dt
		}
		b.x += b.vx * dt
		b.y += b.vy * dt
		b.ttl -= dt
		wrap(&b.x, &b.y, w, h)
		if b.ttl > 0 && !g.holeSwallows(b.x, b.y) {
			live = append(live, b)
		}
	}
	g.bullets = live

	// escudo reflectante: devuelve los disparos que le entran
	g.reflectShotsLocked()

	// partículas
	plive := g.parts[:0]
	for _, p := range g.parts {
		p.x += p.vx * dt
		p.y += p.vy * dt
		p.ttl -= dt
		if p.ttl > 0 {
			plive = append(plive, p)
		}
	}
	g.parts = plive

	// rocas (con el empuje de la fase final y la gravedad del agujero; el
	// agujero no se las traga, solo las zarandea). Las estructuras del campo
	// fijo no se mueven.
	for i := range g.rocks {
		r := &g.rocks[i]
		if r.fixed {
			continue
		}
		if ax, ay := g.holeAccel(r.x, r.y); ax != 0 || ay != 0 {
			r.vx += ax * dt
			r.vy += ay * dt
			if sp := math.Hypot(r.vx, r.vy); sp > rockSpeedMax {
				r.vx, r.vy = r.vx/sp*rockSpeedMax, r.vy/sp*rockSpeedMax
			}
		}
		r.x += r.vx * g.rockRushMult * dt
		r.y += r.vy * g.rockRushMult * dt
		r.angle += r.spin * dt
		wrap(&r.x, &r.y, w, h)
	}

	// cronómetro del nivel: al llegar a 0 aparece el agujero negro y el reloj
	// sigue en negativo (parpadeando), mientras las rocas van acelerando. En el
	// campo fijo no hay nada de esto: ni prisa ni agujero.
	if g.fixedArena() {
		g.timeLeft = levelTimeTotal
		g.rockRushMult = 1
		if g.hole != nil {
			g.hole = nil
		}
	} else {
		g.timeLeft -= dt
		if !g.timeWarned && g.timeLeft <= levelTimeWarn {
			g.timeWarned = true
			g.playSound("alert", 0)
		}
		if g.timeLeft <= 0 && g.hole == nil {
			g.spawnHoleLocked()
		}
		if g.timeLeft <= levelTimeWarn {
			g.rockRushMult *= 1 + rockRushRate*dt
			if g.rockRushMult > rockRushMax {
				g.rockRushMult = rockRushMax
			}
		}
		g.updateHole(dt)
	}

	// tiempo de juego y aparición de la oleada enemiga (a los 20 s, el doble por nivel)
	g.gameTime += dt
	// en el campo fijo no hay enemigos: es un duelo entre jugadores
	if !g.fixedArena() && g.gameTime >= g.nextEnemyAt && len(g.enemies) == 0 {
		g.spawnWave()
		g.nextEnemyAt = g.gameTime + enemyRespawn
	}

	g.updateMissiles(dt)
	g.updateMines(dt)
	g.updateEnemies(dt)
	g.updateBoss(dt)
	g.updateBossBeams(dt)
	g.updatePowerups(dt)
	g.updateEnemyShots(dt)
	g.updateChains(dt)

	// colisiones bala-roca
	var keep []astBullet
	for _, b := range g.bullets {
		// El proyectil se mueve mucho por frame (el láser, ~29 px; la bala, ~7):
		// se comprueba TODO el recorrido del frame y no solo el punto final, que
		// si no el láser "atravesaba" las rocas sin tocarlas.
		px, py := b.x-b.vx*dt, b.y-b.vy*dt
		hit := false
		for i := range g.rocks {
			r := &g.rocks[i]
			if distToSegment(px, py, b.x, b.y, r.x, r.y) < r.r {
				hit = true
				g.hitRock(i)
				break
			}
		}
		if !hit {
			for i := range g.enemies {
				if distToSegment(px, py, b.x, b.y, g.enemies[i].x, g.enemies[i].y) < g.enemies[i].r+2 {
					g.damageEnemy(i, 1)
					hit = true
					break
				}
			}
		}
		// daño al jefe
		if !hit && g.boss != nil {
			if distToSegment(px, py, b.x, b.y, g.boss.x, g.boss.y) < g.boss.r {
				g.damageBoss(5)
				hit = true
			}
		}
		// las balas del jugador también pueden destruir las balas enemigas
		if !hit {
			for i := range g.enemyShots {
				if g.enemyShots[i].ttl > 0 && distToSegment(px, py, b.x, b.y, g.enemyShots[i].x, g.enemyShots[i].y) < 9 {
					g.enemyShots[i].ttl = 0
					hit = true
					break
				}
			}
		}
		// las balas revientan los misiles en el aire (defensa antimisil): no los
		// del que dispara, para no reventarse los suyos. El radio es generoso y se
		// mira TODO el recorrido del frame: así un disparo que pasa cerca también
		// los revienta, no hace falta darles de lleno
		if !hit && !b.pink {
			for i := range g.missiles {
				mi := &g.missiles[i]
				if mi.owner == b.owner {
					continue
				}
				if distToSegment(px, py, b.x, b.y, mi.x, mi.y) < bulletMissileHitR {
					g.explosion(mi.x, mi.y, 40, g.playerByIDLocked(b.owner))
					g.missiles = append(g.missiles[:i], g.missiles[i+1:]...)
					hit = true
					break
				}
			}
		}
		// en PvP, las balas también hacen daño a las naves de los demás (no a la
		// del que disparó, ni a los disparos del compañero)
		if !hit && g.pvp && !b.pink {
			for _, o := range g.players {
				if o.ID == b.owner || !o.ship.alive {
					continue
				}
				// el radio es generoso a propósito: el cliente extrapola las naves
				// (las dibuja algo por delante) y la silueta llega a ~18 px del
				// centro, así que con radios pequeños una bala que se veía tocando
				// no explotaba en el servidor
				if distToSegment(px, py, b.x, b.y, o.ship.x, o.ship.y) < 20 {
					g.damagePlayer(o, shieldEnemyHitCost, g.playerByIDLocked(b.owner))
					hit = true
					break
				}
			}
		}
		if hit {
			// el impacto tiene que verse: un puñado de chispas donde toca
			g.sparks(b.x, b.y, 4, 110)
		} else {
			keep = append(keep, b)
		}
	}
	g.bullets = keep
	// limpiar ya las balas enemigas destruidas por las del jugador
	es := g.enemyShots[:0]
	for _, s := range g.enemyShots {
		if s.ttl > 0 {
			es = append(es, s)
		}
	}
	g.enemyShots = es

	// nivel superado → efecto hiperespacio; la escena nueva aparece después
	lp := g.Local()
	if len(g.rocks) == 0 && (lp == nil || lp.hyper <= 0) && g.fadeIn <= 0 {
		g.level++
		if lp != nil {
			g.startHyper(lp)
		}
		g.playSound("hyper", 0)
		// sólo estrellas: se limpia la escena
		g.enemies = nil
		g.enemyShots = nil
		g.boss = nil
		g.bossBeams = nil
		g.mines = nil
		g.powerups = nil
		g.chains = nil
		g.hole = nil
	}
}

// stepPlayerLocked avanza a un jugador: input, rotación, empuje, escudo,
// disparo, fricción, gravedad del agujero e integración de la nave, más sus
// subsistemas propios (railgun, rayo letal, asistentes, compañero, disparos de
// los asistentes, recarga de misiles, colisiones nave-roca y muerte por agujero
// negro). El llamador tiene el lock tomado.
// collideShipRocksLocked comprueba si la nave de p choca con una roca o con una
// estructura del campo fijo. El llamador tiene el lock tomado.
func (g *Sim) collideShipRocksLocked(p *Player, s *astShip) {
	if !s.alive || s.invuln > 0 {
		return
	}
	for i := range g.rocks {
		r := &g.rocks[i]
		if math.Hypot(s.x-r.x, s.y-r.y) < r.r+10 {
			// el impacto directo hace más daño cuanto más rápida va la nave
			dmg := ramDamage(math.Hypot(s.vx, s.vy))
			if p.permShield > 0 {
				// escudo permanente: rompe la roca pero cuesta energía proporcional
				g.spendShield(p, shieldCost(r.r, r.hpMax))
				g.rockDamage(i, dmg)
				g.playSound("shield", 120*time.Millisecond)
			} else if p.shieldT > 0 {
				g.rockDamage(i, dmg)
				g.playSound("shield", 120*time.Millisecond)
			} else {
				// ni escudo ni nada: el choque quita CASCO (tres golpes y la nave
				// revienta), no mata de una vez
				p.hull--
				p.hullRegen = 0
				s.x, s.y = pushOutOf(r, s.x, s.y)
				g.bounceOff(r, s)
				g.sparks(s.x, s.y, 12, 160)
				if p.hull <= 0 {
					g.killShip(p)
					return
				}
				g.playSound("hullHit", 90*time.Millisecond)
			}
			// pequeño margen para no encadenar varios impactos en el mismo roce
			if s.alive {
				s.invuln = math.Max(s.invuln, 0.6)
			}
			return
		}
	}
}

func (g *Sim) stepPlayerLocked(p *Player, dt, w, h float64) {
	// la animación de muerte corre aunque la nave no esté viva
	if p.deathT > 0 {
		p.deathT -= dt
	}
	// recomposición: al acabarse el retardo aparece la nave en el sitio elegido
	if !p.ship.alive && p.respawnT > 0 {
		p.respawnT -= dt
		if p.respawnT <= 0 {
			p.respawnT = 0
			p.ship = astShip{
				x: p.spawnX, y: p.spawnY,
				angle: -math.Pi / 2,
				alive: true, invuln: shipInvulnSpawn,
			}
			p.hull = shipHullMax // casco nuevo al reaparecer
			p.hullRegen = 0
			// el sitio se limpia de minas: si no, apareces encima de una y mueres
			// nada más llegar (las minas matan aunque estés invulnerable)
			g.clearMinesNearLocked(p.spawnX, p.spawnY, mineClearRadius)
			// al materializarse: sonido propio (el 'shipDie' de la muerte ya sonó)
			g.playSound("materialize", 0)
		}
	}
	s := &p.ship
	if s.alive {
		if s.invuln > 0 {
			s.invuln -= dt
		}
		if s.teleCooldown > 0 {
			s.teleCooldown -= dt
		}
		// rotación (A/D, y las flechas como siempre)
		if p.keys[KeyA] || p.keys[KeyLeft] {
			s.angle -= ShipTurnRate * dt
		}
		if p.keys[KeyD] || p.keys[KeyRight] {
			s.angle += ShipTurnRate * dt
		}
		// empuje (W, y la flecha arriba)
		if p.keys[KeyW] || p.keys[KeyUp] {
			s.vx += math.Cos(s.angle) * shipThrust * dt
			s.vy += math.Sin(s.angle) * shipThrust * dt
			g.playSound("thrust", 110*time.Millisecond)
			// partícula de propulsión
			g.parts = append(g.parts, astParticle{
				x: s.x - math.Cos(s.angle)*12, y: s.y - math.Sin(s.angle)*12,
				vx:  -math.Cos(s.angle)*90 + (rand.Float64()-0.5)*30,
				vy:  -math.Sin(s.angle)*90 + (rand.Float64()-0.5)*30,
				ttl: 0.3,
			})
		}
		// escudo: hasta 3 s activo y luego 10 s de recarga
		if p.shieldT > 0 {
			p.shieldT -= dt
			if p.shieldT <= 0 {
				p.shieldT = 0
				p.shieldCd = shieldReload
				g.playSound("shieldOff", 120*time.Millisecond)
			}
		} else if p.shieldCd > 0 {
			p.shieldCd -= dt
			if p.shieldCd < 0 {
				p.shieldCd = 0
			}
		}
		// disparo sostenido (la cadencia la mejora el power-up correspondiente)
		if s.fireTimer > 0 {
			s.fireTimer -= dt
		}
		if p.keys[KeySpace] {
			g.fireLocked(p)
		}
		// fricción
		s.vx *= shipFriction
		s.vy *= shipFriction
		if sp := math.Hypot(s.vx, s.vy); sp > shipSpeedMax {
			s.vx, s.vy = s.vx/sp*shipSpeedMax, s.vy/sp*shipSpeedMax
		}
		// gravedad del agujero negro (se aplica tras el tope de velocidad para
		// que pueda arrastrar a la nave aunque los motores vayan a fondo)
		if ax, ay := g.holeAccel(s.x, s.y); ax != 0 || ay != 0 {
			s.vx += ax * dt
			s.vy += ay * dt
		}
		s.x += s.vx * dt
		s.y += s.vy * dt
		wrap(&s.x, &s.y, w, h)
	}

	// subsistemas propios del jugador
	g.updateRail(p, dt)
	if p.lethalCd > 0 {
		p.lethalCd -= dt
		if p.lethalCd < 0 {
			p.lethalCd = 0
		}
	}
	g.updateOrbs(p, dt)
	g.updateCompanion(p, dt)
	g.updateOrbShots(p, dt)

	// en el campo fijo van apareciendo power-ups sueltos de vez en cuando
	if g.fixedArena() {
		g.nextPowerup -= dt
		if g.nextPowerup <= 0 {
			g.nextPowerup = powerupSpawnEvery + rand.Float64()*powerupSpawnEvery
			g.spawnFreePowerupLocked()
		}
	}

	// el casco se va reparando solo si no te vuelven a golpear
	if p.hull < shipHullMax {
		p.hullRegen += dt
		if p.hullRegen >= hullRegenEvery {
			p.hullRegen = 0
			p.hull++
		}
	} else {
		p.hullRegen = 0
	}

	// Los misiles NO se recargan: se empieza con uno y los demás salen de los
	// power-ups (2 por power-up). Antes se reponían solos hasta 5.
	p.missileReload = 0

	g.collideShipRocksLocked(p, s)

	// muerte por agujero negro: si toca el agujero, es engullido
	if g.hole != nil && s.alive && math.Hypot(s.x-g.hole.x, s.y-g.hole.y) < g.hole.r+8 {
		g.holeKillLocked(p)
	}
}

func wrap(x, y *float64, w, h float64) {
	if *x < 0 {
		*x += w
	}
	if *x > w {
		*x -= w
	}
	if *y < 0 {
		*y += h
	}
	if *y > h {
		*y -= h
	}
}

// ---------- agujero negro ----------

// spawnHoleLocked hace aparecer el agujero negro en el centro de la pantalla.
// El llamador debe tener g.mu tomado.
func (g *Sim) spawnHoleLocked() {
	w, h := g.w, g.h
	if w == 0 {
		w, h = 800, 600
	}
	g.hole = &astHole{x: w / 2, y: h / 2, r: 6}
	g.playSound("hyper", 0)
}

// holeAccel devuelve la aceleración gravitatoria del agujero sobre (x,y). Es 0
// si no hay agujero: la fuerza crece con el radio y cae con la distancia.
func (g *Sim) holeAccel(x, y float64) (float64, float64) {
	h := g.hole
	if h == nil {
		return 0, 0
	}
	dx, dy := h.x-x, h.y-y
	d := math.Hypot(dx, dy)
	if d < 1e-6 {
		return 0, 0
	}
	dr := d
	if dr < h.r {
		dr = h.r
	}
	a := holePullK * h.r / dr
	return a * dx / d, a * dy / d
}

// holeSwallows indica si (x,y) ha caído dentro del agujero.
func (g *Sim) holeSwallows(x, y float64) bool {
	h := g.hole
	return h != nil && math.Hypot(x-h.x, y-h.y) < h.r
}

// holeUnstoppableNow indica si la gravedad ya no se puede contrarrestar (los
// motores no bastan y el teleport queda inutilizado).
func (g *Sim) holeUnstoppableNow() bool {
	return g.hole != nil && g.hole.age >= holeUnstoppable
}

// updateHole hace crecer el agujero y envejecerlo. El engullir de las naves se
// comprueba por jugador en stepPlayerLocked.
func (g *Sim) updateHole(dt float64) {
	h := g.hole
	if h == nil {
		return
	}
	h.age += dt
	if h.r < holeRMax {
		h.r += holeGrowRate * dt
		if h.r > holeRMax {
			h.r = holeRMax
		}
	}
}

// holeKillLocked: el jugador cae en el agujero negro. Pierde una vida y el
// nivel arranca de nuevo (rocas y cronómetro incluidos). Con el lock tomado.
func (g *Sim) holeKillLocked(p *Player) {
	g.killShip(p)
	if g.over {
		return
	}
	g.restartLevelLocked()
}

// restartLevelLocked vuelve a empezar el nivel actual sin cambiar de nivel: se
// repueblan las rocas y el jefe, y el cronómetro se pone a cero.
func (g *Sim) restartLevelLocked() {
	g.rocks = nil
	g.bullets = nil
	g.missiles = nil
	g.mines = nil
	g.enemies = nil
	g.enemyShots = nil
	g.chains = nil
	g.bossBeams = nil
	g.boss = nil
	g.hole = nil
	g.gameTime = 0
	g.nextEnemyAt = enemyFirstAt
	// el jefe que reaparece es el mismo: no se sube el contador de apariciones
	prevBoss := g.bossCount
	g.newLevelLocked()
	g.bossCount = prevBoss
	g.fadeIn = 0.9
}

// nearestPowerup devuelve el power-up más cercano a (x,y) dentro de r que
// además sirva de algo al jugador p ahora mismo.
func (g *Sim) nearestPowerup(p *Player, x, y, r float64) (float64, float64, bool) {
	best := r
	var bx, by float64
	ok := false
	for i := range g.powerups {
		if !g.powerupUseful(p, g.powerups[i].kind) {
			continue
		}
		if d := math.Hypot(g.powerups[i].x-x, g.powerups[i].y-y); d < best {
			best, bx, by, ok = d, g.powerups[i].x, g.powerups[i].y, true
		}
	}
	return bx, by, ok
}

// nearestTarget devuelve el objetivo más cercano (roca, nave enemiga o jefe).
func (g *Sim) nearestTarget(x, y float64) (float64, float64, bool) {
	best := math.Inf(1)
	var bx, by float64
	ok := false
	for i := range g.rocks {
		if d := math.Hypot(g.rocks[i].x-x, g.rocks[i].y-y); d < best {
			best, bx, by, ok = d, g.rocks[i].x, g.rocks[i].y, true
		}
	}
	for i := range g.enemies {
		if d := math.Hypot(g.enemies[i].x-x, g.enemies[i].y-y); d < best {
			best, bx, by, ok = d, g.enemies[i].x, g.enemies[i].y, true
		}
	}
	if g.boss != nil {
		if d := math.Hypot(g.boss.x-x, g.boss.y-y); d < best {
			best, bx, by, ok = d, g.boss.x, g.boss.y, true
		}
	}
	return bx, by, ok
}

// ---------- misiles ----------

func (g *Sim) launchMissile(p *Player) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !p.ship.alive || g.over || p.missilesLeft <= 0 || p.railCharging {
		return
	}
	p.missilesLeft--
	s := p.ship
	g.missiles = append(g.missiles, astMissile{
		x: s.x + math.Cos(s.angle)*14, y: s.y + math.Sin(s.angle)*14,
		angle: s.angle, ttl: 6, owner: p.ID,
	})
	g.playSound("missile", 120*time.Millisecond)
}

func (g *Sim) updateMissiles(dt float64) {
	live := g.missiles[:0]
	for _, m := range g.missiles {
		m.ttl -= dt
		if tx, ty, ok := g.nearestEnemyFor(m.x, m.y, m.owner); ok {
			des := math.Atan2(ty-m.y, tx-m.x)
			diff := math.Mod(des-m.angle+3*math.Pi, 2*math.Pi) - math.Pi
			turn := 3.4 * dt
			if diff > turn {
				diff = turn
			} else if diff < -turn {
				diff = -turn
			}
			m.angle += diff
		}
		sp := g.missileSpeedLocked()
		mvx, mvy := math.Cos(m.angle)*sp, math.Sin(m.angle)*sp
		if ax, ay := g.holeAccel(m.x, m.y); ax != 0 || ay != 0 {
			mvx += ax * dt // la gravedad del agujero desvía el misil
			mvy += ay * dt
		}
		m.x += mvx * dt
		m.y += mvy * dt
		wrap(&m.x, &m.y, g.w, g.h)
		if g.holeSwallows(m.x, m.y) {
			continue
		}
		m.trail = append(m.trail, astTrailPt{m.x, m.y})
		if len(m.trail) > 46 {
			m.trail = m.trail[1:]
		}
		hit := false
		for i := range g.rocks {
			if math.Hypot(m.x-g.rocks[i].x, m.y-g.rocks[i].y) < g.rocks[i].r+4 {
				g.explosion(m.x, m.y, 60, g.playerByIDLocked(m.owner))
				hit = true
				break
			}
		}
		if !hit {
			for i := range g.enemies {
				if math.Hypot(m.x-g.enemies[i].x, m.y-g.enemies[i].y) < g.enemies[i].r+6 {
					g.explosion(m.x, m.y, 50, g.playerByIDLocked(m.owner))
					hit = true
					break
				}
			}
		}
		if !hit && g.boss != nil {
			if math.Hypot(m.x-g.boss.x, m.y-g.boss.y) < g.boss.r+8 {
				g.explosion(m.x, m.y, 50, g.playerByIDLocked(m.owner))
				g.damageBoss(30)
				hit = true
			}
		}
		// en PvP también da a los demás jugadores (antes solo miraba rocas,
		// naves enemigas y el jefe, así que "no daba" al contrario)
		if !hit && g.pvp {
			for _, o := range g.players {
				if o.ID == m.owner || !o.ship.alive {
					continue
				}
				if math.Hypot(m.x-o.ship.x, m.y-o.ship.y) < 18 {
					g.explosion(m.x, m.y, 55, g.playerByIDLocked(m.owner))
					hit = true
					break
				}
			}
		}
		if hit || m.ttl <= 0 {
			continue
		}
		live = append(live, m)
	}
	g.missiles = live
}

// explosion destruye rocas/naves en un radio y suelta partículas.
func (g *Sim) explosion(x, y, radius float64, by *Player) {
	for i := len(g.rocks) - 1; i >= 0; i-- {
		if math.Hypot(x-g.rocks[i].x, y-g.rocks[i].y) < radius+g.rocks[i].r {
			g.hitRock(i)
		}
	}
	for i := len(g.enemies) - 1; i >= 0; i-- {
		if math.Hypot(x-g.enemies[i].x, y-g.enemies[i].y) < radius+g.enemies[i].r {
			g.damageEnemy(i, blastDamage)
		}
	}
	for k := 0; k < 24; k++ {
		a := rand.Float64() * 2 * math.Pi
		sp := 60 + rand.Float64()*180
		g.parts = append(g.parts, astParticle{
			x: x, y: y, vx: math.Cos(a) * sp, vy: math.Sin(a) * sp,
			ttl: 0.5 + rand.Float64()*0.4,
		})
	}
	// en PvP la explosión también alcanza a las naves (a la tuya incluida: es una
	// bomba)
	if g.pvp {
		for _, o := range g.players {
			if math.Hypot(x-o.ship.x, y-o.ship.y) < radius+11 {
				g.damagePlayer(o, shieldEnemyHitCost, by)
			}
		}
	}
	g.playSound("mineBoom", 90*time.Millisecond)
}

// ---------- minas ----------

func (g *Sim) dropMine(p *Player) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !p.ship.alive || g.over || p.minesLeft <= 0 {
		return
	}
	p.minesLeft--
	g.mines = append(g.mines, astMine{
		x: p.ship.x - math.Cos(p.ship.angle)*14,
		y: p.ship.y - math.Sin(p.ship.angle)*14,
		r: 9, arm: 1.2, life: 30, owner: p.ID,
	})
	g.playSound("mineDrop", 80*time.Millisecond)
}

func (g *Sim) updateMines(dt float64) {
	live := g.mines[:0]
	for _, m := range g.mines {
		m.life -= dt
		if g.holeSwallows(m.x, m.y) {
			continue // el agujero se la traga
		}
		if m.arm > 0 {
			m.arm -= dt
			m.beep -= dt
			if m.beep <= 0 {
				m.beep = 0.35
				g.playSound("mineArm", 60*time.Millisecond)
			}
			if m.arm <= 0 {
				m.arm = 0
				m.r = 11
			}
		} else {
			boom := false
			for _, p := range g.players {
				if p.ship.alive && math.Hypot(p.ship.x-m.x, p.ship.y-m.y) < m.r+9 {
					if p.shieldT <= 0 {
						g.killShip(p)
					}
					boom = true
					break
				}
			}
			if !boom {
				for i := range g.rocks {
					if math.Hypot(g.rocks[i].x-m.x, g.rocks[i].y-m.y) < m.r+g.rocks[i].r*0.6 {
						boom = true
						break
					}
				}
			}
			if !boom {
				for i := range g.enemies {
					if math.Hypot(g.enemies[i].x-m.x, g.enemies[i].y-m.y) < m.r+g.enemies[i].r+8 {
						boom = true
						break
					}
				}
			}
			if boom {
				g.explosion(m.x, m.y, 55, g.playerByIDLocked(m.owner))
				continue
			}
		}
		if m.life > 0 {
			live = append(live, m)
		}
	}
	g.mines = live
}

// ---------- nave enemiga ----------

// enemyHP es la resistencia de las naves enemigas: aguantan un disparo en el
// primer nivel y a partir del segundo van subiendo una resistencia por nivel.
func enemyHP(level int) int {
	if level < 2 {
		return 1
	}
	h := level
	if h > 8 {
		h = 8
	}
	return h
}

// enemyWaveCount: cada nivel salen el doble de naves enemigas (1, 2, 4, 8…).
func enemyWaveCount(level int) int {
	n := 1
	for i := 1; i < level && n < 8; i++ {
		n *= 2
	}
	return n
}

func (g *Sim) spawnEnemy(shape int) {
	var x, y float64
	switch rand.Intn(4) {
	case 0:
		x, y = 0, rand.Float64()*g.h
	case 1:
		x, y = g.w, rand.Float64()*g.h
	case 2:
		x, y = rand.Float64()*g.w, 0
	default:
		x, y = rand.Float64()*g.w, g.h
	}
	sp := 70 + rand.Float64()*40 + float64(g.level-1)*8
	a := math.Atan2(g.h/2-y, g.w/2-x) + (rand.Float64()-0.5)*0.5
	hp := enemyHP(g.level)
	g.enemies = append(g.enemies, astEnemy{
		x: x, y: y, vx: math.Cos(a) * sp, vy: math.Sin(a) * sp,
		angle: a, shootCd: 1.5, ttl: 60, r: 16, shape: shape,
		hp: hp, hpMax: hp,
	})
}

// spawnWave lanza la oleada del nivel con siluetas distintas.
func (g *Sim) spawnWave() {
	n := enemyWaveCount(g.level)
	base := rand.Intn(enemyShapeCount)
	for i := 0; i < n; i++ {
		g.spawnEnemy((base + i) % enemyShapeCount)
	}
	g.playSound("enemy", 0)
}

func (g *Sim) updateEnemies(dt float64) {
	live := g.enemies[:0]
	for i := range g.enemies {
		e := g.enemies[i]
		e.ttl -= dt
		if tp := g.nearestAlivePlayer(e.x, e.y); tp != nil {
			des := math.Atan2(tp.ship.y-e.y, tp.ship.x-e.x)
			cur := math.Atan2(e.vy, e.vx)
			diff := math.Mod(des-cur+3*math.Pi, 2*math.Pi) - math.Pi
			turn := 0.9 * dt
			if diff > turn {
				diff = turn
			} else if diff < -turn {
				diff = -turn
			}
			na := cur + diff
			sp := math.Hypot(e.vx, e.vy)
			e.vx, e.vy = math.Cos(na)*sp, math.Sin(na)*sp
			e.angle = na
		}
		// los enemigos son inmunes a los asteroides, pero el agujero los atrae
		if ax, ay := g.holeAccel(e.x, e.y); ax != 0 || ay != 0 {
			e.vx += ax * dt
			e.vy += ay * dt
		}
		e.x += e.vx * dt
		e.y += e.vy * dt
		wrap(&e.x, &e.y, g.w, g.h)
		e.shootCd -= dt
		if e.shootCd <= 0 {
			if tp := g.nearestAlivePlayer(e.x, e.y); tp != nil {
				e.shootCd = 1.6 + rand.Float64()*1.2
				a := math.Atan2(tp.ship.y-e.y, tp.ship.x-e.x) + (rand.Float64()-0.5)*0.3
				g.enemyShots = append(g.enemyShots, astEnemyBullet{
					x: e.x, y: e.y, vx: math.Cos(a) * 260, vy: math.Sin(a) * 260, ttl: 3,
				})
				g.playSound("enemyShot", 120*time.Millisecond)
			}
		}
		dead := e.ttl <= 0
		if tp := g.nearestAlivePlayerWithin(e.x, e.y, e.r+10); tp != nil {
			if tp.permShield > 0 {
				// escudo permanente: no la destruye, la REPELE (cuesta energía)
				g.spendShield(tp, shieldCost(e.r, 1))
				dx, dy := e.x-tp.ship.x, e.y-tp.ship.y
				d := math.Hypot(dx, dy)
				if d < 0.001 {
					dx, dy, d = 1, 0, 1
				}
				e.x = tp.ship.x + dx/d*(e.r+28)
				e.y = tp.ship.y + dy/d*(e.r+28)
				e.vx, e.vy = dx/d*220, dy/d*220
				g.playSound("shield", 120*time.Millisecond)
			} else if tp.shieldT > 0 {
				dead = true
				g.playSound("shield", 120*time.Millisecond)
			} else {
				g.killShip(tp)
			}
		}
		if !dead {
			live = append(live, e)
		} else {
			g.awardScore(100)
			for k := 0; k < 18; k++ {
				a := rand.Float64() * 2 * math.Pi
				sp := 40 + rand.Float64()*140
				g.parts = append(g.parts, astParticle{
					x: e.x, y: e.y, vx: math.Cos(a) * sp, vy: math.Sin(a) * sp,
					ttl: 0.4 + rand.Float64()*0.3,
				})
			}
		}
	}
	g.enemies = live
}

func (g *Sim) updateEnemyShots(dt float64) {
	for i := range g.enemyShots {
		s := &g.enemyShots[i]
		if s.homing {
			if tp := g.nearestAlivePlayer(s.x, s.y); tp != nil {
				des := math.Atan2(tp.ship.y-s.y, tp.ship.x-s.x)
				cur := math.Atan2(s.vy, s.vx)
				diff := math.Mod(des-cur+3*math.Pi, 2*math.Pi) - math.Pi
				turn := 2.2 * dt
				if diff > turn {
					diff = turn
				} else if diff < -turn {
					diff = -turn
				}
				na := cur + diff
				sp := math.Hypot(s.vx, s.vy)
				s.vx, s.vy = math.Cos(na)*sp, math.Sin(na)*sp
			}
		}
		if ax, ay := g.holeAccel(s.x, s.y); ax != 0 || ay != 0 {
			s.vx += ax * dt
			s.vy += ay * dt
		}
		s.x += s.vx * dt
		s.y += s.vy * dt
		s.ttl -= dt
		wrap(&s.x, &s.y, g.w, g.h)
		if g.holeSwallows(s.x, s.y) {
			s.ttl = 0
			continue
		}
		// estela de los misiles del jefe (como los del jugador)
		if s.homing {
			s.trail = append(s.trail, astTrailPt{s.x, s.y})
			if len(s.trail) > 34 {
				s.trail = s.trail[1:]
			}
		}
		if s.ttl > 0 {
			if tp := g.nearestAlivePlayerWithin(s.x, s.y, 10); tp != nil {
				if tp.permShield > 0 {
					g.spendShield(tp, 0.5) // un disparo enemigo gasta poco escudo
					g.playSound("shield", 120*time.Millisecond)
				} else if tp.shieldT <= 0 {
					g.killShip(tp)
				}
				s.ttl = 0
			}
		}
	}
	live := g.enemyShots[:0]
	for _, s := range g.enemyShots {
		if s.ttl > 0 {
			live = append(live, s)
		}
	}
	g.enemyShots = live
}

// damageEnemy aplica daño a una nave enemiga; si agota su resistencia, explota.
func (g *Sim) damageEnemy(i, amt int) {
	if i < 0 || i >= len(g.enemies) {
		return
	}
	e := &g.enemies[i]
	e.hp -= amt
	if e.hp <= 0 {
		g.destroyEnemy(i)
		return
	}
	// chispas del impacto (las naves duras aguantan varios golpes)
	for k := 0; k < 5; k++ {
		a := rand.Float64() * 2 * math.Pi
		sp := 40 + rand.Float64()*90
		g.parts = append(g.parts, astParticle{
			x: e.x, y: e.y, vx: math.Cos(a) * sp, vy: math.Sin(a) * sp,
			ttl: 0.2 + rand.Float64()*0.2,
		})
	}
	g.playSound("bangSmall", 40*time.Millisecond)
}

func (g *Sim) destroyEnemy(i int) {
	if i < 0 || i >= len(g.enemies) {
		return
	}
	e := g.enemies[i]
	g.awardScore(200)
	for k := 0; k < 24; k++ {
		a := rand.Float64() * 2 * math.Pi
		sp := 50 + rand.Float64()*160
		g.parts = append(g.parts, astParticle{
			x: e.x, y: e.y, vx: math.Cos(a) * sp, vy: math.Sin(a) * sp,
			ttl: 0.5 + rand.Float64()*0.4,
		})
	}
	g.playSound("bangBig", 70*time.Millisecond)
	g.enemies = append(g.enemies[:i], g.enemies[i+1:]...)
	g.enemiesKilled++
	// el compañero es el más raro: 1 de cada 25 naves y solo si no hay otro
	local := g.Local()
	if g.enemiesKilled%companionDropEvery == 0 && (local == nil || local.companion == nil) {
		g.spawnPowerupIfUseful(e.x, e.y, powerupCompanion)
		return
	}
	// 1 de cada 3 naves derribadas suelta el escudo permanente
	if g.enemiesKilled%3 == 0 {
		g.spawnPowerupIfUseful(e.x, e.y, powerupShield)
		return
	}
	// el resto: vida, cadencia, asistente y los nuevos (1 de cada 3, aprox.):
	// munición de misiles/minas y el cambio a láser
	roll := rand.Float64()
	switch {
	case roll < 0.05:
		g.spawnPowerupIfUseful(e.x, e.y, powerupLife)
	case roll < 0.30:
		g.spawnPowerupIfUseful(e.x, e.y, powerupRapid)
	case roll < 0.50:
		g.spawnPowerupIfUseful(e.x, e.y, powerupOrb)
	case roll < 0.60:
		g.spawnPowerupIfUseful(e.x, e.y, powerupMissiles)
	case roll < 0.70:
		g.spawnPowerupIfUseful(e.x, e.y, powerupMines)
	case roll < 0.80:
		// banda de armas: a partes iguales entre "el arma contraria a la que
		// llevamos" y el disparo doble (así el doble sale con la misma
		// frecuencia que el láser). Si ya llevamos el doble, sale el arma.
		contrary := powerupLaser
		if local != nil && local.laser {
			contrary = powerupGun
		}
		if local != nil && local.doubleShot {
			g.spawnPowerupIfUseful(e.x, e.y, contrary)
		} else if rand.Float64() < 0.5 {
			g.spawnPowerupIfUseful(e.x, e.y, contrary)
		} else {
			g.spawnPowerupIfUseful(e.x, e.y, powerupDouble)
		}
	}
}

// spawnPowerup suelta un power-up en (x,y). Los tipos están en las constantes
// powerup* (asistente, vida, escudo, cadencia, munición y compañero).
func (g *Sim) spawnPowerup(x, y float64, kind int) {
	g.powerups = append(g.powerups, astPowerup{
		x: x, y: y,
		vx: (rand.Float64() - 0.5) * 50, vy: (rand.Float64() - 0.5) * 50,
		ttl: powerupTTL, kind: kind,
	})
}

// ---------- power-ups y asistentes rotatorios ----------

func (g *Sim) updatePowerups(dt float64) {
	live := g.powerups[:0]
	for _, pu := range g.powerups {
		if ax, ay := g.holeAccel(pu.x, pu.y); ax != 0 || ay != 0 {
			pu.vx += ax * dt
			pu.vy += ay * dt
		}
		pu.x += pu.vx * dt
		pu.y += pu.vy * dt
		pu.ttl -= dt
		if g.holeSwallows(pu.x, pu.y) {
			continue
		}
		pu.spin += dt * 5
		wrap(&pu.x, &pu.y, g.w, g.h)
		// recoge el primero que lo toque: una nave tiene prioridad sobre los
		// compañeros, como antes
		var owner *Player
		for _, p := range g.players {
			if p.ship.alive && math.Hypot(p.ship.x-pu.x, p.ship.y-pu.y) < 28 {
				owner = p
				break
			}
		}
		if owner == nil {
			for _, p := range g.players {
				if p.companion != nil && math.Hypot(p.companion.x-pu.x, p.companion.y-pu.y) < 28 {
					owner = p // el compañero también recoge power-ups
					break
				}
			}
		}
		if owner != nil && g.applyPowerupLocked(owner, pu.kind) {
			continue
		}
		if pu.ttl <= 0 {
			continue
		}
		live = append(live, pu)
	}
	g.powerups = live
}

// powerupUseful indica si un power-up tendría efecto al jugador p ahora mismo
// (si el escudo, la cadencia o los asistentes ya están al máximo, no sirve).
func (g *Sim) powerupUseful(p *Player, kind int) bool {
	switch kind {
	case powerupShield:
		return p.permShield < permShieldCap
	case powerupRapid:
		return p.rapidFire < maxRapidFire
	case powerupOrb:
		return len(p.orbs) < orbMax
	case powerupLaser:
		return !p.laser
	case powerupGun:
		return p.laser
	case powerupDouble:
		return !p.doubleShot
	}
	return true
}

// applyPowerupLocked aplica el efecto de un power-up sobre el jugador p.
// Devuelve false si ahora mismo no sirve: en ese caso el power-up sigue
// flotando. Con el lock tomado.
func (g *Sim) applyPowerupLocked(p *Player, kind int) bool {
	if !g.powerupUseful(p, kind) {
		return false
	}
	switch kind {
	case powerupLife:
		p.lives++
		g.playSound("extra", 0)
	case powerupShield:
		p.permShield += permShieldPer
		if p.permShield > permShieldCap {
			p.permShield = permShieldCap
		}
		g.playSound("shield", 0)
	case powerupRapid:
		p.rapidFire++
		g.playSound("extra", 0)
	case powerupOrb:
		dir := 1.0
		if len(p.orbs)%2 == 1 {
			dir = -1.0
		}
		p.orbs = append(p.orbs, astOrb{
			ang:   float64(len(p.orbs)) * 2 * math.Pi / orbMax,
			dir:   dir,
			hp:    orbHP,
			hpMax: orbHP,
		})
		g.playSound("extra", 0)
	case powerupMissiles:
		p.missilesLeft += missilePowerupAmmo
		g.playSound("extra", 0)
	case powerupMines:
		p.minesLeft += ammoPowerup
		g.playSound("extra", 0)
	case powerupLaser:
		p.laser = true
		g.playSound("laser", 0)
	case powerupGun:
		p.laser = false
		g.playSound("shot", 0)
	case powerupDouble:
		p.doubleShot = true
		g.playSound("extra", 0)
	case powerupCompanion:
		if p.companion != nil {
			p.companion.hp = companionHP // "reparación" si ya está en juego
			p.companion.invuln = companionHitInvuln
		} else {
			g.spawnCompanionLocked(p)
		}
		g.playSound("extra", 0)
	}
	return true
}

// ---------- compañero (nave aliada rosa) ----------

// spawnCompanionLocked pone en juego al compañero junto al jugador p.
func (g *Sim) spawnCompanionLocked(p *Player) {
	x := p.ship.x + 70
	if g.w > 0 {
		x = math.Mod(x, g.w)
	}
	p.companion = &astCompanion{x: x, y: p.ship.y, hp: companionHP, invuln: 1.5}
}

// damageCompanionLocked resta un impacto al compañero del jugador p (muere si
// agota su vida).
func (g *Sim) damageCompanionLocked(p *Player) {
	c := p.companion
	if c == nil || c.invuln > 0 {
		return
	}
	c.hp--
	if c.hp > 0 {
		c.invuln = companionHitInvuln
		g.playSound("bangSmall", 80*time.Millisecond)
		return
	}
	for k := 0; k < 26; k++ {
		a := rand.Float64() * 2 * math.Pi
		sp := 50 + rand.Float64()*160
		g.parts = append(g.parts, astParticle{
			x: c.x, y: c.y, vx: math.Cos(a) * sp, vy: math.Sin(a) * sp,
			ttl: 0.5 + rand.Float64()*0.4,
		})
	}
	g.playSound("bangBig", 70*time.Millisecond)
	p.companion = nil
}

func (g *Sim) updateCompanion(p *Player, dt float64) {
	c := p.companion
	if c == nil {
		return
	}
	if c.invuln > 0 {
		c.invuln -= dt
	}
	if c.fireCd > 0 {
		c.fireCd -= dt
	}
	// el agujero negro lo atrae y se lo traga
	if ax, ay := g.holeAccel(c.x, c.y); ax != 0 || ay != 0 {
		c.vx += ax * dt
		c.vy += ay * dt
	}
	if g.holeSwallows(c.x, c.y) {
		p.companion = nil
		g.playSound("bangBig", 70*time.Millisecond)
		return
	}

	// elige objetivo cada cierto tiempo (reacciona tarde y se queda con él):
	// si tiene un power-up a tiro va a por él; si no, al enemigo/roca más cercano
	c.think -= dt
	if c.think <= 0 {
		c.think = companionThink
		c.chasing = false
		if px, py, ok := g.nearestPowerup(p, c.x, c.y, companionPickup); ok {
			c.tx, c.ty, c.hasTarget, c.chasing = px, py, true, true
		} else {
			c.tx, c.ty, c.hasTarget = g.nearestTarget(c.x, c.y)
		}
	}

	desired, thrust := c.angle, true
	switch {
	// si el agujero está encima, sale por patas
	case g.hole != nil && math.Hypot(g.hole.x-c.x, g.hole.y-c.y) < g.hole.r+170:
		desired = math.Atan2(c.y-g.hole.y, c.x-g.hole.x)
	case c.hasTarget:
		d := math.Hypot(c.tx-c.x, c.ty-c.y)
		desired = math.Atan2(c.ty-c.y, c.tx-c.x)
		// a por un power-up no se detiene hasta tocarlo; a por una nave se queda
		// a distancia de tiro
		if c.chasing {
			thrust = d > 4
		} else {
			thrust = d > companionStandoff
		}
	case p.ship.alive:
		desired = math.Atan2(p.ship.y-c.y, p.ship.x-c.x)
		thrust = math.Hypot(p.ship.x-c.x, p.ship.y-c.y) > companionOrbit
	}
	diff := math.Mod(desired-c.angle+3*math.Pi, 2*math.Pi) - math.Pi
	turn := companionTurn * dt
	if diff > turn {
		diff = turn
	} else if diff < -turn {
		diff = -turn
	}
	c.angle += diff
	if thrust {
		c.vx += math.Cos(c.angle) * companionThrust * dt
		c.vy += math.Sin(c.angle) * companionThrust * dt
	}
	c.vx *= 0.99
	c.vy *= 0.99
	if sp := math.Hypot(c.vx, c.vy); sp > companionSpeedMax {
		c.vx, c.vy = c.vx/sp*companionSpeedMax, c.vy/sp*companionSpeedMax
	}
	c.x += c.vx * dt
	c.y += c.vy * dt
	wrap(&c.x, &c.y, g.w, g.h)

	// dispara cuando el objetivo está a tiro y bien encarado (nunca a un
	// power-up). Con dispersión: no es un gran tirador
	if c.fireCd <= 0 && c.hasTarget && !c.chasing &&
		math.Hypot(c.tx-c.x, c.ty-c.y) < companionRange {
		aim := math.Atan2(c.ty-c.y, c.tx-c.x)
		err := math.Mod(aim-c.angle+3*math.Pi, 2*math.Pi) - math.Pi
		if math.Abs(err) < companionAimTol {
			c.fireCd = companionFireCd
			a := c.angle + (rand.Float64()-0.5)*companionSpread
			g.bullets = append(g.bullets, astBullet{
				x: c.x + math.Cos(a)*14, y: c.y + math.Sin(a)*14,
				vx: math.Cos(a) * bulletSpeed, vy: math.Sin(a) * bulletSpeed,
				ttl: bulletTTL, pink: true,
			})
			g.playSound("shot", 60*time.Millisecond)
		}
	}

	// puede morir: le hacen daño las rocas, las naves, el jefe y sus disparos
	if c.invuln <= 0 {
		hit := false
		for i := range g.rocks {
			if math.Hypot(c.x-g.rocks[i].x, c.y-g.rocks[i].y) < g.rocks[i].r+8 {
				hit = true
				break
			}
		}
		if !hit {
			for i := range g.enemies {
				if math.Hypot(c.x-g.enemies[i].x, c.y-g.enemies[i].y) < g.enemies[i].r+10 {
					hit = true
					break
				}
			}
		}
		if !hit && g.boss != nil && math.Hypot(c.x-g.boss.x, c.y-g.boss.y) < g.boss.r+12 {
			hit = true
		}
		if !hit {
			for i := range g.enemyShots {
				if g.enemyShots[i].ttl > 0 && math.Hypot(c.x-g.enemyShots[i].x, c.y-g.enemyShots[i].y) < 10 {
					g.enemyShots[i].ttl = 0
					hit = true
					break
				}
			}
		}
		if hit {
			g.damageCompanionLocked(p)
			if c = p.companion; c != nil {
				c.vx, c.vy = -c.vx, -c.vy // rebote para salir del peligro
			}
		}
	}
}

func (g *Sim) updateOrbs(p *Player, dt float64) {
	if !p.ship.alive {
		return
	}
	live := p.orbs[:0]
	for _, o := range p.orbs {
		o.ang += o.dir * orbRotSpeed * dt
		ox := p.ship.x + math.Cos(o.ang)*orbRadius
		oy := p.ship.y + math.Sin(o.ang)*orbRadius
		// intercepción: bala enemiga o asteroide
		hit := false
		for j := range g.enemyShots {
			if g.enemyShots[j].ttl > 0 && math.Hypot(g.enemyShots[j].x-ox, g.enemyShots[j].y-oy) < 11 {
				g.enemyShots[j].ttl = 0
				hit = true
				break
			}
		}
		if !hit {
			for j := len(g.rocks) - 1; j >= 0; j-- {
				if math.Hypot(g.rocks[j].x-ox, g.rocks[j].y-oy) < g.rocks[j].r*0.7+6 {
					g.destroyRockComplete(j)
					hit = true
					break
				}
			}
		}
		if hit {
			o.hp--
			for k := 0; k < 16; k++ {
				a := rand.Float64() * 2 * math.Pi
				sp := 40 + rand.Float64()*140
				g.parts = append(g.parts, astParticle{
					x: ox, y: oy, vx: math.Cos(a) * sp, vy: math.Sin(a) * sp,
					ttl: 0.4 + rand.Float64()*0.3,
				})
			}
			g.playSound("bangSmall", 80*time.Millisecond)
			if o.hp <= 0 {
				continue // agotado: el asistente se destruye
			}
			live = append(live, o) // todavía aguanta
			continue
		}
		if o.shootCd > 0 {
			o.shootCd -= dt
		}
		if o.shotID == 0 && o.shootCd <= 0 {
			if tx, ty, _, _, ok := g.nearestWithin(ox, oy, orbShootRange); ok {
				a := math.Atan2(ty-oy, tx-ox)
				g.nextShotID++
				o.shotID = g.nextShotID
				o.shootCd = 0.4
				p.orbShots = append(p.orbShots, astOrbShot{
					x: ox, y: oy, vx: math.Cos(a) * orbShotSpeed, vy: math.Sin(a) * orbShotSpeed,
					ttl: 1.6, id: o.shotID,
				})
				g.playSound("shot", 70*time.Millisecond)
			}
		}
		live = append(live, o)
	}
	p.orbs = live
}

func (g *Sim) updateOrbShots(p *Player, dt float64) {
	live := p.orbShots[:0]
	alive := map[int]bool{}
	for _, s := range p.orbShots {
		if ax, ay := g.holeAccel(s.x, s.y); ax != 0 || ay != 0 {
			s.vx += ax * dt
			s.vy += ay * dt
		}
		s.x += s.vx * dt
		s.y += s.vy * dt
		s.ttl -= dt
		wrap(&s.x, &s.y, g.w, g.h)
		if g.holeSwallows(s.x, s.y) {
			continue
		}
		hit := false
		for j := len(g.rocks) - 1; j >= 0; j-- {
			if math.Hypot(s.x-g.rocks[j].x, s.y-g.rocks[j].y) < g.rocks[j].r {
				g.hitRock(j)
				hit = true
				break
			}
		}
		if !hit {
			for j := range g.enemies {
				if math.Hypot(s.x-g.enemies[j].x, s.y-g.enemies[j].y) < g.enemies[j].r+2 {
					g.damageEnemy(j, 1)
					hit = true
					break
				}
			}
		}
		if !hit && g.boss != nil && math.Hypot(s.x-g.boss.x, s.y-g.boss.y) < g.boss.r {
			g.damageBoss(4)
			hit = true
		}
		if !hit {
			for j := range g.enemyShots {
				if g.enemyShots[j].ttl > 0 && math.Hypot(s.x-g.enemyShots[j].x, s.y-g.enemyShots[j].y) < 9 {
					g.enemyShots[j].ttl = 0
					hit = true
					break
				}
			}
		}
		if hit || s.ttl <= 0 {
			continue
		}
		live = append(live, s)
		alive[s.id] = true
	}
	p.orbShots = live
	// liberar asistentes cuyo disparo ya no está en vuelo
	for i := range p.orbs {
		if p.orbs[i].shotID != 0 && !alive[p.orbs[i].shotID] {
			p.orbs[i].shotID = 0
		}
	}
}

// ---------- jefe (nave grande con barra de energía) ----------

func (g *Sim) spawnBoss() {
	g.bossCount++
	n := g.bossCount
	r := 34.0
	w := g.w
	if w == 0 {
		w = 800
	}
	b := &astBoss{
		x: w / 2, y: -50,
		vx: (rand.Float64() - 0.5) * 50, vy: 45,
		angle:        math.Pi / 2,
		maxEnergy:    bossEnergyBase + float64(n-1)*bossEnergyPer,
		r:            r,
		missilesLeft: bossMissileStock(g.level), // 5 en el nivel 2, 10 en el 3, 20 en el 4…
		deployLeft:   n,                         // 1 en la 1ª, 2 en la 2ª…
		missileCd:    2.5, deployCd: 2.0,
		railCd: 5.0,
	}
	b.energy = b.maxEnergy
	g.boss = b
	g.playSound("boss", 0)
}

// bossMissileStock son los misiles de la nodriza según el nivel: 0 en el 2
// (nivel de entrada, sin misiles), 10 en el 3, 20 en el 4, 30 en el 5… (+10).
func bossMissileStock(level int) int {
	if level <= 2 {
		return 0
	}
	return (level - 2) * 10
}

func (g *Sim) updateBoss(dt float64) {
	b := g.boss
	if b == nil {
		return
	}
	w, h := g.w, g.h
	if b.hitFlash > 0 {
		b.hitFlash -= dt
	}
	if tp := g.nearestAlivePlayer(b.x, b.y); tp != nil {
		des := math.Atan2(tp.ship.y-b.y, tp.ship.x-b.x)
		cur := math.Atan2(b.vy, b.vx)
		diff := math.Mod(des-cur+3*math.Pi, 2*math.Pi) - math.Pi
		turn := 0.5 * dt
		if diff > turn {
			diff = turn
		} else if diff < -turn {
			diff = -turn
		}
		na := cur + diff
		sp := math.Hypot(b.vx, b.vy) + 6*dt
		if sp > 95 {
			sp = 95
		}
		b.vx, b.vy = math.Cos(na)*sp, math.Sin(na)*sp
		b.angle = na
	}
	b.x += b.vx * dt
	b.y += b.vy * dt
	wrap(&b.x, &b.y, w, h)

	// misiles teledirigidos al jugador
	b.missileCd -= dt
	if b.missileCd <= 0 && b.missilesLeft > 0 {
		if tp := g.nearestAlivePlayer(b.x, b.y); tp != nil {
			b.missileCd = 2.2
			b.missilesLeft--
			a := math.Atan2(tp.ship.y-b.y, tp.ship.x-b.x)
			g.enemyShots = append(g.enemyShots, astEnemyBullet{
				x: b.x, y: b.y, vx: math.Cos(a) * 200, vy: math.Sin(a) * 200, ttl: 6, homing: true,
			})
			g.playSound("enemyShot", 140*time.Millisecond)
		}
	}
	// despliega naves pequeñas
	b.deployCd -= dt
	if b.deployCd <= 0 && b.deployLeft > 0 {
		b.deployCd = 2.4
		b.deployLeft--
		g.spawnEnemy(rand.Intn(enemyShapeCount))
		if n := len(g.enemies); n > 0 {
			g.enemies[n-1].x = b.x + (rand.Float64()-0.5)*40
			g.enemies[n-1].y = b.y + (rand.Float64()-0.5)*40
		}
	}
	// railgun de la nodriza (desde el nivel 3): avisa y dispara un haz blanco/rojo
	b.railCd -= dt
	if b.railWarn > 0 {
		b.railWarn -= dt
		if tp := g.nearestAlivePlayer(b.x, b.y); tp != nil {
			b.railAngle = math.Atan2(tp.ship.y-b.y, tp.ship.x-b.x) // sigue al jugador
		}
		if b.railWarn <= 0 {
			g.bossFireRail(b)
		}
	} else if b.railCd <= 0 && g.level >= 3 {
		if tp := g.nearestAlivePlayer(b.x, b.y); tp != nil {
			b.railCd = 6.5
			b.railWarn = 0.8
			b.railAngle = math.Atan2(tp.ship.y-b.y, tp.ship.x-b.x)
			g.playSound("railWarn", 0)
		}
	}
	// el jefe es inmune a los asteroides: los atraviesa sin dañarlos ni
	// perder energía (sus disparos tampoco los tocan)
	if b.energy <= 0 {
		g.destroyBoss()
		return
	}
	if tp := g.nearestAlivePlayerWithin(b.x, b.y, b.r+12); tp != nil {
		if tp.permShield > 0 {
			g.spendShield(tp, shieldCost(b.r, 1))
			dx, dy := b.x-tp.ship.x, b.y-tp.ship.y
			d := math.Hypot(dx, dy)
			if d < 0.001 {
				dx, dy, d = 1, 0, 1
			}
			b.x = tp.ship.x + dx/d*(b.r+40)
			b.y = tp.ship.y + dy/d*(b.r+40)
			b.vx, b.vy = dx/d*140, dy/d*140
			g.playSound("shield", 120*time.Millisecond)
		} else if tp.shieldT > 0 {
			b.energy -= 40
			g.playSound("shield", 120*time.Millisecond)
		} else {
			g.killShip(tp)
		}
	}
}

// bossFireRail dispara el haz cónico blanco/rojo de la nodriza hacia el jugador.
func (g *Sim) bossFireRail(b *astBoss) {
	ang := b.railAngle
	dx, dy := math.Cos(ang), math.Sin(ang)
	length := rayToEdge(b.x, b.y, ang, g.w, g.h)
	base := 8.0
	tip := base + 2*length*math.Tan(0.20) // cono ancho (~11.5° de semiángulo)
	g.bossBeams = append(g.bossBeams, astBossBeam{x: b.x, y: b.y, angle: ang, base: base, tip: tip, ttl: 0.32})
	// el haz puede pillar a varios jugadores a la vez
	for _, p := range g.players {
		s := p.ship
		if !s.alive {
			continue
		}
		d := (s.x-b.x)*dx + (s.y-b.y)*dy
		if d > 0 && d < length {
			half := (base + (tip-base)*(d/length)) / 2
			if distToRay(b.x, b.y, dx, dy, length, s.x, s.y) < half+9 {
				if p.permShield > 0 {
					g.spendShield(p, 1.0)
					g.playSound("shield", 120*time.Millisecond)
				} else if p.shieldT <= 0 {
					g.killShip(p)
				}
			}
		}
	}
	g.playSound("boss", 0)
}

func (g *Sim) updateBossBeams(dt float64) {
	live := g.bossBeams[:0]
	for _, b := range g.bossBeams {
		b.ttl -= dt
		if b.ttl > 0 {
			live = append(live, b)
		}
	}
	g.bossBeams = live
}

// playSound reproduce un sonido respetando el mute (tecla S). En el servidor no
// hay reproductor (OnSound == nil) y no pasa nada.
func (g *Sim) playSound(name string, gap time.Duration) {
	if g.muted.Load() || g.OnSound == nil {
		return
	}
	g.OnSound(name, gap)
}

// nearestRockIdx devuelve el índice y la posición de la roca más cercana.
func (g *Sim) nearestRockIdx(x, y, r float64) (int, float64, float64, bool) {
	best := r
	idx := -1
	var bx, by float64
	for i := range g.rocks {
		if d := math.Hypot(g.rocks[i].x-x, g.rocks[i].y-y); d < best {
			best, idx, bx, by = d, i, g.rocks[i].x, g.rocks[i].y
		}
	}
	return idx, bx, by, idx >= 0
}

// damageBoss resta energía al jefe (lo destruye si llega a 0).
func (g *Sim) damageBoss(dmg float64) {
	if g.boss == nil {
		return
	}
	g.boss.energy -= dmg
	g.boss.hitFlash = 0.2
	if g.boss.energy <= 0 {
		g.destroyBoss()
	}
}

func (g *Sim) destroyBoss() {
	b := g.boss
	if b == nil {
		return
	}
	g.awardScore(1000)
	for k := 0; k < 70; k++ {
		a := rand.Float64() * 2 * math.Pi
		sp := 60 + rand.Float64()*220
		g.parts = append(g.parts, astParticle{
			x: b.x, y: b.y, vx: math.Cos(a) * sp, vy: math.Sin(a) * sp,
			ttl: 0.6 + rand.Float64()*0.6,
		})
	}
	// cada jefe (1º, 2º, 3º…) revienta con su propio sonido
	n := g.bossCount
	if n < 1 {
		n = 1
	}
	g.playSound(fmt.Sprintf("bossDie%d", (n-1)%bossDieSounds+1), 0)
	g.boss = nil
}

// startHyper lanza el efecto hiperespacio (al pasar de nivel) para el jugador p.
func (g *Sim) startHyper(p *Player) {
	p.hyper = hyperTime
	p.hyperStars = p.hyperStars[:0]
	for i := 0; i < 130; i++ {
		p.hyperStars = append(p.hyperStars, hyperStar{
			ang: rand.Float64() * 2 * math.Pi,
			rad: 6 + rand.Float64()*24,
			spd: 40 + rand.Float64()*80, // arranca despacio y va acelerando
		})
	}
}

// ---------- railgun ----------

func (g *Sim) activateShield(p *Player) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !p.ship.alive || g.over || p.shieldT > 0 || p.shieldCd > 0 {
		return
	}
	p.shieldT = shieldOnTime
	g.playSound("shield", 140*time.Millisecond)
}

// fireRail empieza a cargar el railgun mientras se mantiene la R.
func (g *Sim) fireRail(p *Player) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !p.ship.alive || g.over || g.paused {
		return
	}
	if p.railCharging || p.railCharge < p.railRechargeTotal {
		return
	}
	p.railCharging = true
	p.railHold = 0
	p.chargeBeep = 0
	p.railAngle = p.ship.angle
	g.playSound("railWarn", 0)
}

// releaseRail dispara el haz cargado (al soltar R). La fuerza depende del tiempo
// que se mantuvo la R, y la recarga del próximo disparo también.
func (g *Sim) releaseRail(p *Player) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !p.railCharging {
		return
	}
	p.railCharging = false
	if !p.ship.alive || g.over || g.paused {
		p.railHold = 0 // se cancela sin gastar recarga
		return
	}
	frac := p.railHold / railHoldMax
	p.railHold = 0
	baseW := 4 + frac*30
	halfAng := 0.09 + 0.19*frac // semiángulo ~5°..16°: cono bien abierto
	g.railSweep(p, baseW, halfAng, 1+frac*99, 60+frac*600)
	p.railFlash = 0.3
	p.railFlashBase = baseW
	p.railFlashHalf = halfAng
	// recarga proporcional a la fuerza del disparo (30..120 s)
	p.railCharge = 0
	p.railRechargeTotal = railRechargeMin + frac*(railRechargeMax-railRechargeMin)
	g.playSound("railgun", 0)
}

func (g *Sim) updateRail(p *Player, dt float64) {
	if p.railCharging {
		if !p.ship.alive {
			p.railCharging = false
			p.railHold = 0
			return
		}
		p.railHold += dt
		if p.railHold > railHoldMax {
			p.railHold = railHoldMax
		}
		p.railAngle = p.ship.angle
		// pitido que sube de tono mientras carga
		p.chargeBeep -= dt
		if p.chargeBeep <= 0 {
			p.chargeBeep = 0.5
			idx := int(p.railHold / railHoldMax * 9.999)
			if idx < 0 {
				idx = 0
			}
			if idx > 9 {
				idx = 9
			}
			g.playSound(fmt.Sprintf("charge%d", idx), 0)
		}
		return
	}
	if p.railFlash > 0 {
		p.railFlash -= dt
	}
	// recarga del último disparo
	if p.railCharge < p.railRechargeTotal {
		p.railCharge += dt
		if p.railCharge >= p.railRechargeTotal {
			p.railCharge = p.railRechargeTotal
			g.playSound("charge9", 0)
		}
	}
}

// railSweep dispara un haz cónico: grosor baseW en la nave y un semiángulo
// halfAng (el cono se ensancha con la distancia). rockDmg es el daño en
// "disparos" a las rocas; bossDmg es energía al jefe.
func (g *Sim) railSweep(p *Player, baseW, halfAng, rockDmg, bossDmg float64) {
	s := p.ship
	ang := p.railAngle
	dx, dy := math.Cos(ang), math.Sin(ang)
	length := rayToEdge(s.x, s.y, ang, g.w, g.h)
	if length < 1 {
		return
	}
	tipW := baseW + 2*length*math.Tan(halfAng)
	coneHalf := func(tx, ty float64) float64 {
		d := (tx-s.x)*dx + (ty-s.y)*dy
		if d < 0 {
			d = 0
		}
		if d > length {
			d = length
		}
		return (baseW + (tipW-baseW)*(d/length)) / 2
	}
	for i := len(g.rocks) - 1; i >= 0; i-- {
		r := g.rocks[i]
		if distToRay(s.x, s.y, dx, dy, length, r.x, r.y) < r.r+coneHalf(r.x, r.y) {
			g.rockDamage(i, int(rockDmg))
		}
	}
	for i := len(g.enemies) - 1; i >= 0; i-- {
		e := g.enemies[i]
		if distToRay(s.x, s.y, dx, dy, length, e.x, e.y) < e.r+coneHalf(e.x, e.y) {
			g.destroyEnemy(i)
		}
	}
	if g.boss != nil && distToRay(s.x, s.y, dx, dy, length, g.boss.x, g.boss.y) < g.boss.r+coneHalf(g.boss.x, g.boss.y) {
		g.damageBoss(bossDmg)
	}
	// en PvP el haz también barre a los demás jugadores; si alguno lleva el
	// escudo reflectante activo, se lo devuelve al que disparó
	if g.pvp {
		for _, o := range g.players {
			if o.ID == p.ID || !o.ship.alive {
				continue
			}
			if distToRay(s.x, s.y, dx, dy, length, o.ship.x, o.ship.y) < 13+coneHalf(o.ship.x, o.ship.y) {
				if g.reflectiveShield(o) {
					g.reflectRailLocked(o, p)
					continue
				}
				g.damagePlayer(o, shieldEnemyHitCost, p)
			}
		}
	}
}

// ---------- rayo letal (blanco, ramificado) ----------

func (g *Sim) fireLethalRay(p *Player) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !p.ship.alive || g.over || p.lethalCd > 0 {
		return
	}
	p.lethalCd = lethalCooldown
	s := p.ship
	if !g.chainRay(s.x, s.y, 1) {
		// sin objetivo: rayo blanco recto corto (sin ramificar)
		g.chains = append(g.chains, astChain{
			x1: s.x, y1: s.y,
			x2: s.x + math.Cos(s.angle)*220, y2: s.y + math.Sin(s.angle)*220,
			ttl: 0.3, level: 1,
		})
	}
	g.playSound("lethal", 0)
}

// chainRay lanza un rayo desde (x0,y0) al objetivo más cercano dentro del radio
// del nivel, lo destruye y, si no ha llegado al máximo, se ramifica en 2 rayos
// con la mitad del radio. Devuelve true si impactó en algo.
func (g *Sim) chainRay(x0, y0 float64, level int) bool {
	if level > lethalMaxLevel {
		return false
	}
	r := lethalFirstRange
	if level > 1 {
		r = lethalBaseRadius
		for i := 3; i <= level; i++ {
			r /= 2
		}
	}
	// El rayo letal SOLO daña asteroides (no naves ni jefe): 3 disparos por impacto.
	idx, tx, ty, ok := g.nearestRockIdx(x0, y0, r)
	if !ok {
		return false
	}
	g.chains = append(g.chains, astChain{x1: x0, y1: y0, x2: tx, y2: ty, ttl: 0.35, level: level})
	g.rockDamage(idx, 3)
	if level < lethalMaxLevel {
		for k := 0; k < 2; k++ {
			g.chainRay(tx, ty, level+1)
		}
	}
	return true
}

// nearestWithin devuelve el objetivo más cercano a (x,y) dentro de r.
// kind: 0 = roca, 1 = nave enemiga, 2 = jefe.
func (g *Sim) nearestWithin(x, y, r float64) (tx, ty float64, kind, idx int, ok bool) {
	best := r
	for i := range g.rocks {
		if d := math.Hypot(g.rocks[i].x-x, g.rocks[i].y-y); d < best {
			best, tx, ty, idx, kind, ok = d, g.rocks[i].x, g.rocks[i].y, i, 0, true
		}
	}
	for i := range g.enemies {
		if d := math.Hypot(g.enemies[i].x-x, g.enemies[i].y-y); d < best {
			best, tx, ty, idx, kind, ok = d, g.enemies[i].x, g.enemies[i].y, i, 1, true
		}
	}
	if g.boss != nil {
		if d := math.Hypot(g.boss.x-x, g.boss.y-y); d < best {
			best, tx, ty, idx, kind, ok = d, g.boss.x, g.boss.y, -1, 2, true
		}
	}
	return
}

func (g *Sim) updateChains(dt float64) {
	live := g.chains[:0]
	for _, c := range g.chains {
		c.ttl -= dt
		if c.ttl > 0 {
			live = append(live, c)
		}
	}
	g.chains = live
}

// distToRay calcula la distancia de un punto al segmento (origen + dirección).
func distToRay(x, y, dx, dy, length, px, py float64) float64 {
	t := (px-x)*dx + (py-y)*dy
	if t < 0 {
		t = 0
	} else if t > length {
		t = length
	}
	return math.Hypot(px-(x+dx*t), py-(y+dy*t))
}

// destroyRockComplete elimina una roca por completo (sin partirla). Lo usa el
// railgun, que "destruye todo lo que pilla".
func (g *Sim) destroyRockComplete(i int) {
	if i < 0 || i >= len(g.rocks) {
		return
	}
	r := g.rocks[i]
	switch r.size {
	case 3:
		g.awardScore(20)
	case 2:
		g.awardScore(50)
	default:
		g.awardScore(100)
	}
	for k := 0; k < 10+r.size*4; k++ {
		a := rand.Float64() * 2 * math.Pi
		sp := 40 + rand.Float64()*130
		g.parts = append(g.parts, astParticle{
			x: r.x, y: r.y, vx: math.Cos(a) * sp, vy: math.Sin(a) * sp,
			ttl: 0.4 + rand.Float64()*0.4,
		})
	}
	g.playSound("bangBig", 60*time.Millisecond)
	g.rocks = append(g.rocks[:i], g.rocks[i+1:]...)
}

// destroyRock parte la roca en dos (o la elimina si es pequeña).
func (g *Sim) destroyRock(i int) {
	r := g.rocks[i]
	// si la estructura escondía un power-up, sale al romperla
	if r.powerup >= 0 {
		g.spawnPowerup(r.x, r.y, r.powerup)
	}
	if r.hpMax > 1 {
		g.awardScore((r.hpMax - 1) * 30) // bonus por romper una roca dura
	}
	switch r.size {
	case 3:
		g.awardScore(20)
		g.playSound("bangBig", 70*time.Millisecond)
	case 2:
		g.awardScore(50)
		g.playSound("bangMid", 60*time.Millisecond)
	default:
		g.awardScore(100)
		g.playSound("bangSmall", 50*time.Millisecond)
	}
	// partículas
	for k := 0; k < 10+r.size*4; k++ {
		a := rand.Float64() * 2 * math.Pi
		sp := 40 + rand.Float64()*130
		g.parts = append(g.parts, astParticle{
			x: r.x, y: r.y,
			vx: math.Cos(a) * sp, vy: math.Sin(a) * sp,
			ttl: 0.4 + rand.Float64()*0.4,
		})
	}
	// dividir
	if r.fixed {
		// las estructuras del campo fijo se rompen del todo (no se parten en
		// trozos): así se van abriendo túneles. Hay que quitarla de la lista.
		g.rocks = append(g.rocks[:i], g.rocks[i+1:]...)
		return
	}
	if r.size > 1 {
		for k := 0; k < 2; k++ {
			nr := g.makeRock(g.w, g.h, r.size-1)
			// los trocitos mantienen el grosor (y dureza) del padre
			nr.hpMax = r.hpMax
			if r.hpMax > 1 {
				nr.hp = 1 + 3*(r.hpMax-1)
			}
			nr.x, nr.y = r.x, r.y
			sp := 50 + rand.Float64()*90
			if rand.Float64() < 0.45 {
				sp += 60 + rand.Float64()*100
			}
			sp += float64(g.level-1) * 6
			a := rand.Float64() * 2 * math.Pi
			nr.vx, nr.vy = math.Cos(a)*sp, math.Sin(a)*sp
			g.rocks = append(g.rocks, nr)
		}
	}
	g.rocks = append(g.rocks[:i], g.rocks[i+1:]...)
}

func (g *Sim) killShip(p *Player) {
	g.flash = 1
	// animación de muerte: se guarda dónde cayó (la nave reaparece en el centro,
	// así que el sitio hay que recordarlo) y saltan trozos.
	p.deathT = shipDeathAnim
	p.deathX, p.deathY = p.ship.x, p.ship.y
	for k := 0; k < 28; k++ {
		a := rand.Float64() * 2 * math.Pi
		sp := 60 + rand.Float64()*210
		g.parts = append(g.parts, astParticle{
			x: p.ship.x, y: p.ship.y,
			vx: math.Cos(a) * sp, vy: math.Sin(a) * sp,
			ttl: 0.8 + rand.Float64()*0.8, // los trozos duran más que la onda
		})
	}
	p.lives--
	p.deaths++
	p.ship.alive = false
	g.playSound("shipDie", 0) // morir tiene que sonar (también en la última vida)
	if p.score > g.Best {
		g.Best = p.score
		g.BestName = p.Name
		g.BestLevel = g.level
		if g.OnBest != nil {
			g.OnBest(g.Best, g.BestLevel, g.BestName)
		}
	}
	if p.lives < 0 {
		// solo es fin de partida cuando no le queda vida a NINGÚN jugador
		if g.matchOver() {
			g.over = true
			g.playSound("gameover", 0)
		}
		return
	}
	// reaparición: se pierde el escudo, pero se conservan las armas y los
	// power-ups que ya teníamos (misiles, minas, cadencia, láser y asistentes).
	// El sitio se elige AHORA y la nave tarda un par de segundos en recomponerse:
	// mientras se ve el efecto de teletransporte ahí.
	p.shieldT, p.shieldCd = 0, 0
	p.permShield = 0
	// OJO: aquí NO se vacían g.powerups. Los power-ups de la arena son de todos
	// (flotan a la vista de cualquiera), así que borrarlos al morir uno hacía que
	// desaparecieran enseguida: con bots muriendo todo el rato, nunca duraban los
	// ~36 s que se supone que duran.
	sx, sy := g.spawnSpotLocked(p)
	if g.hole != nil {
		// no reaparecer dentro del agujero negro: se sale a un lado
		a := rand.Float64() * 2 * math.Pi
		d := g.hole.r + 120
		if g.w > 0 {
			sx = math.Mod(g.hole.x+math.Cos(a)*d+g.w, g.w)
		}
		if g.h > 0 {
			sy = math.Mod(g.hole.y+math.Sin(a)*d+g.h, g.h)
		}
	}
	p.spawnX, p.spawnY = sx, sy
	p.respawnT = respawnDelay
}

func (g *Sim) fire(p *Player) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fireLocked(p)
}

// fireInterval es la cadencia de disparo: se reduce a la mitad con cada
// power-up de cadencia acumulado (hasta maxRapidFire → x8). El láser dispara
// además algo más rápido que las balas.
func (g *Sim) fireInterval(p *Player) float64 {
	n := p.rapidFire
	if n > maxRapidFire {
		n = maxRapidFire
	}
	iv := fireIntervalBase / float64(uint(1)<<uint(n))
	if p.laser {
		iv *= laserFireFactor
	}
	if iv < fireIntervalMin {
		iv = fireIntervalMin
	}
	return iv
}

// fireLocked dispara si la nave está viva y respeta la cadencia actual.
func (g *Sim) fireLocked(p *Player) {
	s := &p.ship
	if !s.alive || g.over || s.fireTimer > 0 {
		return
	}
	maxBullets := 5 + p.rapidFire*5
	if p.doubleShot {
		maxBullets *= 2 // cada disparo son dos proyectiles
	}
	if maxBullets > 60 {
		maxBullets = 60
	}
	// el tope cuenta solo los disparos propios: los del compañero no estorban
	own := 0
	for i := range g.bullets {
		if !g.bullets[i].pink {
			own++
		}
	}
	if own >= maxBullets {
		return
	}
	// el disparo doble lanza dos proyectiles en paralelo (desplazados a los
	// lados); el simple, uno centrado
	n := 1
	offsets := [2]float64{0, 0}
	if p.doubleShot {
		n = 2
		offsets = [2]float64{-7, 7}
	}
	// El origen y la dirección del disparo salen de la POSE que manda el cliente
	// (con el adelanto de lag), no de la nave del servidor: la del servidor va un
	// viaje por detrás de la que el jugador ve en su pantalla, así que si se
	// disparara desde ahí la bala salía desplazada y girada. Con la pose, la bala
	// nace en el morro que el jugador está viendo.
	// (Los bots no mandan pose: aimFor devuelve su propia nave y no cambia nada.)
	// OJO: la bala NO hereda la velocidad de la nave. Si la heredara, al ir
	// derivando la trayectoria se torcía respecto al morro (hasta 30° yendo
	// rápido), y el disparo se veía desalineado: salía del morro pero se iba de
	// lado. Con esto sale SIEMPRE en la dirección en la que apunta la nave.
	fx, fy, fang := g.aimFor(p)
	px, py := -math.Sin(fang), math.Cos(fang)
	for k := 0; k < n; k++ {
		b := astBullet{
			x:     fx + math.Cos(fang)*14 + px*offsets[k],
			y:     fy + math.Sin(fang)*14 + py*offsets[k],
			vx:    math.Cos(fang) * bulletSpeed,
			vy:    math.Sin(fang) * bulletSpeed,
			ttl:   bulletTTL,
			owner: p.ID,
		}

		if p.laser {
			// mismo alcance (velocidad x tiempo), pero mucho más rápido
			b.laser = true
			b.vx = math.Cos(fang) * laserSpeed
			b.vy = math.Sin(fang) * laserSpeed
			b.ttl = laserTTL
		}
		g.bullets = append(g.bullets, b)
	}
	s.fireTimer = g.fireInterval(p)
	g.shots++ // el cliente mira este contador para sonar al instante al predecir
	g.lastLaser = p.laser
	if p.laser {
		g.playSound("laser", 60*time.Millisecond)
	} else {
		g.playSound("shot", 40*time.Millisecond)
	}
}

// ShotsFired devuelve cuántos disparos se han hecho y si el último fue láser. El
// cliente multijugador lo usa para reproducir el sonido de SU predicción al
// instante (el del servidor llegaría con el retardo de la red).
func (g *Sim) ShotsFired() (int, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.shots, g.lastLaser
}

func (g *Sim) teleport(p *Player) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !p.ship.alive || g.over || p.ship.teleCooldown > 0 {
		return
	}
	if g.holeUnstoppableNow() {
		return // el agujero tira demasiado fuerte: el teleport no responde
	}
	for k := 0; k < 30; k++ {
		a := rand.Float64() * 2 * math.Pi
		sp := 30 + rand.Float64()*120
		g.parts = append(g.parts, astParticle{
			x: p.ship.x, y: p.ship.y,
			vx: math.Cos(a) * sp, vy: math.Sin(a) * sp, ttl: 0.5,
		})
	}
	// buscar sitio libre
	for try := 0; try < 40; try++ {
		x := 60 + rand.Float64()*(g.w-120)
		y := 60 + rand.Float64()*(g.h-120)
		ok := true
		for _, r := range g.rocks {
			if math.Hypot(x-r.x, y-r.y) < r.r+40 {
				ok = false
				break
			}
		}
		if ok {
			p.ship.x, p.ship.y = x, y
			break
		}
	}
	p.ship.vx, p.ship.vy = 0, 0
	p.ship.teleCooldown = 3
	p.ship.invuln = math.Max(p.ship.invuln, 1.2)
	g.playSound("teleport", 0)
}

func (g *Sim) restart() {
	g.mu.Lock()
	g.rocks = nil
	g.bullets = nil
	g.parts = nil
	g.missiles = nil
	g.mines = nil
	g.enemies = nil
	g.enemyShots = nil
	g.chains = nil
	g.bossBeams = nil
	g.boss = nil
	g.bossCount = 0
	g.powerups = nil
	g.nextShotID = 0
	g.gameTime = 0
	g.nextEnemyAt = enemyFirstAt
	g.enemiesKilled = 0
	g.timeLeft = levelTimeTotal
	g.timeWarned = false
	g.rockRushMult = 1
	g.hole = nil
	g.level = 1
	g.over = false
	g.paused = false
	// reiniciar a todos los jugadores
	for _, p := range g.players {
		g.resetPlayerLocked(p)
	}
	g.mu.Unlock()
	for _, p := range g.players {
		g.resetShip(p)
	}
	g.newLevel()
	g.playSound("start", 0)
}

// ---------- entrada de teclado ----------

// TypedRune detecta los cheatcodes escritos (no se muestran en pantalla).
func (g *Sim) TypedRune(r rune) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	g.cheatBuf = append(g.cheatBuf, cheatKey{r: r, at: now})
	g.cheatLastAt = now
	cut := now.Add(-1200 * time.Millisecond)
	i := 0
	for i < len(g.cheatBuf) && g.cheatBuf[i].at.Before(cut) {
		i++
	}
	if i > 0 {
		g.cheatBuf = g.cheatBuf[i:]
	}
	var sb strings.Builder
	for _, c := range g.cheatBuf {
		sb.WriteRune(c.r)
	}
	seq := sb.String()
	switch {
	case strings.HasSuffix(seq, "666"):
		if lp := g.Local(); lp != nil {
			lp.missilesLeft = 666
		}
		g.cheatBuf = nil
	case strings.HasSuffix(seq, "zzz"):
		g.cheatBuf = nil
		g.cheatLethalStorm()
	case strings.HasSuffix(seq, "ggg"):
		g.cheatBuf = nil
		g.cheatShieldMax()
	}
}

// commitCheats aplica el cheat "h<numero>" (salta al nivel) cuando el jugador
// deja de teclear ~350 ms (así "h12" no salta antes de tiempo con "h1").
func (g *Sim) commitCheats() {
	if len(g.cheatBuf) == 0 {
		return
	}
	if time.Since(g.cheatLastAt) < 350*time.Millisecond {
		return
	}
	var sb strings.Builder
	for _, c := range g.cheatBuf {
		sb.WriteRune(c.r)
	}
	if n, ok := levelFromCheat(sb.String()); ok {
		g.cheatJumpLevel(n)
	}
	g.cheatBuf = nil
}

// levelFromCheat extrae el nivel de una secuencia tipo "h7" o "h+12".
func levelFromCheat(seq string) (int, bool) {
	i := strings.LastIndex(seq, "h")
	if i < 0 {
		return 0, false
	}
	rest := strings.TrimPrefix(seq[i+1:], "+")
	if rest == "" {
		return 0, false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 1 || n > 99 {
		return 0, false
	}
	return n, true
}

// cheatJumpLevel salta directamente al nivel n con el efecto de hiperespacio.
func (g *Sim) cheatJumpLevel(n int) {
	if n < 1 {
		n = 1
	}
	g.level = n
	if lp := g.Local(); lp != nil {
		g.startHyper(lp)
	}
	g.playSound("hyper", 0)
	g.enemies = nil
	g.enemyShots = nil
	g.boss = nil
	g.bossBeams = nil
	g.mines = nil
	g.powerups = nil
	g.chains = nil
	g.rocks = nil
	g.hole = nil
	g.rockRushMult = 1
}

// cheatShieldMax pone el escudo al máximo acumulable.
func (g *Sim) cheatShieldMax() {
	if lp := g.Local(); lp != nil {
		lp.permShield = permShieldCap
	}
	g.playSound("shield", 0)
}

// cheatLethalStorm lanza 30 rayos letales en todas direcciones hasta el borde.
func (g *Sim) cheatLethalStorm() {
	lp := g.Local()
	if lp == nil {
		return
	}
	s := lp.ship
	if !s.alive || g.over || g.w == 0 {
		return
	}
	for k := 0; k < 30; k++ {
		ang := float64(k) * 2 * math.Pi / 30
		g.castSegment(s.x, s.y, ang, rayToEdge(s.x, s.y, ang, g.w, g.h))
	}
	g.playSound("lethal", 0)
}

// castSegment destruye todo lo que pilla el segmento (origen+dirección+longitud).
func (g *Sim) castSegment(x0, y0, ang, length float64) {
	dx, dy := math.Cos(ang), math.Sin(ang)
	g.chains = append(g.chains, astChain{
		x1: x0, y1: y0, x2: x0 + dx*length, y2: y0 + dy*length, ttl: 0.6, level: 1,
	})
	for i := len(g.rocks) - 1; i >= 0; i-- {
		r := g.rocks[i]
		if distToRay(x0, y0, dx, dy, length, r.x, r.y) < r.r {
			g.destroyRockComplete(i)
		}
	}
	for i := len(g.enemies) - 1; i >= 0; i-- {
		e := g.enemies[i]
		if distToRay(x0, y0, dx, dy, length, e.x, e.y) < e.r {
			g.destroyEnemy(i)
		}
	}
	if g.boss != nil && distToRay(x0, y0, dx, dy, length, g.boss.x, g.boss.y) < g.boss.r {
		g.damageBoss(50)
	}
}

// rayToEdge devuelve la longitud desde (x,y) en la dirección ang hasta el borde.
func rayToEdge(x, y, ang, w, h float64) float64 {
	L := w + h
	dx, dy := math.Cos(ang), math.Sin(ang)
	t := 1.0
	if dx > 0 {
		t = math.Min(t, (w-x)/(dx*L))
	} else if dx < 0 {
		t = math.Min(t, (0-x)/(dx*L))
	}
	if dy > 0 {
		t = math.Min(t, (h-y)/(dy*L))
	} else if dy < 0 {
		t = math.Min(t, (0-y)/(dy*L))
	}
	if t < 0 {
		t = 0
	}
	return t * L
}

// KeyDown/KeyUp reciben el nombre de la tecla (los mismos nombres que Fyne). El
// input del multijugador viaja como estos eventos, o como estado de teclas.
// KeyDown/KeyUp reciben el nombre de la tecla (los mismos nombres que Fyne) y
// actúan sobre el jugador local (índice 0): la plataforma de un solo jugador no
// necesita saber nada del multijugador.
func (g *Sim) KeyDown(name string) { g.KeyDownFor(g.Local(), name) }

func (g *Sim) KeyUp(name string) { g.KeyUpFor(g.Local(), name) }

// KeyDownFor/KeyUpFor entregan una tecla al jugador p. Las teclas globales de
// la partida (X silencio, P pausa) siguen siendo del Sim aunque lleguen con un
// jugador.
//
// Controles: W/A/D mover (S escudo), espacio disparar, M misil, K minas, L rayo
// letal, G railgun (cargando mientras se mantiene), enter teletransporte.
func (g *Sim) KeyDownFor(p *Player, name string) {
	if p == nil {
		p = g.Local()
	}
	if p == nil {
		return
	}
	switch name {
	case KeySpace:
		g.mu.Lock()
		p.keys[KeySpace] = true
		g.mu.Unlock()
		g.fire(p)
		return
	case KeyReturn, KeyEnter:
		g.teleport(p)
		return
	case "M":
		g.launchMissile(p)
		return
	case "K":
		g.dropMine(p)
		return
	case "L":
		g.fireLethalRay(p)
		return
	case KeyS, KeyDown:
		g.activateShield(p)
		return
	case "G":
		g.fireRail(p)
		return
	case "R":
		// al acabar la partida, R vuelve a empezar
		if g.over {
			g.restart()
		}
		return
	case KeyMute:
		g.muted.Store(!g.muted.Load())
		return
	case "P":
		g.mu.Lock()
		g.paused = !g.paused
		g.mu.Unlock()
		return
	}
	g.mu.Lock()
	p.keys[name] = true
	g.mu.Unlock()
}

func (g *Sim) KeyUpFor(p *Player, name string) {
	if p == nil {
		p = g.Local()
	}
	if p == nil {
		return
	}
	if name == "G" {
		g.releaseRail(p)
	}
	g.mu.Lock()
	delete(p.keys, name)
	g.mu.Unlock()
}

// ---------- render ----------

// formatScore separa los millares según el idioma (es/fr punto, en coma).
func formatScore(n int) string {
	sep := ","
	langMu.RLock()
	l := curLang
	langMu.RUnlock()
	if l == "es" || l == "fr" {
		sep = "."
	}
	digits := fmt.Sprintf("%d", n)
	neg := ""
	if strings.HasPrefix(digits, "-") {
		neg, digits = "-", digits[1:]
	}
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteString(sep)
		}
		b.WriteRune(c)
	}
	return neg + b.String()
}

// formatClock da formato M:SS al cronómetro del nivel, con signo cuando ya
// cuenta en negativo (agujero negro activo).
func formatClock(t float64) string {
	neg := t < 0
	if neg {
		t = -t
	}
	s := int(math.Ceil(t))
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s%d:%02d", sign, s/60, s%60)
}

// glowPoly dibuja una polilínea con efecto de fósforo (halo + línea brillante).
func glowPoly(d Drawer, pts [][2]float64, close bool, width float64) {
	glowPolyColor(d, pts, close, width, 0x39, 0xff, 0x6a)
}

// playerTints son los colores de las naves, por jugador. El 0 es el verde de
// fósforo de siempre; los demás, para distinguirlos en multijugador.
var playerTints = [8][3]uint8{
	{0x39, 0xff, 0x6a}, // verde
	{0xc0, 0x7a, 0xff}, // morado
	{0x5a, 0xd0, 0xff}, // cian
	{0xff, 0xc0, 0x50}, // ámbar
	{0xff, 0x7a, 0x9a}, // rosa
	{0xff, 0xff, 0xff}, // blanco
	{0xff, 0x9a, 0x4a}, // naranja
	{0x9a, 0x9a, 0xff}, // azul
}

// tintOf devuelve el color de la nave del jugador id.
func tintOf(id int) (uint8, uint8, uint8) {
	n := len(playerTints)
	t := playerTints[((id%n)+n)%n]
	return t[0], t[1], t[2]
}

// glowPolyColor es glowPoly con color propio (p. ej. ámbar para rocas duras).
func glowPolyColor(d Drawer, pts [][2]float64, close bool, width float64, cr, cg, cb uint8) {
	if len(pts) < 2 {
		return
	}
	// el halo son hasta 3 pases (ancho + medio + línea); glowPasses() permite
	// recortarlos para medir cuánto cuesta el efecto
	for pass := 3 - glowPasses(); pass < 3; pass++ {
		var w float64
		var alpha uint8
		switch pass {
		case 0:
			w, alpha = width*3.5, 0x22
		case 1:
			w, alpha = width*2, 0x55
		default:
			w, alpha = width, 0xff
		}
		d.SetColor(color.NRGBA{R: cr, G: cg, B: cb, A: alpha})
		d.SetLineWidth(w)
		d.MoveTo(pts[0][0], pts[0][1])
		for _, p := range pts[1:] {
			d.LineTo(p[0], p[1])
		}
		if close {
			d.ClosePath()
		}
		d.Stroke()
	}
}

// enemyShapeCount es el número de siluetas enemigas disponibles.
const enemyShapeCount = 6

// drawEnemyShip dibuja la silueta de una nave enemiga (inspiradas en naves de
// villanos de Star Trek: ave de presa Klingon, warbird Romulana, cubo Borg,
// Galor Cardassiana, crucero D7 y marauder Ferengi).
func drawEnemyShip(d Drawer, e astEnemy) {
	x, y, r, a := e.x, e.y, e.r, e.angle
	d.SetColor(color.NRGBA{R: 0xff, G: 0x6a, B: 0x6a, A: 0xee})
	d.SetLineWidth(2)
	rot := func(px, py float64) (float64, float64) {
		c, s := math.Cos(a), math.Sin(a)
		return x + px*c - py*s, y + px*s + py*c
	}
	poly := func(pts ...[2]float64) {
		d.MoveTo(rot(pts[0][0]*r, pts[0][1]*r))
		for _, p := range pts[1:] {
			d.LineTo(rot(p[0]*r, p[1]*r))
		}
	}
	switch e.shape {
	case 0: // Ave de presa Klingon (dardo central + alas en flecha)
		poly([2]float64{0, -0.95}, [2]float64{0.28, 0}, [2]float64{0, 0.7}, [2]float64{-0.28, 0})
		d.ClosePath()
		d.Stroke()
		poly([2]float64{-0.2, -0.1}, [2]float64{-1.15, 0.55}, [2]float64{-1.15, 0.25}, [2]float64{-0.2, 0.2})
		d.ClosePath()
		d.Stroke()
		poly([2]float64{0.2, -0.1}, [2]float64{1.15, 0.55}, [2]float64{1.15, 0.25}, [2]float64{0.2, 0.2})
		d.ClosePath()
		d.Stroke()
	case 1: // Warbird Romulana (alas largas hacia atrás)
		poly([2]float64{0, -0.95}, [2]float64{0.35, -0.1}, [2]float64{1.05, 0.5}, [2]float64{0.4, 0.55},
			[2]float64{0, 0.3}, [2]float64{-0.4, 0.55}, [2]float64{-1.05, 0.5}, [2]float64{-0.35, -0.1})
		d.ClosePath()
		d.Stroke()
	case 2: // Cubo Borg
		poly([2]float64{-0.6, -0.6}, [2]float64{0.6, -0.6}, [2]float64{0.6, 0.6}, [2]float64{-0.6, 0.6})
		d.ClosePath()
		d.Stroke()
		poly([2]float64{-0.3, -0.3}, [2]float64{0.3, -0.3}, [2]float64{0.3, 0.3}, [2]float64{-0.3, 0.3})
		d.ClosePath()
		d.Stroke()
		for _, c := range [][2]float64{{-0.6, -0.6}, {0.6, -0.6}, {0.6, 0.6}, {-0.6, 0.6}} {
			px, py := rot(c[0]*r, c[1]*r)
			d.DrawCircle(px, py, 1.6)
			d.Fill()
		}
	case 3: // Galor Cardassiana (punta de flecha ancha)
		poly([2]float64{0, -0.7}, [2]float64{1.05, 0.5}, [2]float64{0.28, 0.55},
			[2]float64{0, 0.2}, [2]float64{-0.28, 0.55}, [2]float64{-1.05, 0.5})
		d.ClosePath()
		d.Stroke()
	case 4: // Crucero Klingon D7 (sajón + cuello + góndolas)
		cx, cy := rot(0, -0.55*r)
		d.DrawCircle(cx, cy, 0.48*r)
		d.Stroke()
		poly([2]float64{0, -0.5}, [2]float64{0, 0.45})
		d.Stroke()
		poly([2]float64{-0.5, 0.45}, [2]float64{-0.5, 0.75}, [2]float64{0.5, 0.75}, [2]float64{0.5, 0.45})
		d.ClosePath()
		d.Stroke()
	case 5: // Marauder Ferengi (herradura)
		poly([2]float64{-0.85, -0.35}, [2]float64{-0.85, 0.55}, [2]float64{-0.4, 0.15},
			[2]float64{0.4, 0.15}, [2]float64{0.85, 0.55}, [2]float64{0.85, -0.35})
		d.Stroke()
	}
}

// drawEnemyHealth dibuja la barrita de resistencia de una nave enemiga. Solo
// se ve en las naves duras (a partir del 2º nivel), encima de la silueta.
func drawEnemyHealth(d Drawer, e astEnemy) {
	if e.hpMax <= 1 {
		return
	}
	frac := float64(e.hp) / float64(e.hpMax)
	if frac < 0 {
		frac = 0
	}
	bw := 26.0
	bx, by := e.x-bw/2, e.y-e.r-13
	d.SetColor(color.NRGBA{R: 0x33, G: 0x00, B: 0x00, A: 0xcc})
	d.DrawRectangle(bx, by, bw, 4)
	d.Fill()
	col := color.NRGBA{R: 0x2f, G: 0xd0, B: 0x55, A: 0xff}
	if frac < 0.35 {
		col = color.NRGBA{R: 0xe0, G: 0x36, B: 0x36, A: 0xff}
	} else if frac < 0.7 {
		col = color.NRGBA{R: 0xf0, G: 0xa0, B: 0x10, A: 0xff}
	}
	d.SetColor(col)
	d.DrawRectangle(bx, by, bw*frac, 4)
	d.Fill()
}

// drawBossShip dibuja la nave jefe (silueta grande) con su barra de energía.
func drawBossShip(d Drawer, b astBoss) {
	x, y, r, a := b.x, b.y, b.r, b.angle
	col := color.NRGBA{R: 0xff, G: 0x6a, B: 0x6a, A: 0xff}
	if b.hitFlash > 0 {
		col = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	}
	d.SetColor(col)
	d.SetLineWidth(3)
	rot := func(px, py float64) (float64, float64) {
		c, s := math.Cos(a), math.Sin(a)
		return x + px*c - py*s, y + px*s + py*c
	}
	poly := func(pts ...[2]float64) {
		d.MoveTo(rot(pts[0][0]*r, pts[0][1]*r))
		for _, p := range pts[1:] {
			d.LineTo(rot(p[0]*r, p[1]*r))
		}
	}
	// Raider cylon (Battlestar Galactica): dos brazos-ala que salen de la cola,
	// se abren hacia delante y curvan las puntas hacia dentro (pinza), con el
	// cuerpo central entre ellos y los dos "ojos" rojos cerca de la proa.
	poly([2]float64{0.10, 0.88}, [2]float64{-1.30, -0.38}, [2]float64{-0.42, -0.99},
		[2]float64{-0.34, -0.70}, [2]float64{-0.86, -0.30}, [2]float64{-0.10, 0.62})
	d.ClosePath()
	d.Stroke()
	poly([2]float64{-0.10, 0.88}, [2]float64{1.30, -0.38}, [2]float64{0.42, -0.99},
		[2]float64{0.34, -0.70}, [2]float64{0.86, -0.30}, [2]float64{0.10, 0.62})
	d.ClosePath()
	d.Stroke()
	// cuerpo central (hoja) de proa a cola
	poly([2]float64{0, -0.95}, [2]float64{0.20, -0.30}, [2]float64{0.20, 0.72},
		[2]float64{0, 1.02}, [2]float64{-0.20, 0.72}, [2]float64{-0.20, -0.30})
	d.ClosePath()
	d.Stroke()
	// los dos "ojos" rojos
	for _, e := range [][2]float64{{0.09, -0.55}, {-0.09, -0.55}} {
		ex, ey := rot(e[0]*r, e[1]*r)
		d.SetColor(color.NRGBA{R: 0xff, G: 0x30, B: 0x30, A: 0xff})
		d.DrawCircle(ex, ey, r*0.11)
		d.Fill()
	}
	d.SetColor(col)
	// barra de energía
	bw := 104.0
	bx := x - bw/2
	by := y - r - 20
	d.SetColor(color.NRGBA{R: 0x33, G: 0x00, B: 0x00, A: 0xcc})
	d.DrawRectangle(bx, by, bw, 7)
	d.Fill()
	frac := b.energy / b.maxEnergy
	if frac < 0 {
		frac = 0
	}
	ec := color.NRGBA{R: 0x2f, G: 0xd0, B: 0x55, A: 0xff}
	if frac < 0.35 {
		ec = color.NRGBA{R: 0xe0, G: 0x36, B: 0x36, A: 0xff}
	} else if frac < 0.7 {
		ec = color.NRGBA{R: 0xf0, G: 0xa0, B: 0x10, A: 0xff}
	}
	d.SetColor(ec)
	d.DrawRectangle(bx, by, bw*frac, 7)
	d.Fill()
}

// render dibuja el frame en un raster propio y devuelve la imagen: es la vía
// nativa, donde Fyne pinta ese raster. En web se dibuja directamente al canvas
// (draw_canvas_wasm.go) para no rasterizar por CPU.
// DrawFrame prepara el lienzo d (de rw x rh píxeles) y dibuja la escena en un
// mundo de worldW x worldH unidades: fondo del letterbox, transformación y juego.
// La plataforma decide el tamaño del lienzo (nitidez) y del mundo (unidades
// lógicas); aquí solo se pinta.
func (g *Sim) DrawFrame(d Drawer, rw, rh int, worldW, worldH float64) {
	// el fondo del letterbox va en otro tono para que se vea dónde acaba el mundo
	// (durante el spike; en el juego final será todo negro)
	d.SetHexColor("#0a1a2a")
	d.Clear()
	if rw < 8 || rh < 8 {
		return
	}
	// el mundo se escala para llenar el lienzo sin deformar y se centra
	view := math.Min(float64(rw)/worldW, float64(rh)/worldH)
	d.Translate((float64(rw)-worldW*view)/2, (float64(rh)-worldH*view)/2)
	d.Scale(view, view)
	// fondo del mundo (lo de fuera es el letterbox)
	d.SetHexColor("#020a04")
	d.DrawRectangle(0, 0, worldW, worldH)
	d.Fill()

	g.drawArena(d, rw, rh, worldW, worldH)
}

// drawPowerupIcon dibuja el icono de un power-up en (x,y). Lo usan los que
// flotan por la arena y los que están escondidos dentro de una estructura del
// campo fijo (esos se ven a través).
func drawPowerupIcon(d Drawer, x, y float64, kind int, spin float64) {

	switch kind {
	case 1:
		// vida extra: navecita verde
		d.SetColor(color.NRGBA{R: 0x2f, G: 0xd0, B: 0x55, A: 0xff})
		d.MoveTo(x, y-9)
		d.LineTo(x-7, y+6)
		d.LineTo(x, y+2)
		d.LineTo(x+7, y+6)
		d.ClosePath()
		d.Fill()
		return
	case 2:
		// escudo permanente: escudo cian BIEN visible (relleno, contorno blanco y
		// halo), que antes iba casi transparente y no se veía
		shield := func(r float64) {
			d.MoveTo(x, y-r)
			d.LineTo(x+r*0.85, y-r*0.5)
			d.LineTo(x+r*0.85, y+r*0.25)
			d.LineTo(x, y+r)
			d.LineTo(x-r*0.85, y+r*0.25)
			d.LineTo(x-r*0.85, y-r*0.5)
			d.ClosePath()
		}
		// halo
		d.SetColor(color.NRGBA{R: 0x7a, G: 0xe0, B: 0xff, A: 0x55})
		shield(13)
		d.Fill()
		// relleno
		d.SetColor(color.NRGBA{R: 0x9f, G: 0xe8, B: 0xff, A: 0xf0})
		shield(10)
		d.Fill()
		// contorno
		d.SetColor(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
		d.SetLineWidth(2.2)
		shield(10)
		d.Stroke()
		return
	case 3:
		// cadencia x2: rayo amarillo
		d.SetColor(color.NRGBA{R: 0xff, G: 0xe2, B: 0x40, A: 0xff})
		d.MoveTo(x+3, y-9)
		d.LineTo(x-4, y+1)
		d.LineTo(x+1, y+1)
		d.LineTo(x-3, y+9)
		d.LineTo(x+5, y-2)
		d.LineTo(x, y-2)
		d.ClosePath()
		d.Fill()
		return
	case powerupMissiles:
		// +10 misiles: misil ámbar
		d.SetColor(color.NRGBA{R: 0xff, G: 0xd0, B: 0x60, A: 0xff})
		d.MoveTo(x, y-10)
		d.LineTo(x-4, y+4)
		d.LineTo(x+4, y+4)
		d.ClosePath()
		d.Fill()
		d.SetLineWidth(1.6)
		d.MoveTo(x-5, y+8)
		d.LineTo(x+5, y+8)
		d.Stroke()
		return
	case powerupMines:
		// +10 minas: mina naranja con púas
		d.SetColor(color.NRGBA{R: 0xff, G: 0x5a, B: 0x4a, A: 0xff})
		d.DrawCircle(x, y, 5)
		d.Fill()
		d.SetLineWidth(1.5)
		for k := 0; k < 6; k++ {
			a := float64(k) * math.Pi / 3
			d.MoveTo(x+math.Cos(a)*5, y+math.Sin(a)*5)
			d.LineTo(x+math.Cos(a)*9, y+math.Sin(a)*9)
		}
		d.Stroke()
		return
	case powerupLaser:
		// láser: raya corta cian
		d.SetColor(color.NRGBA{R: 0x9f, G: 0xe8, B: 0xff, A: 0xff})
		d.SetLineWidth(3)
		d.MoveTo(x, y-9)
		d.LineTo(x, y+9)
		d.Stroke()
		d.SetColor(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
		d.SetLineWidth(1.2)
		d.MoveTo(x, y-9)
		d.LineTo(x, y+9)
		d.Stroke()
		return
	case powerupGun:
		// ametralladora: tres balas verdes
		d.SetColor(color.NRGBA{R: 0x8f, G: 0xff, B: 0xb0, A: 0xff})
		for k := -1; k <= 1; k++ {
			d.DrawRectangle(x+float64(k)*6-1.5, y-8, 3, 9)
			d.Fill()
		}
		return
	case powerupDouble:
		// disparo doble: dos balas gemelas
		d.SetColor(color.NRGBA{R: 0xff, G: 0x8a, B: 0xe0, A: 0xff})
		for k := -1; k <= 1; k += 2 {
			d.DrawRectangle(x+float64(k)*5-2, y-8, 4, 10)
			d.Fill()
		}
		return
	case powerupCompanion:
		// compañero: navecita azul
		d.SetColor(color.NRGBA{R: 0x5a, G: 0x9a, B: 0xff, A: 0xff})
		d.MoveTo(x, y-10)
		d.LineTo(x-7, y+6)
		d.LineTo(x, y+2)
		d.LineTo(x+7, y+6)
		d.ClosePath()
		d.Fill()
		return
	}
	// kind 0: asistente ("bolita protectora"): órbita + núcleo orbitando
	r := 8 + 1.5*math.Sin(spin)
	d.SetColor(color.NRGBA{R: 0x6a, G: 0xd0, B: 0xff, A: 0xcc})
	d.SetLineWidth(1.5)
	d.DrawCircle(x, y, r)
	d.Stroke()
	da := spin * 2
	d.SetColor(color.NRGBA{R: 0xdf, G: 0xf6, B: 0xff, A: 0xff})
	d.DrawCircle(x+math.Cos(da)*r*0.55, y+math.Sin(da)*r*0.55, 2.6)
	d.Fill()

}

// drawArena dibuja la escena en coordenadas de mundo. rastW/rastH son las del
// lienzo real, solo para el HUD de diagnóstico.
func (g *Sim) drawArena(d Drawer, rastW, rastH int, worldW, worldH float64) {
	pw, ph := int(worldW), int(worldH)

	g.mu.Lock()
	if g.w != float64(pw) || g.h != float64(ph) {
		if g.w == 0 && g.h == 0 {
			// primera medida: recolocar a los jugadores en el centro
			for _, p := range g.players {
				p.ship.x, p.ship.y = float64(pw)/2, float64(ph)/2
			}
		}
		g.w, g.h = float64(pw), float64(ph)
	}
	// copia del estado para no dibujar con el lock tomado
	rocks := append([]astRock(nil), g.rocks...)
	bullets := append([]astBullet(nil), g.bullets...)
	parts := append([]astParticle(nil), g.parts...)
	missiles := append([]astMissile(nil), g.missiles...)
	mines := append([]astMine(nil), g.mines...)
	enemies := append([]astEnemy(nil), g.enemies...)
	enemyShots := append([]astEnemyBullet(nil), g.enemyShots...)
	chains := append([]astChain(nil), g.chains...)
	bossBeams := append([]astBossBeam(nil), g.bossBeams...)
	var bossV astBoss
	hasBoss := g.boss != nil
	if hasBoss {
		bossV = *g.boss
	}
	powerups := append([]astPowerup(nil), g.powerups...)
	muted := g.muted.Load()
	gameTime := g.gameTime
	timeLeft := g.timeLeft
	var holeV astHole
	hasHole := g.hole != nil
	if hasHole {
		holeV = *g.hole
	}
	level, over, paused := g.level, g.over, g.paused
	pvpMode := g.pvp
	fixedArena := g.fixedArena()
	best, bestLevel, bestName := g.Best, g.BestLevel, g.BestName
	fadeIn := g.fadeIn
	levelIntro := g.levelIntro
	flash := g.flash
	// copia por jugador (la nave, sus armas y sus asistentes)
	players := make([]playerRender, len(g.players))
	for i, p := range g.players {
		pr := &players[i]
		pr.id = p.ID
		pr.name = p.Name
		pr.kills = p.kills
		pr.deaths = p.deaths
		pr.deathT = p.deathT
		pr.deathX, pr.deathY = p.deathX, p.deathY
		pr.respawnT = p.respawnT
		pr.spawnX, pr.spawnY = p.spawnX, p.spawnY
		pr.ship = p.ship
		pr.shieldT = p.shieldT
		pr.shieldCd = p.shieldCd
		pr.permShield = p.permShield
		pr.hull = p.hull
		pr.rapidFire = p.rapidFire
		pr.laser = p.laser
		pr.doubleShot = p.doubleShot
		pr.lethalCd = p.lethalCd
		pr.railCharge = p.railCharge
		pr.railRechargeTotal = p.railRechargeTotal
		pr.railCharging = p.railCharging
		pr.railHold = p.railHold
		pr.railAngle = p.railAngle
		pr.railFlash = p.railFlash
		pr.railFlashBase = p.railFlashBase
		pr.railFlashHalf = p.railFlashHalf
		pr.missilesLeft = p.missilesLeft
		pr.minesLeft = p.minesLeft
		pr.score = p.score
		pr.lives = p.lives
		pr.hyper = p.hyper
		pr.hyperStars = append([]hyperStar(nil), p.hyperStars...)
		pr.orbs = append([]astOrb(nil), p.orbs...)
		pr.orbShots = append([]astOrbShot(nil), p.orbShots...)
		pr.hasComp = p.companion != nil
		if pr.hasComp {
			pr.comp = *p.companion
		}
	}
	// el HUD muestra los datos de TU jugador (lo dice localID), no siempre el 0
	local := &playerRender{}
	if len(players) > 0 {
		local = &players[0]
	}
	for i := range players {
		if players[i].id == g.localID {
			local = &players[i]
			break
		}
	}
	g.mu.Unlock()

	// durante el hiperespacio sólo se ven las estrellas
	if local.hyper > 0 {
		cxm, cym := float64(pw)/2, float64(ph)/2
		for _, s := range local.hyperStars {
			r1 := s.rad * 0.35
			al := uint8(math.Min(255, s.rad*0.8))
			d.SetColor(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: al})
			d.SetLineWidth(1.6)
			d.MoveTo(cxm+math.Cos(s.ang)*r1, cym+math.Sin(s.ang)*r1)
			d.LineTo(cxm+math.Cos(s.ang)*s.rad, cym+math.Sin(s.ang)*s.rad)
			d.Stroke()
		}
		return
	}

	// rejilla tenue + scanlines
	d.SetColor(color.NRGBA{R: 0x0d, G: 0x3d, B: 0x1c, A: 0xff})
	for x := 0.0; x < float64(pw); x += 64 {
		d.DrawLine(x, 0, x, float64(ph))
	}
	for y := 0.0; y < float64(ph); y += 64 {
		d.DrawLine(0, y, float64(pw), y)
	}
	d.SetLineWidth(1)
	d.Stroke()

	// rocas: las duras se distinguen por una línea más gruesa (a más "nivel de
	// grosor", más dura) y un tono ámbar.
	for _, r := range rocks {
		if r.fixed {
			// estructura del campo fijo: bloque cuadrado en azul (se distingue de
			// los asteroides). A más dureza, trazo más grueso.
			w := 2.0
			if r.hpMax > fixedBlockHP {
				w = 2 + float64(r.hpMax-fixedBlockHP)*0.8
			}
			// si esconde un power-up, se ve a través de la estructura
			if r.powerup >= 0 {
				drawPowerupIcon(d, r.x, r.y, r.powerup, 0)
			}
			// grietas según lo golpeada que esté (para ver su estado). Van del
			// MISMO color que el borde del bloque: en azul oscuro sobre el fondo
			// negro no se veían.
			if dmg := r.hpMax - r.hp; dmg > 0 {
				d.SetColor(color.NRGBA{R: 0x7a, G: 0xd0, B: 0xff, A: 0xc0})
				d.SetLineWidth(2.0)
				for k := 0; k < dmg && k < 6; k++ {
					// grietas deterministas (misma forma siempre para ese bloque)
					a := float64(k)*2.4 + float64(int(r.x+r.y)%3)
					ax := r.x + math.Cos(a)*r.r*0.7
					ay := r.y + math.Sin(a)*r.r*0.7
					bx := r.x + math.Cos(a+1.1)*r.r*0.95
					by := r.y + math.Sin(a+1.1)*r.r*0.95
					cx2 := r.x + math.Cos(a+2.1)*r.r*0.6
					cy2 := r.y + math.Sin(a+2.1)*r.r*0.6
					d.MoveTo(ax, ay)
					d.LineTo(bx, by)
					d.LineTo(cx2, cy2)
					d.Stroke()
				}
			}
			pts := [][2]float64{
				{r.x - r.r, r.y - r.r},
				{r.x + r.r, r.y - r.r},
				{r.x + r.r, r.y + r.r},
				{r.x - r.r, r.y + r.r},
			}
			glowPolyColor(d, pts, true, w, 0x7a, 0xd0, 0xff)
			continue
		}
		n := len(r.shape)
		pts := make([][2]float64, n)
		for i := 0; i < n; i++ {
			a := r.angle + float64(i)*2*math.Pi/float64(n)
			rr := r.r * r.shape[i]
			pts[i] = [2]float64{r.x + math.Cos(a)*rr, r.y + math.Sin(a)*rr}
		}
		w := 2.0
		if r.hpMax > 1 {
			// mismo color, pero paredes más gruesas: a más dureza, más grosor
			w = 1.2 + float64(r.hpMax)*0.8
		}
		glowPoly(d, pts, true, w)
	}

	// partículas
	for _, p := range parts {
		d.SetColor(color.NRGBA{R: 0x39, G: 0xff, B: 0x6a, A: uint8(math.Min(255, p.ttl*500))})
		d.DrawCircle(p.x, p.y, 1.6)
		d.Fill()
	}

	// balas: normales (verde), láser (raya corta cian) y del compañero (rosa)
	for _, b := range bullets {
		switch {
		case b.laser:
			ux, uy := b.vx, b.vy
			if n := math.Hypot(ux, uy); n > 0 {
				ux, uy = ux/n, uy/n
			}
			glowPolyColor(d, [][2]float64{
				{b.x - ux*laserDashBack, b.y - uy*laserDashBack},
				{b.x + ux*laserDashFront, b.y + uy*laserDashFront},
			}, false, 2.2, 0x9f, 0xe8, 0xff)
		case b.pink:
			glowPolyColor(d, [][2]float64{
				{b.x, b.y}, {b.x - b.vx*0.02, b.y - b.vy*0.02},
			}, false, 2.2, 0x5a, 0x9a, 0xff)
		default:
			glowPoly(d, [][2]float64{{b.x, b.y}, {b.x - b.vx*0.012, b.y - b.vy*0.012}}, false, 2.5)
		}
	}

	// minas
	for _, m := range mines {
		if m.arm > 0 {
			pulse := 0.5 + 0.5*math.Sin(gameTime*18)
			d.SetColor(color.NRGBA{R: 0xff, G: 0xb0, B: 0x40, A: uint8(80 + 150*pulse)})
			d.SetLineWidth(1.5)
			d.DrawCircle(m.x, m.y, m.r*(0.8+0.5*pulse))
			d.Stroke()
			d.SetColor(color.NRGBA{R: 0xff, G: 0xb0, B: 0x40, A: 0xff})
			d.DrawCircle(m.x, m.y, 3)
			d.Fill()
		} else {
			d.SetColor(color.NRGBA{R: 0xff, G: 0x5a, B: 0x4a, A: 0xff})
			d.DrawCircle(m.x, m.y, m.r*0.5)
			d.Fill()
			d.SetLineWidth(1.5)
			for k := 0; k < 6; k++ {
				a := float64(k) * math.Pi / 3
				d.MoveTo(m.x+math.Cos(a)*m.r*0.5, m.y+math.Sin(a)*m.r*0.5)
				d.LineTo(m.x+math.Cos(a)*m.r, m.y+math.Sin(a)*m.r)
			}
			d.Stroke()
		}
	}

	// misiles + estela curva que se desvanece
	for _, m := range missiles {
		n := len(m.trail)
		for i := 1; i < n; i++ {
			al := uint8(200 * float64(i) / float64(n))
			d.SetColor(color.NRGBA{R: 0xff, G: 0xd0, B: 0x60, A: al})
			d.SetLineWidth(1.4)
			d.MoveTo(m.trail[i-1].x, m.trail[i-1].y)
			d.LineTo(m.trail[i].x, m.trail[i].y)
			d.Stroke()
		}
		d.SetColor(color.NRGBA{R: 0xff, G: 0xe0, B: 0x80, A: 0xff})
		d.DrawCircle(m.x, m.y, 3)
		d.Fill()
		d.MoveTo(m.x+math.Cos(m.angle)*7, m.y+math.Sin(m.angle)*7)
		d.LineTo(m.x+math.Cos(m.angle+2.6)*4, m.y+math.Sin(m.angle+2.6)*4)
		d.LineTo(m.x+math.Cos(m.angle-2.6)*4, m.y+math.Sin(m.angle-2.6)*4)
		d.ClosePath()
		d.Fill()
	}

	// disparos enemigos: los misiles (homing) con estela y forma de misil, como
	// los del jugador; los simples, un punto rojo.
	for _, s := range enemyShots {
		if s.homing && len(s.trail) > 1 {
			n := len(s.trail)
			for i := 1; i < n; i++ {
				al := uint8(190 * float64(i) / float64(n))
				d.SetColor(color.NRGBA{R: 0xff, G: 0x6a, B: 0x6a, A: al})
				d.SetLineWidth(1.4)
				d.MoveTo(s.trail[i-1].x, s.trail[i-1].y)
				d.LineTo(s.trail[i].x, s.trail[i].y)
				d.Stroke()
			}
			ang := math.Atan2(s.vy, s.vx)
			d.SetColor(color.NRGBA{R: 0xff, G: 0x8a, B: 0x8a, A: 0xff})
			d.DrawCircle(s.x, s.y, 2.6)
			d.Fill()
			d.MoveTo(s.x+math.Cos(ang)*7, s.y+math.Sin(ang)*7)
			d.LineTo(s.x+math.Cos(ang+2.6)*4, s.y+math.Sin(ang+2.6)*4)
			d.LineTo(s.x+math.Cos(ang-2.6)*4, s.y+math.Sin(ang-2.6)*4)
			d.ClosePath()
			d.Fill()
			continue
		}
		d.SetColor(color.NRGBA{R: 0xff, G: 0x6a, B: 0x6a, A: 0xff})
		d.DrawCircle(s.x, s.y, 2.4)
		d.Fill()
	}

	// naves enemigas (siluetas estilo Star Trek) con su barrita de resistencia
	for _, e := range enemies {
		drawEnemyShip(d, e)
		drawEnemyHealth(d, e)
	}

	// jefe (nave grande con barra de energía)
	if hasBoss {
		drawBossShip(d, bossV)
	}

	// railgun: haz cónico violeta, una sola raya continua (núcleo blanco)
	for pi := range players {
		pr := &players[pi]
		ship := pr.ship
		if !ship.alive || !(pr.railCharging || pr.railFlash > 0) {
			continue
		}
		var baseW, halfAng, alpha, frac float64
		if pr.railFlash > 0 {
			baseW, halfAng = pr.railFlashBase, pr.railFlashHalf
			alpha = math.Min(255, pr.railFlash/0.3*220)
		} else {
			frac = pr.railHold / railHoldMax
			baseW = 4 + frac*30
			halfAng = 0.09 + 0.19*frac
			alpha = 90 + 150*frac
		}
		length := rayToEdge(ship.x, ship.y, pr.railAngle, float64(pw), float64(ph))
		if pr.railFlash <= 0 {
			length *= 0.2 + 0.8*frac // el haz va creciendo hacia el borde
		}
		tipW := baseW + 2*length*math.Tan(halfAng)
		ex := ship.x + math.Cos(pr.railAngle)*length
		ey := ship.y + math.Sin(pr.railAngle)*length
		px, py := -math.Sin(pr.railAngle), math.Cos(pr.railAngle)
		p1x, p1y := ship.x+px*baseW/2, ship.y+py*baseW/2
		p2x, p2y := ex+px*tipW/2, ey+py*tipW/2
		p3x, p3y := ex-px*tipW/2, ey-py*tipW/2
		p4x, p4y := ship.x-px*baseW/2, ship.y-py*baseW/2
		au := uint8(alpha)
		// cono relleno (violeta), sin contornos que parezcan "rayillas"
		d.SetColor(color.NRGBA{R: 0xa8, G: 0x55, B: 0xff, A: uint8(alpha * 6 / 10)})
		d.MoveTo(p1x, p1y)
		d.LineTo(p2x, p2y)
		d.LineTo(p3x, p3y)
		d.LineTo(p4x, p4y)
		d.ClosePath()
		d.Fill()
		// núcleo blanco continuo a lo largo del eje
		d.SetColor(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: au})
		d.SetLineWidth(math.Max(2.5, baseW*0.55))
		d.MoveTo(ship.x, ship.y)
		d.LineTo(ex, ey)
		d.Stroke()
	}
	// railgun de la nodriza (blanco/rojo)
	for _, b := range bossBeams {
		al := uint8(255 * math.Min(1, math.Max(0, b.ttl/0.32)))
		tipW := b.tip
		ex := b.x + math.Cos(b.angle)*rayToEdge(b.x, b.y, b.angle, float64(pw), float64(ph))
		ey := b.y + math.Sin(b.angle)*rayToEdge(b.x, b.y, b.angle, float64(pw), float64(ph))
		px, py := -math.Sin(b.angle), math.Cos(b.angle)
		p1x, p1y := b.x+px*b.base/2, b.y+py*b.base/2
		p2x, p2y := ex+px*tipW/2, ey+py*tipW/2
		p3x, p3y := ex-px*tipW/2, ey-py*tipW/2
		p4x, p4y := b.x-px*b.base/2, b.y-py*b.base/2
		cone := color.NRGBA{R: 0xff, G: 0x30, B: 0x30, A: uint8(int(al) * 6 / 10)}
		if b.reflect {
			cone = color.NRGBA{R: 0xff, G: 0xc8, B: 0x40, A: uint8(int(al) * 7 / 10)}
		}
		d.SetColor(cone)
		d.MoveTo(p1x, p1y)
		d.LineTo(p2x, p2y)
		d.LineTo(p3x, p3y)
		d.LineTo(p4x, p4y)
		d.ClosePath()
		d.Fill()
		d.SetColor(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: al})
		d.SetLineWidth(math.Max(2.5, b.base*0.55))
		d.MoveTo(b.x, b.y)
		d.LineTo(ex, ey)
		d.Stroke()
	}
	// rayo letal (blanco, ramificado)
	for _, c := range chains {
		al := uint8(255 * math.Min(1, math.Max(0, c.ttl/0.35)))
		for pass := 0; pass < 2; pass++ {
			var w float64
			var a uint8
			if pass == 0 {
				w, a = 7, 0x55
			} else {
				w, a = 2.5, al
			}
			d.SetColor(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: a})
			d.SetLineWidth(w)
			d.MoveTo(c.x1, c.y1)
			d.LineTo(c.x2, c.y2)
			d.Stroke()
		}
	}

	// nave, compañero y asistentes de cada jugador
	for pi := range players {
		pr := &players[pi]
		ship := pr.ship
		// muerte: la nave hecha trozos, que salen volando girando y se apagan
		if pr.deathT > 0 {
			cr, cg, cb := tintOf(pr.id)
			drawShipWreck(d, pr, cr, cg, cb)
		}
		// reaparición: efecto de teletransporte donde se va a recomponer
		if pr.respawnT > 0 {
			cr, cg, cb := tintOf(pr.id)
			drawTeleport(d, pr, cr, cg, cb)
		}
		if ship.alive {
			pts := shipShape(pr.id, ship.x, ship.y, ship.angle)
			shieldLv := pr.permShield / permShieldPer // 0..5 acumulaciones
			hullW := 2.0
			if pr.permShield > 0 {
				hullW = 2 + shieldLv*1.4 // el escudo permanente engorda la línea del casco
			}
			cr, cg, cb := tintOf(pr.id)
			if ship.invuln <= 0 || int(ship.invuln*8)%2 == 0 {
				glowPolyColor(d, pts, true, hullW, cr, cg, cb)
			}
			// escudo activo (manual, temporal): dorado si es reflectante (cuando
			// además llevas escudo de power-up)
			if pr.shieldT > 0 {
				ring := color.NRGBA{R: 0x6a, G: 0xd0, B: 0xff, A: 0x99}
				lw := 2.0
				if pr.permShield > 0 {
					ring = color.NRGBA{R: 0xff, G: 0xc8, B: 0x40, A: 0xee}
					lw = 3.0
				}
				d.SetColor(ring)
				d.SetLineWidth(lw)
				d.DrawCircle(ship.x, ship.y, reflectRadius)
				d.Stroke()
				if pr.permShield > 0 {
					// destello interior, para que se note que rebota
					d.SetColor(color.NRGBA{R: 0xff, G: 0xe8, B: 0x9a, A: 0x55})
					d.SetLineWidth(1.2)
					d.DrawCircle(ship.x, ship.y, reflectRadius*0.8)
					d.Stroke()
				}
			}
			// escudo permanente: anillo cian + barra de energía
			if pr.permShield > 0 {
				d.SetColor(color.NRGBA{R: 0x7a, G: 0xe0, B: 0xff, A: 0xcc})
				d.SetLineWidth(2 + shieldLv*0.8)
				d.DrawCircle(ship.x, ship.y, 20+shieldLv*1.5)
				d.Stroke()
				bw := 34.0
				frac := pr.permShield / permShieldCap
				if frac > 1 {
					frac = 1
				}
				bx, by := ship.x-bw/2, ship.y-32
				d.SetColor(color.NRGBA{R: 0x0d, G: 0x3d, B: 0x1c, A: 0xff})
				d.DrawRectangle(bx-1, by-3, bw+2, 6)
				d.Fill()
				d.SetColor(color.NRGBA{R: 0x7a, G: 0xe0, B: 0xff, A: 0xff})
				d.DrawRectangle(bx, by-2, bw*frac, 4)
				d.Fill()
			}
		}

		// compañero: nave aliada azul, misma silueta que la nuestra
		if pr.hasComp {
			c := pr.comp
			pts := [][2]float64{
				{c.x + math.Cos(c.angle)*15, c.y + math.Sin(c.angle)*15},
				{c.x + math.Cos(c.angle+2.5)*11, c.y + math.Sin(c.angle+2.5)*11},
				{c.x + math.Cos(c.angle+math.Pi)*5, c.y + math.Sin(c.angle+math.Pi)*5},
				{c.x + math.Cos(c.angle-2.5)*11, c.y + math.Sin(c.angle-2.5)*11},
			}
			glowPolyColor(d, pts, true, 2, 0x5a, 0x9a, 0xff)
			if c.invuln > 0 && int(c.invuln*10)%2 == 0 {
				d.SetColor(color.NRGBA{R: 0x9a, G: 0xc8, B: 0xff, A: 0xcc})
				d.SetLineWidth(1.6)
				d.DrawCircle(c.x, c.y, 20)
				d.Stroke()
			}
		}

		// asistentes rotatorios (bichos que giran y disparan)
		for _, o := range pr.orbs {
			ox := ship.x + math.Cos(o.ang)*orbRadius
			oy := ship.y + math.Sin(o.ang)*orbRadius
			d.SetColor(color.NRGBA{R: 0x6a, G: 0xd0, B: 0xff, A: 0x55})
			d.SetLineWidth(1)
			d.MoveTo(ship.x, ship.y)
			d.LineTo(ox, oy)
			d.Stroke()
			d.SetColor(color.NRGBA{R: 0x8f, G: 0xe0, B: 0xff, A: 0xff})
			d.DrawCircle(ox, oy, 5)
			d.Fill()
			d.SetColor(color.NRGBA{R: 0xdf, G: 0xf6, B: 0xff, A: 0xff})
			d.DrawCircle(ox, oy, 2.2)
			d.Fill()
			// resistencia restante del asistente (solo si ya ha recibido golpes)
			if o.hpMax > 0 && o.hp < o.hpMax {
				frac := float64(o.hp) / float64(o.hpMax)
				bw := 12.0
				d.SetColor(color.NRGBA{R: 0x0d, G: 0x3d, B: 0x1c, A: 0xff})
				d.DrawRectangle(ox-bw/2, oy+8, bw, 2)
				d.Fill()
				d.SetColor(color.NRGBA{R: 0x8f, G: 0xe0, B: 0xff, A: 0xff})
				d.DrawRectangle(ox-bw/2, oy+8, bw*frac, 2)
				d.Fill()
			}
		}
		for _, s := range pr.orbShots {
			d.SetColor(color.NRGBA{R: 0x9f, G: 0xe8, B: 0xff, A: 0xff})
			d.DrawCircle(s.x, s.y, 2.4)
			d.Fill()
		}
	}
	for _, p := range powerups {
		// los últimos segundos parpadean, para avisar de que se van
		if p.ttl < powerupBlink && int(p.ttl*6)%2 == 0 {
			continue
		}
		drawPowerupIcon(d, p.x, p.y, p.kind, p.spin)
	}

	// agujero negro: disco oscuro, halo de acreción y remolino giratorio
	if hasHole {
		hx, hy, hr := holeV.x, holeV.y, holeV.r
		for pass := 0; pass < 3; pass++ {
			var rr, al float64
			var lw float64
			switch pass {
			case 0:
				rr, al, lw = hr*2.3, 0.09, 4
			case 1:
				rr, al, lw = hr*1.6, 0.16, 2.6
			default:
				rr, al, lw = hr*1.15, 0.55, 1.8
			}
			d.SetColor(color.NRGBA{R: 0xa8, G: 0x55, B: 0xff, A: uint8(255 * al)})
			d.SetLineWidth(lw)
			d.DrawCircle(hx, hy, rr)
			d.Stroke()
		}
		d.SetColor(color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xff})
		d.DrawCircle(hx, hy, hr)
		d.Fill()
		for k := 0; k < 3; k++ {
			a := gameTime*1.6 + float64(k)*2*math.Pi/3
			d.SetColor(color.NRGBA{R: 0xd8, G: 0xb0, B: 0xff, A: 0xcc})
			d.SetLineWidth(1.6)
			d.MoveTo(hx+math.Cos(a)*hr*1.06, hy+math.Sin(a)*hr*1.06)
			for t := 1; t <= 10; t++ {
				f := float64(t) / 10
				aa := a + f*2.2
				rr := hr * (1.06 + f*1.15)
				d.LineTo(hx+math.Cos(aa)*rr, hy+math.Sin(aa)*rr)
			}
			d.Stroke()
		}
	}

	// HUD
	d.SetFont(18)
	small := func() {
		d.SetFont(14.3)
	}
	d.SetColor(color.NRGBA{R: 0x8f, G: 0xff, B: 0xb0, A: 0xff})
	d.DrawStringAnchored("SCORE "+formatScore(local.score), 16, 26, 0, 0.5)
	bLv := bestLevel
	if bLv < 1 {
		bLv = 1
	}
	maxTxt := fmt.Sprintf("MAX %s · LV %d", formatScore(best), bLv)
	if bestName != "" {
		maxTxt = fmt.Sprintf("MAX %s (%s) · LV %d", formatScore(best), bestName, bLv)
	}
	d.DrawStringAnchored(maxTxt, float64(pw)/2, 26, 0.5, 0.5)
	// cronómetro del nivel: en rojo en los últimos 30 s y parpadeando cuando
	// ya cuenta en negativo (el agujero negro está activo)
	clockCol := color.NRGBA{R: 0x8f, G: 0xff, B: 0xb0, A: 0xff}
	if timeLeft <= levelTimeWarn {
		clockCol = color.NRGBA{R: 0xff, G: 0x6a, B: 0x6a, A: 0xff}
	}
	if !fixedArena && !(timeLeft < 0 && int(gameTime*4)%2 == 0) {
		d.SetColor(clockCol)
		d.DrawStringAnchored(tr("TIME %s", formatClock(timeLeft)), float64(pw)/2, 48, 0.5, 0.5)
	}
	if pvpMode {
		small()
		d.SetColor(color.NRGBA{R: 0xff, G: 0x6a, B: 0x6a, A: 0xff})
		d.DrawStringAnchored("PVP", 16, 46, 0, 0.5)
	}
	if muted {
		small()
		d.SetColor(color.NRGBA{R: 0xff, G: 0xd0, B: 0x60, A: 0xff})
		d.DrawStringAnchored(tr("MUTE (X)"), 16, 66, 0, 0.5)
	}
	for i := 0; i < local.lives; i++ {
		x := float64(pw) - 26 - float64(i)*22
		glowPoly(d, [][2]float64{{x, 14}, {x - 6, 26}, {x + 6, 26}}, true, 1.6)
	}
	d.SetFont(22)
	// barra de escudo (3 s) / recarga (10 s)
	frac := 0.0
	if local.shieldT > 0 {
		frac = local.shieldT / shieldOnTime
	} else if local.shieldCd > 0 {
		frac = 1 - local.shieldCd/shieldReload
	}
	d.SetColor(color.NRGBA{R: 0x39, G: 0xff, B: 0x6a, A: 0x88})
	d.DrawRectangle(16, float64(ph)-26, 120*frac, 6)
	d.Fill()
	// barra de carga del railgun (la recarga depende de la fuerza del disparo)
	railFrac := 1.0
	if local.railRechargeTotal > 0 {
		railFrac = local.railCharge / local.railRechargeTotal
	}
	if railFrac > 1 {
		railFrac = 1
	}
	d.SetColor(color.NRGBA{R: 0xc0, G: 0x6a, B: 0xff, A: 0x99})
	d.DrawRectangle(16, float64(ph)-40, 120*railFrac, 6)
	d.Fill()
	small()
	d.SetColor(color.NRGBA{R: 0x8f, G: 0xff, B: 0xb0, A: 0xff})
	// nivel de escudo permanente (0..500 puntos) junto al estado del escudo manual
	shieldPts := int(local.permShield + 0.5)
	shieldLabel := tr("SHIELD %d/%d (S)", shieldPts, int(permShieldCap))
	if local.shieldT > 0 {
		shieldLabel = tr("SHIELD %d/%d · %.1fs", shieldPts, int(permShieldCap), local.shieldT)
	} else if local.shieldCd > 0 {
		shieldLabel = tr("SHIELD %d/%d reload %.1fs", shieldPts, int(permShieldCap), local.shieldCd)
	}
	d.DrawStringAnchored(shieldLabel, 16, float64(ph)-32, 0, 1)
	// casco: golpes que aguanta contra los bloques antes de destrozarse
	hullCol := color.NRGBA{R: 0x8f, G: 0xff, B: 0xb0, A: 0xff}
	if local.hull <= 1 {
		hullCol = color.NRGBA{R: 0xff, G: 0x6a, B: 0x6a, A: 0xff}
	}
	d.SetColor(hullCol)
	d.DrawStringAnchored(tr("HULL %d/%d", local.hull, shipHullMax), 16, float64(ph)-18, 0, 1)
	d.SetColor(color.NRGBA{R: 0x8f, G: 0xff, B: 0xb0, A: 0xff})
	railLabel := tr("RAILGUN %d%% (G)", int(railFrac*100))
	if local.railCharging {
		railLabel = tr("RAILGUN CHARGING %.0f%%", local.railHold/railHoldMax*100)
	} else if local.railCharge >= local.railRechargeTotal {
		railLabel = tr("RAILGUN READY (G)")
	}
	d.DrawStringAnchored(railLabel, 16, float64(ph)-46, 0, 1)
	d.DrawStringAnchored(tr("MISSILES %d (M)  ·  MINES %d (K)", local.missilesLeft, local.minesLeft), 16, float64(ph)-60, 0, 1)
	lethalLabel := tr("LETHAL RAY (L)")
	if local.lethalCd > 0 {
		lethalLabel = tr("LETHAL RAY %.1fs", local.lethalCd)
	}
	d.DrawStringAnchored(lethalLabel, 16, float64(ph)-74, 0, 1)
	d.DrawStringAnchored(tr("ASSISTANTS %d/%d", len(local.orbs), orbMax), 16, float64(ph)-88, 0, 1)
	if local.rapidFire > 0 {
		d.SetColor(color.NRGBA{R: 0xff, G: 0xe2, B: 0x60, A: 0xff})
		d.DrawStringAnchored(tr("RAPID FIRE x%d", 1<<uint(min(local.rapidFire, maxRapidFire))), 16, float64(ph)-102, 0, 1)
		d.SetColor(color.NRGBA{R: 0x8f, G: 0xff, B: 0xb0, A: 0xff})
	}
	// arma actual y si el compañero está en juego
	weapon := tr("WEAPON BULLETS")
	if local.laser {
		weapon = tr("WEAPON LASER")
	}
	if local.doubleShot {
		weapon += " ×2"
	}
	if local.hasComp {
		weapon += "  ·  " + tr("COMPANION")
	}
	d.DrawStringAnchored(weapon, 16, float64(ph)-116, 0, 1)
	if local.ship.teleCooldown > 0 {
		d.DrawStringAnchored(tr("TELEPORT %.1fs", local.ship.teleCooldown), float64(pw)-16, float64(ph)-34, 1, 1)
	} else {
		d.DrawStringAnchored(tr("TELEPORT  (ENTER)"), float64(pw)-16, float64(ph)-34, 1, 1)
	}
	d.DrawStringAnchored(tr("WASD move · SPACE fire · S shield · ENTER teleport · M/K/G/L weapons"), float64(pw)/2, float64(ph)-10, 0.5, 0.5)

	if paused {
		d.DrawStringAnchored(tr("PAUSED"), float64(pw)/2, float64(ph)/2, 0.5, 0.5)
	}
	// "Nivel XXX" al empezar la partida tras el hiperespacio
	if levelIntro > 0 {
		d.SetFont(46)
		d.SetColor(color.NRGBA{R: 0x8f, G: 0xff, B: 0xb0, A: 0xff})
		d.DrawStringAnchored(tr("Level %d", level), float64(pw)/2, float64(ph)*0.42, 0.5, 0.5)
		d.SetFont(18)
	}
	// fundido de entrada de la escena nueva
	if fadeIn > 0 {
		a := uint8(math.Min(255, 255*(fadeIn/0.9)))
		d.SetColor(color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: a})
		d.DrawRectangle(0, 0, float64(pw), float64(ph))
		d.Fill()
	}
	if over {
		d.SetFont(40)
		d.SetColor(color.NRGBA{R: 0xff, G: 0x6a, B: 0x6a, A: 0xff})
		d.DrawStringAnchored(tr("GAME OVER"), float64(pw)/2, float64(ph)*0.30, 0.5, 0.5)
		d.SetFont(18)
		d.SetColor(color.NRGBA{R: 0x8f, G: 0xff, B: 0xb0, A: 0xff})
		// En multijugador la partida no se reinicia desde aquí (manda el
		// servidor): R solo salta la tabla y vuelve a la sala de espera.
		hint := tr("press R to play again")
		if NetPlay {
			hint = tr("press R to go back to the lobby")
		}
		d.DrawStringAnchored(hint, float64(pw)/2, float64(ph)*0.30+26, 0.5, 0.5)
		// tabla de puntuaciones: quién ha hecho más muertes y cuántas veces ha muerto
		if len(players) > 0 {
			rows := append([]playerRender(nil), players...)
			sort.Slice(rows, func(i, j int) bool {
				if rows[i].kills != rows[j].kills {
					return rows[i].kills > rows[j].kills
				}
				return rows[i].score > rows[j].score
			})
			const rowH = 28.0
			const colW = 150.0
			const w0 = 380.0
			x0 := float64(pw)/2 - w0/2
			y0 := float64(ph) * 0.46
			h0 := rowH*float64(len(rows)+1) + 24
			// fondo casi negro y bien opaco, para que la tabla no se confunda con
			// el campo, más un borde marcado
			d.SetColor(color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xff})
			d.DrawRectangle(x0-20, y0-26, w0+40, h0+4)
			d.Fill()
			d.SetColor(color.NRGBA{R: 0x8f, G: 0xff, B: 0xb0, A: 0xff})
			d.SetLineWidth(3)
			d.DrawRectangle(x0-20, y0-26, w0+40, h0+4)
			d.Stroke()
			d.SetColor(color.NRGBA{R: 0x8f, G: 0xff, B: 0xb0, A: 0x66})
			d.SetLineWidth(1)
			d.DrawRectangle(x0-14, y0-20, w0+28, h0-8)
			d.Stroke()
			d.SetFont(18)
			// cabecera
			d.SetColor(color.NRGBA{R: 0x8f, G: 0xff, B: 0xb0, A: 0xcc})
			d.DrawStringAnchored(tr("PLAYER"), x0, y0, 0, 0.5)
			d.DrawStringAnchored(tr("KILLS"), x0+w0*0.72, y0, 0.5, 0.5)
			d.DrawStringAnchored(tr("DEATHS"), x0+w0, y0, 1, 0.5)
			for i, r2 := range rows {
				y := y0 + rowH*float64(i+1)
				cr, cg, cb := tintOf(r2.id)
				d.SetColor(color.NRGBA{R: cr, G: cg, B: cb, A: 0xff})
				d.DrawStringAnchored(r2.name, x0, y, 0, 0.5)
				d.DrawStringAnchored(strconv.Itoa(r2.kills), x0+w0*0.72, y, 0.5, 0.5)
				d.DrawStringAnchored(strconv.Itoa(r2.deaths), x0+w0, y, 1, 0.5)
			}
			d.SetColor(color.NRGBA{R: 0x8f, G: 0xff, B: 0xb0, A: 0xff})
			d.SetFont(22)
		}
	}
	if flash > 0 {
		d.SetColor(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: uint8(math.Min(90, flash*90))})
		d.DrawRectangle(0, 0, float64(pw), float64(ph))
		d.Fill()
	}
}
