package game

import (
	"time"

	"rock-brothers/internal/sim"
)

// shotSoundAtVar envuelve la marca del último disparo propio (la pone el cliente
// web; en nativo no se usa).
var shotSoundAtVar = func() time.Time { return time.Time{} }

// remoteSim indica que el estado lo manda el servidor (multijugador): entonces
// el bucle NO simula, solo presenta lo que llega. En solitario es false y el
// bucle avanza la simulación local. Lo redefine el cliente web al conectarse.
var remoteSim = func() bool { return false }

// runLoop avanza la simulación a 60 Hz y presenta cada frame. Es el bucle del
// cliente NATIVO (la vía web usa requestAnimationFrame, en present_wasm.go).
//
// El dt es FIJO (0.016): a menos de 60 fps el juego va a cámara lenta, a
// propósito. En multijugador lo fija el tick del servidor.
//
// stop puede ser nil (entonces solo para el ticker).
func runLoop(g *sim.Sim, present func(*sim.Sim), stop <-chan struct{}) {
	t := time.NewTicker(16 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if !remoteSim() {
				g.Step(0.016)
			}
			present(g)
		}
	}
}

// wireAudio conecta el banco de sonidos compartido a la simulación y despierta
// el motor. En el navegador, oto crea el AudioContext y espera a la siguiente
// tecla o clic para reanudarlo (el navegador no deja sonar sin un gesto): cuanto
// antes se registre ese listener, antes empieza a sonar.
func wireAudio(g *sim.Sim) {
	bank := audioSounds()
	g.OnSound = func(name string, gap time.Duration) { bank.play(name, gap) }
	_ = sharedAudio() // arranca la inicialización del motor (oto)
}

// playSoundLocal reproduce un sonido en ESTE cliente. En multijugador la
// simulación corre en el servidor (allí no hay audio), así que los sonidos
// llegan por la red y se reproducen aquí. El silencio es local (tecla X).
func playSoundLocal(name string, gap time.Duration) {
	if g := currentGame; g != nil && g.Muted() {
		return
	}
	// El disparo ya ha sonado aquí al predecirlo: se ignora la copia que manda el
	// servidor (llega con el retardo de la red, y en ráfaga si disparaste varias
	// veces). Ventana corta para no perder los disparos de los demás.
	if name == "shot" || name == "laser" {
		if v := shotSoundAtVar(); !v.IsZero() && time.Since(v) < 120*time.Millisecond {
			return
		}
	}
	audioSounds().play(name, gap)
}

// Tr fija el idioma del juego ("en"|"es"|"fr").
func Tr(lang string) { sim.Tr(lang) }
