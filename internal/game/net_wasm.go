//go:build wasm

package game

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"syscall/js"
	"time"

	"rock-brothers/internal/proto"
	"rock-brothers/internal/sim"
)

// Cliente de red del multijugador.
//
// Al arrancar intenta conectarse al WebSocket del servidor. Si lo consigue, la
// partida pasa a ser autoritativa del servidor: este cliente deja de simular,
// manda su input y pinta los snapshots que recibe. Si no hay servidor, el juego
// sigue funcionando en solitario (el bucle local simula, como siempre).

var (
	// Cola de snapshots: el servidor los manda a 30 Hz y por la red llegan a
	// rachas (sobre todo por Wi-Fi). En vez de aplicarlos según caen, se guardan
	// y se consumen a ritmo fijo; así el mundo avanza regular y no a tirones.
	snapQueue  [][]byte
	snapAt     time.Time
	snapPeriod = time.Second / 30

	netConn  js.Value
	netUp    bool    // conectado y recibiendo del servidor
	netYou   int     // índice de mi jugador dentro de la simulación
	netMode  string  // "coop" | "pvp"
	netPhase string  // "lobby" | "countdown" | "playing"
	fitDone  bool    // ya se ha avisado a la página del tamaño del campo
	poseAcc  float64 // acumulador del envío de pose (60 Hz)
	// disparo propio: contador visto y cuándo sonó (para que suene al instante al
	// predecirlo y no repetir la copia que llega del servidor)
	lastShots   int
	shotSoundAt time.Time
)

func init() {
	remoteSim = func() bool { return netUp }
	// el sonido del disparo propio lo controla el cliente (predicción)
	shotSoundAtVar = func() time.Time { return shotSoundAt }
}

// netConnect abre el WebSocket y se presenta. Silencioso si no hay servidor.
func netConnect() {
	wsCtor := js.Global().Get("WebSocket")
	if wsCtor.Type() != js.TypeFunction {
		return // sin WebSocket: modo solitario
	}
	loc := js.Global().Get("location")
	scheme := "ws:"
	if loc.Get("protocol").String() == "https:" {
		scheme = "wss:"
	}
	url := scheme + "//" + loc.Get("host").String() + "/ws"
	conn := wsCtor.New(url)
	conn.Set("binaryType", "arraybuffer")
	netConn = conn

	conn.Call("addEventListener", "open", js.FuncOf(func(this js.Value, args []js.Value) any {
		sendFrame(proto.JSON(proto.MsgJoin, proto.Join{
			Name: playerName(),
			Mode: gameMode(),
		}))
		return nil
	}))
	conn.Call("addEventListener", "message", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			netMessage(args[0].Get("data"))
		}
		return nil
	}))
	// El botón "listo" de la página llama aquí.
	js.Global().Set("__astReady", js.FuncOf(func(this js.Value, args []js.Value) any {
		ready := true
		if len(args) > 0 {
			ready = args[0].Truthy()
		}
		sendFrame(proto.JSON(proto.MsgReady, proto.Ready{Ready: ready}))
		return nil
	}))
	// Cambiar el nombre desde la sala de espera.
	js.Global().Set("__astSetName", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		name := strings.TrimSpace(args[0].String())
		if name == "" {
			return nil
		}
		if len(name) > 16 {
			name = name[:16]
		}
		sendFrame(proto.JSON(proto.MsgName, proto.Name{Name: name}))
		return nil
	}))
	// Añadir o quitar un bot de pruebas.
	js.Global().Set("__astBot", js.FuncOf(func(this js.Value, args []js.Value) any {
		add := true
		if len(args) > 0 {
			add = args[0].Truthy()
		}
		level := 1
		if len(args) > 1 {
			level = args[1].Int()
		}
		sendFrame(proto.JSON(proto.MsgBot, proto.Bot{Add: add, Level: level}))
		return nil
	}))
	// Cambiar la arena (asteroides / campo fijo).
	js.Global().Set("__astSetArena", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		a := args[0].String()
		sendFrame(proto.JSON(proto.MsgArena, proto.Arena{Arena: a}))
		return nil
	}))
	// Cambiar el tipo de partida (cooperativo / todos contra todos).
	js.Global().Set("__astSetMode", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		m := args[0].String()
		if m != proto.ModePvP {
			m = proto.ModeCoop
		}
		sendFrame(proto.JSON(proto.MsgMode, proto.Mode{Mode: m}))
		return nil
	}))
	conn.Call("addEventListener", "close", js.FuncOf(func(this js.Value, args []js.Value) any {
		netUp = false
		netStatus("desconectado · modo solitario")
		return nil
	}))
	conn.Call("addEventListener", "error", js.FuncOf(func(this js.Value, args []js.Value) any {
		// sin servidor: se juega en solitario, sin ruido
		if !netUp {
			netStatus("sin servidor · modo solitario")
		}
		return nil
	}))
}

func playerName() string {
	if v := js.Global().Get("__astName"); v.Type() == js.TypeString && v.String() != "" {
		return v.String()
	}
	return "jugador"
}

func gameMode() string {
	if v := js.Global().Get("__astMode"); v.Type() == js.TypeString && v.String() == proto.ModePvP {
		return proto.ModePvP
	}
	return proto.ModeCoop
}

// netMessage procesa una trama del servidor.
func netMessage(data js.Value) {
	var buf []byte
	switch data.Type() {
	case js.TypeString:
		buf = []byte(data.String())
	default:
		u8 := js.Global().Get("Uint8Array").New(data)
		n := u8.Get("length").Int()
		buf = make([]byte, n)
		js.CopyBytesToGo(buf, u8)
	}
	if len(buf) == 0 {
		return
	}
	switch buf[0] {
	case proto.MsgHello:
		var h proto.Hello
		if json.Unmarshal(buf[1:], &h) == nil {
			netYou = h.You
			netUp = true
			sendToServer = netSendInput
			netMode = h.Mode
			// Multijugador: la partida es del servidor (la tabla del final y la
			// vuelta a la sala las manda él). Y el HUD debe ser el de MI jugador.
			sim.NetPlay = true
			if currentGame != nil {
				currentGame.SetLocalID(h.You)
			}
			// En multijugador los sonidos los manda el servidor: la simulación
			// local (la predicción) no debe sonar, o sonaría todo dos veces.
			if currentGame != nil {
				currentGame.OnSound = nil
			}
			netShowPlayers(h.Players)
		}
	case proto.MsgSnapshot:
		// se guarda en la cola y se aplica en netTick, a ritmo fijo
		body := make([]byte, len(buf)-1)
		copy(body, buf[1:])
		snapQueue = append(snapQueue, body)
		// si vamos muy por detrás, se tira el más viejo (mejor perder un frame
		// que acumular retardo)
		if len(snapQueue) > 4 {
			snapQueue = snapQueue[1:]
		}
	case proto.MsgSounds:
		var s proto.Sounds
		if json.Unmarshal(buf[1:], &s) == nil {
			for _, snd := range s.List {
				playSoundLocal(snd.N, time.Duration(snd.G)*time.Millisecond)
			}
			// contador para poder comprobar desde fuera que suenan
			if len(s.List) > 0 {
				v := js.Global().Get("__sounds")
				n := 0
				if v.Type() == js.TypeNumber {
					n = v.Int()
				}
				js.Global().Set("__sounds", n+len(s.List))
			}
		}
	case proto.MsgPing:
		// el servidor mide la latencia: se lo devolvemos tal cual
		var pg proto.Ping
		if json.Unmarshal(buf[1:], &pg) == nil {
			sendFrame(proto.JSON(proto.MsgPong, pg))
		}
	case proto.MsgState:
		var st proto.State
		if json.Unmarshal(buf[1:], &st) == nil {
			netPhase = st.Phase
			netShowLobby(st)
		}
	case proto.MsgRoster:
		var r proto.Roster
		if json.Unmarshal(buf[1:], &r) == nil {
			netShowPlayers(r.Players)
		}
	case proto.MsgBye:
		var b proto.Bye
		if json.Unmarshal(buf[1:], &b) == nil {
			netStatus("servidor: " + b.Reason)
		}
	}
}

func netSendInput(name string, down bool) {
	if !netUp {
		return
	}
	sendFrame(proto.JSON(proto.MsgInput, proto.Input{Key: name, Down: down}))
	// Además se aplica en local: es lo que hace que TU nave responda al instante
	// (predicción). Se usa SetKey (solo el estado de la tecla, sin acciones
	// inmediatas): el disparo y el resto los hace StepLocalShip a su ritmo, así no
	// se duplican balas (antes el KeyDown disparaba además del paso). El servidor
	// corrige en el siguiente snapshot.
	if g := currentGame; g != nil {
		if p := g.PlayerByID(netYou); p != nil {
			g.SetKey(p, name, down)
		}
	}
}

// applySnapshot mete el estado del servidor en la simulación local, conservando
// la nave predicha si no se ha ido muy lejos (muerte, teletransporte, empujón).
func applySnapshot(g *sim.Sim, body []byte) {
	var px, py, pvx, pvy, pAngle float64
	havePred := false
	if netUp {
		if p := g.PlayerByID(netYou); p != nil {
			px, py, pvx, pvy, pAngle, _, havePred = p.ShipState()
		}
	}
	if err := g.Load(body); err != nil {
		return
	}
	if havePred {
		if p := g.PlayerByID(netYou); p != nil {
			sx, sy, _, _, _, sInvuln, sAlive := p.ShipState()
			if sAlive && math.Hypot(sx-px, sy-py) < 70 {
				// el ángulo es el nuestro (lo predecimos y el servidor dispara
				// desde la pose que le mandamos); la posición se acerca un poco
				const k = 0.08
				p.SetShip(px+(sx-px)*k, py+(sy-py)*k, pvx, pvy, pAngle, sInvuln, true)
			}
		}
	}
	if !fitDone {
		if w, h := g.WorldSize(); w > 0 && h > 0 {
			js.Global().Call("__astFitWorld", w, h)
			fitDone = true
		}
	}
}

// netTick avanza el estado que se pinta entre dos snapshots: extrapola las
// posiciones (para que no dé tirones) y predice el movimiento de tu nave (para
// que responda al instante).
func netTick(g *sim.Sim, dt float64) {
	if g == nil || !netUp || dt <= 0 {
		return
	}
	// El estado del servidor se aplica SIEMPRE, también en la sala de espera y en
	// la tabla del final. Antes se saltaba si la fase no era "playing", y el
	// snapshot que trae el fin de partida (over=true) llegaba ya con la fase
	// cambiada a "over": se quedaba en la cola sin aplicar y la tabla de
	// puntuaciones no llegaba a salir (a veces sí, a veces no, según la carrera
	// con el mensaje de fase).
	// consumir un snapshot cuando toca (a ritmo fijo, como mucho uno por frame,
	// pero sin acumular retardo)
	if len(snapQueue) > 0 && time.Since(snapAt) >= snapPeriod/2 {
		applySnapshot(g, snapQueue[0])
		snapQueue = snapQueue[1:]
		snapAt = time.Now()
	}
	// Desde aquí solo se extrapola/predice durante la partida. En la sala de
	// espera y en la cuenta atrás el mundo está congelado en el servidor: si no,
	// las rocas se moverían solas aquí.
	if netPhase != proto.PhasePlaying {
		return
	}
	p := g.PlayerByID(netYou)
	g.Extrapolate(dt, p)
	if p == nil {
		return
	}
	// La nave se avanza a paso FIJO (0.016), como el servidor: su física lleva
	// rozamiento y topes POR PASO, así que si se avanzara con el dt del frame,
	// en una pantalla de 120 Hz iría distinta que en el servidor y se separaría
	// de la que ve el otro jugador.
	// un solo paso con el dt real del frame: el rozamiento va compensado dentro,
	// así el movimiento es continuo (antes se avanzaba en tramos de 16 ms y a 60
	// fps se notaba a saltitos)
	if dt > 0.05 {
		dt = 0.05 // tras un parón largo (pestaña oculta) no dar un salto
	}
	g.StepLocalShip(p, dt)
	if n, laser := g.ShotsFired(); n != lastShots {
		lastShots = n
		// el servidor también manda este sonido, pero con el retardo de la red:
		// suena aquí al instante y se ignora el suyo unos milisegundos
		shotSoundAt = time.Now()
		if laser {
			playSoundLocal("laser", 0)
		} else {
			playSoundLocal("shot", 0)
		}
	}
	// mandar al servidor dónde y hacia dónde está nuestra nave (60 Hz) para que
	// dispare desde ahí y las balas salgan de la nave que vemos, sin desfase
	poseAcc += dt
	if poseAcc >= 1.0/60 {
		poseAcc = 0
		if x, y, _, _, a, _, alive := p.ShipState(); alive {
			netSendPose(x, y, a)
		}
	}
}

// netSendPose manda la pose de disparo al servidor.
func netSendPose(x, y, a float64) {
	sendFrame(proto.JSON(proto.MsgPose, proto.Pose{X: x, Y: y, A: a}))
}

func sendFrame(b []byte) {
	if netConn.Type() != js.TypeObject {
		return
	}
	u8 := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(u8, b)
	netConn.Call("send", u8)
}

// netStatus escribe el estado de la conexión en la página (si está el hueco).
func netStatus(s string) {
	doc := js.Global().Get("document")
	if doc.Type() != js.TypeObject {
		return
	}
	if el := doc.Call("getElementById", "net"); el.Type() == js.TypeObject {
		el.Set("textContent", s)
	}
}

// netShowLobby pasa el estado de la sala a la página, que dibuja la sala de
// espera (jugadores con su "listo" y su lag, y la cuenta atrás).
func netShowLobby(st proto.State) {
	fn := js.Global().Get("__astLobby")
	if fn.Type() != js.TypeFunction {
		return
	}
	list := make([]any, 0, len(st.Players))
	for _, p := range st.Players {
		lag := "—"
		if p.Lag > 0 {
			lag = fmt.Sprintf("%d ms", p.Lag)
		}
		ready := "…"
		if p.Ready {
			ready = "✓"
		}
		name := p.Name
		if p.ID == netYou {
			name += " (tú)"
		}
		list = append(list, map[string]any{
			"id":    p.ID,
			"name":  name,
			"ready": ready,
			"lag":   lag,
			"me":    p.ID == netYou,
		})
	}
	fn.Invoke(st.Phase, st.Countdown, list, st.Mode, st.Arena)
}

// netShowPlayers pinta la sala: modo y quiénes están (tú marcado).
func netShowPlayers(ps []proto.PlayerInfo) {
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		n := p.Name
		if n == "" {
			n = "jugador"
		}
		if p.ID == netYou {
			n += " (tú)"
		}
		names = append(names, n)
	}
	mode := netMode
	if mode == "" {
		mode = gameMode()
	}
	netStatus(fmt.Sprintf("%s · %s", mode, strings.Join(names, ", ")))
}
