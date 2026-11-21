package game

import (
	"time"

	"rock-brothers/internal/audiokit"
)

// Compartimos el motor de audio de la app (internal/audiokit) para no abrir un
// segundo contexto oto (en macOS fallaba y el juego quedaba mudo).

// audioOut es el motor de audio compartido.
type audioOut = audiokit.Out

// audioBank es un envoltorio del banco de sonidos compartido.
type audioBank struct{ b *audiokit.Bank }

// audioSounds devuelve el banco de sonidos compartido.
func audioSounds() *audioBank { return &audioBank{b: audiokit.Sounds()} }

// play reproduce un sonido con un intervalo mínimo opcional.
func (b *audioBank) play(name string, gap time.Duration) {
	audiokit.Shared().Play(name, b.b.M[name], gap)
}

// sharedAudio devuelve el motor de audio global.
func sharedAudio() *audioOut { return audiokit.Shared() }
