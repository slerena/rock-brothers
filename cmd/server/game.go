package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"

	"rock-brothers/internal/proto"
	"rock-brothers/internal/sim"
)

// Servidor autoritativo del juego.
//
// El servidor es el único que simula: recibe el input de cada jugador, avanza la
// partida a 60 Hz y manda el estado (snapshot) a ~20 Hz. El cliente solo pinta lo
// que le llega, así que no se puede desincronizar ni hacer trampas.
//
// La presentación vive en el cliente (canvas del navegador); aquí no se dibuja ni
// se suena.

const (
	tickRate = 16 * time.Millisecond // 60 Hz (el dt fijo del juego)
	// El tick y el snapshot van juntos (60 Hz): el cliente pinta a 60 fps y, sin
	// interpolación todavía, mandar menos haría que el mundo fuera a tirones. El
	// siguiente paso para bajar ancho de banda es interpolar en el cliente y
	// volver a 20 Hz.
	maxPlayers = 8
	// El mundo lo fija el servidor: el MISMO campo para todos (justo para todos).
	// Se elige el doble del clásico (640x480) para que, escalado a una ventana
	// normal, las naves conserven su tamaño aparente.
	worldW      = sim.ArenaW * 2
	worldH      = sim.ArenaH * 2
	inputBuffer = 1024
	sendBuffer  = 32
)

// countdownSecs es la cuenta atrás desde que todos dicen "listo" hasta que
// empieza la partida (variable para poder acortarla en los tests).
var countdownSecs = 3.0

// overSecs es cuánto se queda la tabla de puntuaciones en pantalla al acabar la
// partida, antes de volver a la sala de espera (variable para los tests).
var overSecs = 12.0

// inputMsg es una tecla de un jugador concreto.
type inputMsg struct {
	player *sim.Player
	key    string
	down   bool
}

// client es una conexión de un navegador.
type client struct {
	conn   *websocket.Conn
	send   chan []byte
	id     int
	name   string
	player *sim.Player

	mu    sync.Mutex
	room  *room
	ready bool          // ha pulsado "listo para jugar"
	lag   time.Duration // ida y vuelta medido con ping/pong

	// pose de disparo que manda su cliente (se guarda aquí, sin tocar la
	// simulación, para no bloquear el lector del WebSocket)
	poseX, poseY, poseA float64
	hasPose             bool
}

func (c *client) trySend(b []byte) {
	select {
	case c.send <- b:
	default: // el cliente va lento: se descarta el frame antes que bloquear la sala
	}
}

func (c *client) writeLoop() {
	for b := range c.send {
		if err := websocket.Message.Send(c.conn, b); err != nil {
			return
		}
	}
}

// room es una partida en curso.
type room struct {
	mode    string
	sim     *sim.Sim
	inputs  chan inputMsg
	done    chan struct{} // se cierra al quedarse la sala vacía
	mu      sync.Mutex
	clients []*client // en orden de entrada
	// fase de la sala: se espera a que todos digan "listo", luego cuenta atrás y
	// por fin se juega (así todos empiezan a la vez, sincronizados)
	phase     string
	countdown float64
	arena     string        // tipo de arena: asteroides (rocks) o campo fijo (fixed)
	overUntil time.Time     // hasta cuándo se enseña la tabla del final
	lobby     *lobby        // para mover la sala al cambiar de tipo de partida
	pings     int           // para hacer ping cada segundo
	tickN     int           // para mandar snapshot cada dos ticks (30 Hz)
	bots      []*sim.Player // bots de pruebas (los pilota el servidor)
	botN      int           // numeración de los bots
	// sonidos disparados desde el último snapshot
	soundMu sync.Mutex // protege sounds (lo escribe Step, lo lee broadcast)
	sounds  []proto.Sound
	closed  bool
}

func newRoom(mode string) *room {
	g := sim.NewRoom()        // sin jugadores: entra uno por cliente
	g.SetSize(worldW, worldH) // el servidor fija el mundo: no hay ventana que medir
	// El récord es del servidor (y se persiste): cada sala arranca con él y avisa
	// cuando se supera.
	g.Best, g.BestLevel, g.BestName = currentBest()
	g.OnBest = saveBest
	r := &room{
		mode:   mode,
		sim:    g,
		inputs: make(chan inputMsg, inputBuffer),
		done:   make(chan struct{}),
		phase:  proto.PhaseLobby,
		arena:  sim.ArenaRocks,
	}
	g.SetArena(sim.ArenaRocks)
	// Los sonidos los decide la simulación, que corre aquí: se capturan y se
	// mandan al cliente para que los reproduzca él (el servidor no tiene audio).
	g.OnSound = r.captureSound
	go r.run()
	return r
}

// stop para la sala. Se llama cuando se queda sin jugadores: así la siguiente
// partida empieza de cero en vez de arrastrar rocas, niveles y naves de la
// anterior.
func (r *room) stop() {
	r.mu.Lock()
	closed := r.closed
	r.closed = true
	r.mu.Unlock()
	if !closed {
		close(r.done)
	}
}

// captureSound guarda un sonido para el próximo snapshot.
func (r *room) captureSound(name string, gap time.Duration) {
	r.soundMu.Lock()
	if len(r.sounds) < 64 {
		r.sounds = append(r.sounds, proto.Sound{N: name, G: int(gap / time.Millisecond)})
	}
	r.soundMu.Unlock()
}

// takeSounds se lleva los sonidos acumulados (y vacía la lista).
func (r *room) takeSounds() []proto.Sound {
	r.soundMu.Lock()
	s := r.sounds
	r.sounds = nil
	r.soundMu.Unlock()
	return s
}

func (r *room) run() {
	tick := time.NewTicker(tickRate)
	slow := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	defer slow.Stop()
	for {
		select {
		case <-r.done:
			return
		case in := <-r.inputs:
			r.applyInput(in)
		case <-tick.C:
			r.tickOnce()
		case <-slow.C:
			r.slowTick()
		}
	}
}

// tickOnce avanza el mundo (solo si se está jugando) y manda el snapshot.
// En la sala de espera no se simula: se manda el estado congelado para que se
// vea la arena mientras todos dicen "listo".
func (r *room) tickOnce() {
	if r.phaseNow() == proto.PhasePlaying {
		r.applyPoses()
		r.driveBots()
		// vaciar la cola de input antes de avanzar el mundo
		for drained := false; !drained; {
			select {
			case in := <-r.inputs:
				r.applyInput(in)
			default:
				drained = true
			}
		}
		r.sim.Step(0.016)
	}
	// Snapshot cada dos ticks (30 Hz): la mitad de ancho de banda y, con el
	// cliente extrapolando entre snapshots, se ve igual de fino.
	r.tickN++
	if r.tickN%2 == 0 {
		r.broadcast()
	}
}

// slowTick manda el estado de la sala (fase, cuenta atrás y jugadores con su
// "listo" y su lag) cada 200 ms, hace ping cada segundo y lleva la cuenta atrás.
func (r *room) slowTick() {
	// OJO con el orden de los locks: la simulación y la sala tienen cada una el
	// suyo, y broadcast() coge primero el de la simulación. Aquí hay que leer la
	// simulación ANTES de coger el de la sala, o se puede producir un interbloqueo
	// (y la sala se queda colgada justo al acabar la partida).
	over := r.sim.Over()

	r.mu.Lock()
	if r.phase == proto.PhasePlaying && over {
		// se acabó: se deja unos segundos para ver la tabla de puntuaciones y
		// luego se vuelve a la sala de espera
		r.phase = proto.PhaseOver
		r.overUntil = time.Now().Add(time.Duration(overSecs * float64(time.Second)))
		log.Printf("sala %s: fin de partida (tabla %v s)", r.mode, overSecs)
	}
	if r.phase == proto.PhaseOver && time.Now().After(r.overUntil) {
		r.phase = proto.PhaseLobby
		r.countdown = 0
		for _, c := range r.clients {
			c.ready = false
		}
	}
	start := false
	if r.phase == proto.PhaseCountdown {
		r.countdown -= 0.2
		if r.countdown <= 0 {
			r.countdown = 0
			r.phase = proto.PhasePlaying
			start = true
		}
	}
	mode, arena := r.mode, r.arena
	phase, cd := r.phase, r.countdown
	players := r.rosterLocked()
	r.pings++
	doPing := r.pings%5 == 0 // cada segundo
	cs := append([]*client(nil), r.clients...)
	r.mu.Unlock()

	if start {
		// la simulación se toca fuera del lock de la sala (ver nota de arriba)
		r.sim.SetPvP(mode == proto.ModePvP)
		r.sim.SetArena(arena)
		r.sim.Restart()
		log.Printf("sala %s: ¡empieza la partida!", mode)
	}

	r.sendState(phase, cd, players)
	if doPing {
		t := time.Now().UnixNano()
		frame := proto.JSON(proto.MsgPing, proto.Ping{T: t})
		for _, c := range cs {
			c.trySend(frame)
		}
	}
}

// phaseNow devuelve la fase actual de la sala.
func (r *room) phaseNow() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.phase
}

// sendState manda a todos la fase de la sala y quiénes están.
func (r *room) sendState(phase string, countdown float64, players []proto.PlayerInfo) {
	// el modo y la arena se leen aquí para no pasarlos por parámetro en cada sitio
	mode, arena := r.phaseAndMode()
	frame := proto.JSON(proto.MsgState, proto.State{
		Phase:     phase,
		Mode:      mode,
		Arena:     arena,
		Countdown: countdown,
		Players:   players,
	})
	r.mu.Lock()
	cs := append([]*client(nil), r.clients...)
	r.mu.Unlock()
	for _, c := range cs {
		c.trySend(frame)
	}
}

// setReady apunta que un jugador está listo (o deja de estarlo) y, si ya lo
// están todos, arranca la cuenta atrás.
func (r *room) setReady(c *client, ready bool) {
	r.mu.Lock()
	c.ready = ready
	allReady := len(r.clients) > 0
	for _, x := range r.clients {
		if !x.ready {
			allReady = false
			break
		}
	}
	if r.phase != proto.PhasePlaying {
		if allReady {
			if r.phase != proto.PhaseCountdown {
				r.phase = proto.PhaseCountdown
				r.countdown = countdownSecs
			}
		} else {
			r.phase = proto.PhaseLobby
			r.countdown = 0
		}
	}
	phase, cd := r.phase, r.countdown
	players := r.rosterLocked()
	r.mu.Unlock()
	log.Printf("sala %s: jugador %d %s listo (%d jugadores)", r.mode, c.id, map[bool]string{true: "está", false: "ya no está"}[ready], len(players))
	r.sendState(phase, cd, players)
}

// setArena cambia la arena de la sala (asteroides que se mueven o campo fijo con
// estructuras). Como en el modo, al cambiar las reglas todos vuelven a "no
// listo".
func (r *room) setArena(c *client, arena string) {
	if arena != sim.ArenaFixed {
		arena = sim.ArenaRocks
	}
	r.mu.Lock()
	if r.arena == arena {
		r.mu.Unlock()
		return
	}
	r.arena = arena
	for _, x := range r.clients {
		x.ready = false
	}
	r.phase = proto.PhaseLobby
	r.countdown = 0
	phase, cd := r.phase, r.countdown
	players := r.rosterLocked()
	r.mu.Unlock()
	r.sim.SetArena(arena)
	log.Printf("sala %s: arena → %s", r.mode, arena)
	r.sendState(phase, cd, players)
}

// setLag guarda la latencia medida con el ping/pong. Además ajusta el adelanto
// de la pose al disparar de ese jugador: el cliente manda su pose con medio
// viaje de retraso, así que se adelanta esa mitad (más el pico de un tick) para
// que la bala salga justo por el morro. En localhost el retardo es casi cero y
// adelantar de más desalineaba el disparo.
func (r *room) setLag(c *client, rtt time.Duration) {
	r.mu.Lock()
	c.lag = rtt
	p := c.player
	r.mu.Unlock()
	if p != nil {
		lead := rtt.Seconds()/2 + 0.016
		if lead > 0.09 {
			lead = 0.09
		}
		r.sim.SetAimLead(p, lead)
	}
}

// setName cambia el nombre visible de un jugador (se edita en la sala de espera).
func (r *room) setName(c *client, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if len(name) > 16 {
		name = name[:16]
	}
	r.sim.SetPlayerName(c.player, name)
	r.mu.Lock()
	c.name = name
	phase, cd := r.phase, r.countdown
	players := r.rosterLocked()
	r.mu.Unlock()
	log.Printf("sala %s: el jugador %d ahora se llama %q", r.mode, c.id, name)
	r.sendState(phase, cd, players)
}

// setMode cambia el tipo de partida de la sala (cooperativo / todos contra
// todos). Lo puede cambiar cualquiera desde la sala de espera; como cambian las
// reglas, todos vuelven a "no listo" y la sala se mueve a la lista del modo
// nuevo (para que los que entren después caigan donde toca).
func (r *room) setMode(c *client, mode string) {
	if mode != proto.ModePvP {
		mode = proto.ModeCoop
	}
	r.mu.Lock()
	if r.mode == mode {
		r.mu.Unlock()
		return
	}
	old := r.mode
	r.mode = mode
	for _, x := range r.clients {
		x.ready = false
	}
	r.phase = proto.PhaseLobby
	r.countdown = 0
	phase, cd := r.phase, r.countdown
	players := r.rosterLocked()
	l := r.lobby
	r.mu.Unlock()

	r.sim.SetPvP(mode == proto.ModePvP)
	if l != nil {
		l.remode(r, old, mode)
	}
	log.Printf("sala %s → %s: el jugador %d cambia el tipo de partida", old, mode, c.id)
	r.sendState(phase, cd, players)
}

func (r *room) applyInput(in inputMsg) {
	if in.player == nil {
		return
	}
	// El silencio (X) es de cada cliente: si el servidor lo aplicara, callaría la
	// partida a todos. Así que aquí se ignora.
	if in.key == sim.KeyMute {
		return
	}
	// "R" (volver a jugar) NO puede llegar a la simulación del servidor: la
	// reiniciaría y la sala se quedaba con una partida nueva a medias que nadie
	// veía (sonaba el arranque y luego se volvía a la sala). En la tabla del
	// final, R sirve para saltarla y volver ya a la sala de espera.
	if in.key == "R" {
		if in.down {
			r.skipOver()
		}
		return
	}
	if in.down {
		r.sim.KeyDownFor(in.player, in.key)
		return
	}
	r.sim.KeyUpFor(in.player, in.key)
}

// skipOver salta lo que queda de la tabla del final y devuelve la sala a la
// espera, para poder jugar otra partida sin aguantar los 12 s.
func (r *room) skipOver() {
	r.mu.Lock()
	if r.phase != proto.PhaseOver {
		r.mu.Unlock()
		return
	}
	r.phase = proto.PhaseLobby
	r.countdown = 0
	for _, c := range r.clients {
		c.ready = false
	}
	phase, cd := r.phase, r.countdown
	players := r.rosterLocked()
	r.mu.Unlock()
	log.Printf("sala %s: se salta la tabla (R)", r.mode)
	r.sendState(phase, cd, players)
}

// rosterLocked devuelve la lista de jugadores (con el "listo" y el lag de cada
// uno). El llamador debe tener r.mu tomado.
func (r *room) rosterLocked() []proto.PlayerInfo {
	out := make([]proto.PlayerInfo, 0, len(r.clients)+len(r.bots))
	for _, c := range r.clients {
		out = append(out, proto.PlayerInfo{
			ID:    c.id,
			Name:  c.name,
			Ready: c.ready,
			Lag:   int(c.lag / time.Millisecond),
		})
	}
	// los bots salen en la lista (siempre "listos" y sin lag)
	for _, b := range r.bots {
		out = append(out, proto.PlayerInfo{ID: b.ID, Name: b.Name, Ready: true, Lag: -1})
	}
	return out
}

// addBot mete un bot de pruebas en la sala (para poder probar el PvP sin gente).
func (r *room) addBot(level int) {
	r.mu.Lock()
	if len(r.bots) >= maxPlayers-1 || len(r.clients)+len(r.bots) >= maxPlayers {
		r.mu.Unlock()
		return
	}
	r.botN++
	name := fmt.Sprintf("Bot %d", r.botN)
	r.mu.Unlock()

	// el jugador se crea en la simulación fuera del lock de la sala (orden de
	// locks: simulación → sala, igual que en broadcast)
	p := r.sim.AddPlayer(name)
	r.sim.SetBotLevel(p, level)

	r.mu.Lock()
	r.bots = append(r.bots, p)
	phase, cd := r.phase, r.countdown
	players := r.rosterLocked()
	mode := r.mode
	r.mu.Unlock()
	log.Printf("sala %s: entra %q (bot, nivel %d)", mode, name, level)
	r.sendState(phase, cd, players)
}

// removeBot saca el último bot de la sala.
func (r *room) removeBot() {
	r.mu.Lock()
	if len(r.bots) == 0 {
		r.mu.Unlock()
		return
	}
	p := r.bots[len(r.bots)-1]
	r.bots = r.bots[:len(r.bots)-1]
	phase, cd := r.phase, r.countdown
	players := r.rosterLocked()
	r.mu.Unlock()
	r.sim.RemovePlayer(p)
	log.Printf("sala %s: sale el bot %d", r.mode, p.ID)
	r.sendState(phase, cd, players)
}

// driveBots pilota a los bots: se les aprietan las teclas y la simulación hace el
// resto (juegan exactamente igual que una persona).
func (r *room) driveBots() {
	r.mu.Lock()
	bots := append([]*sim.Player(nil), r.bots...)
	r.mu.Unlock()
	for _, b := range bots {
		left, right, thrust, fire := r.sim.BotInput(b)
		r.sim.SetKey(b, sim.KeyA, left)
		r.sim.SetKey(b, sim.KeyD, right)
		r.sim.SetKey(b, sim.KeyW, thrust)
		r.sim.SetKey(b, sim.KeySpace, fire)
		// y de vez en cuando sus trastos: minas, misiles, railgun y escudo
		for _, act := range r.sim.BotActions(b) {
			if strings.HasPrefix(act, "-") {
				r.sim.KeyUpFor(b, act[1:])
				continue
			}
			r.sim.KeyDownFor(b, act)
		}
	}
}

// roster devuelve la lista de jugadores de la sala.
func (r *room) roster() []proto.PlayerInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rosterLocked()
}

func (r *room) broadcast() {
	// los sonidos primero: el cliente los reproduce y luego aplica el estado
	if s := r.takeSounds(); len(s) > 0 {
		frame := proto.JSON(proto.MsgSounds, proto.Sounds{List: s})
		r.mu.Lock()
		cs := append([]*client(nil), r.clients...)
		r.mu.Unlock()
		for _, c := range cs {
			c.trySend(frame)
		}
	}

	frame := proto.Frame(proto.MsgSnapshot, r.sim.Snapshot())
	r.mu.Lock()
	cs := append([]*client(nil), r.clients...)
	r.mu.Unlock()
	for _, c := range cs {
		c.trySend(frame)
	}
}

func (r *room) full() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.clients) >= maxPlayers
}

func (r *room) empty() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.clients) == 0
}

// join mete al cliente en la sala (le da un jugador en la simulación) y le manda
// el hello.
func (r *room) join(c *client, name string) {
	// El ID del jugador en la simulación es el del cliente: así el input se
	// enruta sin ambigüedad aunque alguien se vaya.
	p := r.sim.AddPlayer(name)
	c.id = p.ID
	c.player = p
	c.name = name
	r.mu.Lock()
	r.clients = append(r.clients, c)
	// alguien nuevo (que no está listo) deshace la cuenta atrás
	if r.phase == proto.PhaseCountdown {
		r.phase = proto.PhaseLobby
		r.countdown = 0
	}
	phase, cd := r.phase, r.countdown
	players := r.rosterLocked()
	r.mu.Unlock()

	c.trySend(proto.JSON(proto.MsgHello, proto.Hello{
		You:     c.id,
		Mode:    r.mode,
		Players: players,
	}))
	log.Printf("sala %s: entra %q como jugador %d", r.mode, name, c.id)
	r.sendState(phase, cd, players)
}

// leave saca al cliente de la sala. El jugador se queda en la simulación (su
// nave se queda quieta): en esta versión no hay reconexión ni expulsión.
func (r *room) leave(c *client) {
	r.mu.Lock()
	for i, x := range r.clients {
		if x == c {
			r.clients = append(r.clients[:i], r.clients[i+1:]...)
			break
		}
	}
	empty := len(r.clients) == 0
	// si se va alguien durante la cuenta atrás, se cancela
	if !empty && r.phase == proto.PhaseCountdown {
		r.phase = proto.PhaseLobby
		r.countdown = 0
	}
	phase, cd := r.phase, r.countdown
	players := r.rosterLocked()
	r.mu.Unlock()
	r.sim.RemovePlayer(c.player)
	close(c.send)
	log.Printf("sala %s: sale el jugador %d", r.mode, c.id)
	if !empty {
		r.sendState(phase, cd, players)
	}
}

// setPose guarda la pose que manda el cliente. No toca la simulación: eso se hace
// en el tick de la sala, para que el lector del WebSocket nunca se quede
// esperando al lock de la simulación (si no, los inputs llegan tarde o se caen).
func (r *room) setPose(c *client, x, y, a float64) {
	c.mu.Lock()
	c.poseX, c.poseY, c.poseA, c.hasPose = x, y, a, true
	c.mu.Unlock()
}

// applyPoses pasa las poses de disparo a la simulación (una vez por tick).
func (r *room) applyPoses() {
	r.mu.Lock()
	cs := append([]*client(nil), r.clients...)
	r.mu.Unlock()
	for _, c := range cs {
		c.mu.Lock()
		x, y, a, ok := c.poseX, c.poseY, c.poseA, c.hasPose
		c.mu.Unlock()
		if ok {
			r.sim.SetAimPose(c.player, x, y, a)
		}
	}
}

// pushInput encola una tecla del cliente.
func (r *room) pushInput(c *client, in proto.Input) {
	select {
	case r.inputs <- inputMsg{player: c.player, key: in.Key, down: in.Down}:
	default: // a tope: se pierde la tecla antes que bloquear
	}
}

// lobby reparte a los clientes en salas por modo.
type lobby struct {
	mu    sync.Mutex
	rooms map[string][]*room // modo → salas
}

func newLobby() *lobby { return &lobby{rooms: map[string][]*room{}} }

// find devuelve la primera sala del modo con hueco, creando una si hace falta.
func (l *lobby) find(mode string) *room {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.rooms[mode] {
		if !r.full() {
			return r
		}
	}
	r := newRoom(mode)
	r.lobby = l
	l.rooms[mode] = append(l.rooms[mode], r)
	return r
}

// remove saca una sala terminada de la lista.
// remode mueve una sala de la lista de un modo a la del otro.
func (l *lobby) remode(r *room, from, to string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rs := l.rooms[from]
	for i, x := range rs {
		if x == r {
			l.rooms[from] = append(rs[:i], rs[i+1:]...)
			break
		}
	}
	l.rooms[to] = append(l.rooms[to], r)
}

func (l *lobby) remove(r *room) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rs := l.rooms[r.mode]
	for i, x := range rs {
		if x == r {
			l.rooms[r.mode] = append(rs[:i], rs[i+1:]...)
			break
		}
	}
}

func wsHandler(l *lobby) http.Handler {
	return websocket.Handler(func(ws *websocket.Conn) {
		var raw []byte
		if err := websocket.Message.Receive(ws, &raw); err != nil {
			return
		}
		if len(raw) == 0 || raw[0] != proto.MsgJoin {
			_ = websocket.Message.Send(ws, proto.JSON(proto.MsgBye, proto.Bye{Reason: "falta el join"}))
			return
		}
		var j proto.Join
		if err := json.Unmarshal(raw[1:], &j); err != nil {
			_ = websocket.Message.Send(ws, proto.JSON(proto.MsgBye, proto.Bye{Reason: "join ilegible"}))
			return
		}
		if j.Mode != proto.ModePvP {
			j.Mode = proto.ModeCoop
		}
		if j.Name == "" {
			j.Name = "jugador"
		}
		c := &client{conn: ws, send: make(chan []byte, sendBuffer), name: j.Name}
		r := l.find(j.Mode)
		r.join(c, j.Name)
		go c.writeLoop()
		defer func() {
			r.leave(c)
			// sala vacía: se para y se tira, para que la próxima partida empiece
			// limpia (si no, arrastraría rocas y niveles de la anterior)
			if r.empty() {
				r.stop()
				l.remove(r)
			}
		}()

		// bucle de lectura: inputs, "listo" y respuestas al ping
		for {
			if err := websocket.Message.Receive(ws, &raw); err != nil {
				return
			}
			if len(raw) < 2 {
				continue
			}
			switch raw[0] {
			case proto.MsgInput:
				var in proto.Input
				if json.Unmarshal(raw[1:], &in) == nil && in.Key != "" {
					r.pushInput(c, in)
				}
			case proto.MsgReady:
				var rd proto.Ready
				if json.Unmarshal(raw[1:], &rd) == nil {
					r.setReady(c, rd.Ready)
				}
			case proto.MsgPong:
				var pg proto.Ping
				if json.Unmarshal(raw[1:], &pg) == nil {
					r.setLag(c, time.Since(time.Unix(0, pg.T)))
				}
			case proto.MsgName:
				var nm proto.Name
				if json.Unmarshal(raw[1:], &nm) == nil {
					r.setName(c, nm.Name)
				}
			case proto.MsgMode:
				var md proto.Mode
				if json.Unmarshal(raw[1:], &md) == nil {
					r.setMode(c, md.Mode)
				}
			case proto.MsgBot:
				var bt proto.Bot
				if json.Unmarshal(raw[1:], &bt) == nil {
					if bt.Add {
						r.addBot(bt.Level)
					} else {
						r.removeBot()
					}
				}
			case proto.MsgPose:
				var ps proto.Pose
				if json.Unmarshal(raw[1:], &ps) == nil {
					r.setPose(c, ps.X, ps.Y, ps.A)
				}
			case proto.MsgArena:
				var ar proto.Arena
				if json.Unmarshal(raw[1:], &ar) == nil {
					r.setArena(c, ar.Arena)
				}
			}
		}
	})
}

// phaseAndMode devuelve el modo y la arena de la sala.
func (r *room) phaseAndMode() (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.mode, r.arena
}
