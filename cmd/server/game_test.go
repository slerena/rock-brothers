package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"rock-brothers/internal/proto"
	"rock-brothers/internal/sim"
)

// dial conecta un cliente de prueba y manda el join.
func dial(t *testing.T, url, name, mode string) *websocket.Conn {
	t.Helper()
	ws, err := websocket.Dial(url, "", url)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := websocket.Message.Send(ws, proto.JSON(proto.MsgJoin, proto.Join{Name: name, Mode: mode})); err != nil {
		t.Fatalf("join: %v", err)
	}
	return ws
}

// recv lee el siguiente mensaje y devuelve (tipo, carga).
func recv(t *testing.T, ws *websocket.Conn) (byte, []byte) {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	var raw []byte
	if err := websocket.Message.Receive(ws, &raw); err != nil {
		t.Fatalf("recv: %v", err)
	}
	if len(raw) == 0 {
		t.Fatalf("mensaje vacío")
	}
	return raw[0], raw[1:]
}

// mustPlay pone la sala en marcha: manda "listo" por cada cliente y espera a que
// el servidor diga que ya se juega (la cuenta atrás se acorta para el test).
func mustPlay(t *testing.T, cs ...*websocket.Conn) {
	t.Helper()
	old := countdownSecs
	countdownSecs = 0.2
	t.Cleanup(func() { countdownSecs = old })

	for _, c := range cs {
		if err := websocket.Message.Send(c, proto.JSON(proto.MsgReady, proto.Ready{Ready: true})); err != nil {
			t.Fatalf("ready: %v", err)
		}
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		typ, pay := recv(t, cs[0])
		if typ != proto.MsgState {
			continue
		}
		var st proto.State
		if json.Unmarshal(pay, &st) != nil {
			continue
		}
		if st.Phase == proto.PhasePlaying {
			return
		}
	}
	t.Fatal("la partida no empezó tras decir todos que están listos")
}

// TestTwoPlayersShareArena comprueba lo importante del servidor autoritativo:
// dos clientes entran en la misma sala, cada uno recibe su "hello" con un
// jugador distinto, los dos se ponen listos (con cuenta atrás) y los snapshots
// traen a LOS DOS jugadores y el movimiento del que manda input.
func TestTwoPlayersShareArena(t *testing.T) {
	srv := httptest.NewServer(wsHandler(newLobby()))
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	c1 := dial(t, url, "ana", proto.ModeCoop)
	defer c1.Close()
	// El hello del primero puede llegar después del roster del segundo, así que
	// se busca el tipo que toque en cada caso.
	if typ, pay := recvUntil(t, c1, proto.MsgHello); typ == proto.MsgHello {
		var h proto.Hello
		if err := json.Unmarshal(pay, &h); err != nil {
			t.Fatalf("hello 1: %v", err)
		}
		if h.You != 0 || len(h.Players) != 1 {
			t.Fatalf("hello 1: you=%d players=%d (esperado 0 y 1)", h.You, len(h.Players))
		}
	}

	c2 := dial(t, url, "bob", proto.ModeCoop)
	defer c2.Close()
	typ, pay := recvUntil(t, c2, proto.MsgHello)
	if typ != proto.MsgHello {
		t.Fatalf("hello 2: primer mensaje tipo %d", typ)
	}
	var h2 proto.Hello
	if err := json.Unmarshal(pay, &h2); err != nil {
		t.Fatalf("hello 2: %v", err)
	}
	if h2.You != 1 || len(h2.Players) != 2 {
		t.Fatalf("hello 2: you=%d players=%d (esperado 1 y 2)", h2.You, len(h2.Players))
	}

	// los dos listos: la partida arranca con cuenta atrás
	mustPlay(t, c1, c2)

	// bob mueve su nave hacia arriba
	if err := websocket.Message.Send(c2, proto.JSON(proto.MsgInput, proto.Input{Key: sim.KeyUp, Down: true})); err != nil {
		t.Fatalf("input: %v", err)
	}

	// de c1 tienen que llegar snapshots con 2 jugadores, y bob moviéndose
	deadline := time.Now().Add(3 * time.Second)
	var first, last [2][2]float64
	got := 0
	for time.Now().Before(deadline) && got < 8 {
		typ, pay := recv(t, c1)
		if typ != proto.MsgSnapshot {
			continue
		}
		mirror := sim.New()
		if err := mirror.Load(pay); err != nil {
			t.Fatalf("load: %v", err)
		}
		ps := mirror.Players()
		if len(ps) != 2 {
			t.Fatalf("el snapshot trae %d jugadores, esperado 2", len(ps))
		}
		for i, p := range ps {
			x, y := p.Pos()
			if got == 0 {
				first[i] = [2]float64{x, y}
			}
			last[i] = [2]float64{x, y}
		}
		got++
	}
	if got == 0 {
		t.Fatal("no llegó ningún snapshot")
	}
	if last[1] == first[1] {
		t.Errorf("el jugador 1 no se movió con \"Up\": %v", last[1])
	}
	if last[0] != first[0] {
		t.Errorf("el jugador 0 no debería moverse: %v -> %v", first[0], last[0])
	}
}

// recvUntil lee hasta encontrar el tipo pedido (o falla).
func recvUntil(t *testing.T, ws *websocket.Conn, want byte) (byte, []byte) {
	t.Helper()
	for i := 0; i < 8; i++ {
		typ, pay := recv(t, ws)
		if typ == want {
			return typ, pay
		}
	}
	t.Fatalf("no llegó el mensaje tipo %d", want)
	return 0, nil
}

// TestSoundsAreRelayed: la simulación corre en el servidor (sin audio), así que
// los sonidos tienen que viajar al cliente. Al disparar suena algo, y ese sonido
// tiene que llegar como MsgSounds.
func TestSoundsAreRelayed(t *testing.T) {
	srv := httptest.NewServer(wsHandler(newLobby()))
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	c := dial(t, url, "ana", proto.ModeCoop)
	defer c.Close()
	recvUntil(t, c, proto.MsgHello)
	mustPlay(t, c)

	// mantener el disparo
	if err := websocket.Message.Send(c, proto.JSON(proto.MsgInput, proto.Input{Key: sim.KeySpace, Down: true})); err != nil {
		t.Fatalf("input: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		typ, pay := recv(t, c)
		if typ != proto.MsgSounds {
			continue
		}
		var s proto.Sounds
		if err := json.Unmarshal(pay, &s); err != nil {
			t.Fatalf("sounds: %v", err)
		}
		if len(s.List) == 0 {
			t.Fatal("MsgSounds vacío")
		}
		for _, snd := range s.List {
			if snd.N == "" {
				t.Errorf("sonido sin nombre: %+v", snd)
			}
		}
		return // ✅
	}
	t.Fatal("no llegó ningún MsgSounds al disparar")
}

// TestFaseFinal: al acabar la partida la sala se queda en fase "over" para que se
// vea la tabla de puntuaciones, y solo después vuelve a la sala de espera.
func TestFaseFinal(t *testing.T) {
	oldOver, oldCount := overSecs, countdownSecs
	overSecs, countdownSecs = 0.3, 0.1
	defer func() { overSecs, countdownSecs = oldOver, oldCount }()

	r := newRoom(proto.ModePvP)
	defer r.stop()
	r.sim.SetPvP(true)
	a := r.sim.AddPlayer("ana")
	b := r.sim.AddPlayer("bob")
	r.phase = proto.PhasePlaying
	// se les acaban las vidas a los dos (5 cada uno y uno más)
	for i := 0; i < 6; i++ {
		r.sim.KillPlayer(a)
	}
	for i := 0; i < 6; i++ {
		r.sim.KillPlayer(b)
	}

	if !r.sim.Over() {
		t.Fatal("la partida debería haber acabado")
	}
	waitPhase(t, r, proto.PhaseOver, "tabla de puntuaciones")
	waitPhase(t, r, proto.PhaseLobby, "sala de espera")
}

func waitPhase(t *testing.T, r *room, want, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if r.phaseNow() == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("la sala debería estar en %q (%s), está en %q", want, what, r.phaseNow())
}

// TestBots: el servidor puede meter bots de pruebas, salen en la lista y se
// mueven solos (el piloto automático va dentro de la simulación).
func TestBots(t *testing.T) {
	r := newRoom(proto.ModePvP)
	defer r.stop()
	r.phase = proto.PhasePlaying
	if len(r.sim.Players()) != 0 {
		t.Fatalf("la sala debería empezar sin jugadores")
	}
	r.addBot(sim.BotNormal)
	r.addBot(sim.BotNormal)
	if len(r.sim.Players()) != 2 {
		t.Fatalf("deberían estar los dos bots, hay %d", len(r.sim.Players()))
	}
	roster := r.roster()
	if len(roster) != 2 || roster[0].Name == "" || !roster[0].Ready {
		t.Errorf("los bots deben salir en la lista y estar listos: %+v", roster)
	}

	// se pilotan solos: en unos segundos tienen que haberse movido
	p := r.sim.Players()[0]
	x0, y0 := p.Pos()
	for i := 0; i < 300; i++ {
		r.driveBots()
		r.sim.Step(0.016)
	}
	x1, y1 := p.Pos()
	if x1 == x0 && y1 == y0 {
		t.Errorf("el bot no se ha movido de (%.0f,%.0f)", x0, y0)
	}

	r.removeBot()
	if len(r.sim.Players()) != 1 {
		t.Errorf("al quitar un bot debe quedar uno, hay %d", len(r.sim.Players()))
	}
}

// TestRSaltaLaTabla: en multijugador, R no puede reiniciar la simulación del
// servidor (dejaba una partida nueva a medias que nadie veía). Durante la tabla
// del final, R lo que hace es saltársela y volver a la sala de espera.
func TestRSaltaLaTabla(t *testing.T) {
	r := newRoom(proto.ModePvP)
	defer r.stop()
	r.sim.SetPvP(true)
	a := r.sim.AddPlayer("ana")
	b := r.sim.AddPlayer("bob")
	r.phase = proto.PhasePlaying
	for i := 0; i < 6; i++ {
		r.sim.KillPlayer(a)
		r.sim.KillPlayer(b)
	}
	if !r.sim.Over() {
		t.Fatal("la partida debería haber acabado")
	}
	waitPhase(t, r, proto.PhaseOver, "tabla de puntuaciones")

	// R durante la tabla: vuelve a la sala, pero NO reinicia la partida
	r.applyInput(inputMsg{player: a, key: "R", down: true})
	waitPhase(t, r, proto.PhaseLobby, "sala de espera tras pulsar R")
	if !r.sim.Over() {
		t.Error("R no debe reiniciar la partida del servidor")
	}

	// y R durante la partida no reinicia nada tampoco
	r2 := newRoom(proto.ModePvP)
	defer r2.stop()
	r2.sim.SetPvP(true)
	p := r2.sim.AddPlayer("ana")
	r2.sim.AddPlayer("bob")
	r2.phase = proto.PhasePlaying
	r2.sim.Step(0.016)
	r2.applyInput(inputMsg{player: p, key: "R", down: true})
	if r2.sim.Over() {
		t.Error("R durante la partida no debe acabar la partida")
	}
}

// TestLimiteDeJugadores: la sala admite hasta 8 jugadores (antes 4) y se pueden
// meter bots hasta llenarla, para probar partidas gordas.
func TestLimiteDeJugadores(t *testing.T) {
	if maxPlayers != 8 {
		t.Fatalf("el límite de jugadores debe ser 8, es %d", maxPlayers)
	}
	r := newRoom(proto.ModePvP)
	defer r.stop()
	for i := 0; i < maxPlayers+4; i++ { // se piden más de los que caben
		r.addBot(0)
	}
	r.mu.Lock()
	n := len(r.bots)
	r.mu.Unlock()
	if n != maxPlayers-1 {
		t.Errorf("deberían caber %d bots, caben %d", maxPlayers-1, n)
	}
	// la simulación tiene un jugador por bot
	if got := len(r.sim.Players()); got != maxPlayers-1 {
		t.Errorf("la simulación debería tener %d jugadores, tiene %d", maxPlayers-1, got)
	}
}
