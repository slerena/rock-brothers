package audiokit

import (
	"fmt"
	"io"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
)

// Motor de audio propio sobre oto (purego: funciona incluso con CGO_ENABLED=0).
// Se mezclan las voces en Go y se empujan por un io.Pipe a ritmo real con un
// ticker. Se generan trozos PEQUEÑOS (mucha resolución temporal) y el buffer de
// oto se deja amplio para que no se entrecorte; así la latencia baja bastante
// respecto al buffer por defecto (0,5 s) sin sufrir cortes.

const Rate = 22050

type audioVoice struct {
	samples []int16
	pos     int
}

type Out struct {
	mu       sync.Mutex
	player   *oto.Player
	pipeW    *io.PipeWriter
	pipeR    *io.PipeReader
	voices   []*audioVoice
	lastPlay map[string]time.Time
	ok       bool
	closed   bool
}

var (
	sharedOnce sync.Once
	sharedInst *Out
)

// Shared devuelve el motor global; la inicialización (oto) va en segundo plano
// para no bloquear la interfaz en el primer sonido.
func Shared() *Out {
	sharedOnce.Do(func() {
		sharedInst = &Out{lastPlay: map[string]time.Time{}}
		go sharedInst.init()
	})
	return sharedInst
}

// New crea un motor independiente (no compartido).
func New() *Out {
	o := &Out{lastPlay: map[string]time.Time{}}
	go o.init()
	return o
}

func (a *Out) init() {
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   Rate,
		ChannelCount: 1,
		Format:       oto.FormatSignedInt16LE,
	})
	if err != nil {
		return
	}
	<-ready
	pr, pw := io.Pipe()
	p := ctx.NewPlayer(pr)
	// 8192 bytes (~186 ms a 22050 Hz): margen amplio para no entrecortar y aún
	// menos de la mitad del buffer por defecto de oto (0,5 s).
	p.SetBufferSize(8192)
	p.Play()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		_ = pr.Close()
		_ = pw.Close()
		p.Close()
		return
	}
	a.player, a.pipeR, a.pipeW, a.ok = p, pr, pw, true
	a.mu.Unlock()
	go a.pump()
}

// pump genera y escribe el PCM a ritmo real. chunk pequeño (~11,6 ms) para que
// el buffer amplio absorba el jitter del temporizador y no haya cortes.
func (a *Out) pump() {
	const chunk = 256 // ~11.6 ms a 22050 Hz
	buf := make([]int16, chunk)
	pcm := make([]byte, chunk*2)
	period := time.Duration(int64(chunk) * int64(time.Second) / int64(Rate))
	tk := time.NewTicker(period)
	defer tk.Stop()
	for range tk.C {
		a.mu.Lock()
		if a.closed {
			a.mu.Unlock()
			return
		}
		for i := range buf {
			buf[i] = a.mixLocked()
		}
		pw := a.pipeW
		a.mu.Unlock()
		for i, v := range buf {
			pcm[i*2] = byte(v)
			pcm[i*2+1] = byte(uint16(v) >> 8)
		}
		if pw == nil {
			return
		}
		if _, err := pw.Write(pcm); err != nil {
			return
		}
	}
}

func (a *Out) mixLocked() int16 {
	sum := 0
	live := a.voices[:0]
	for _, v := range a.voices {
		if v.pos < len(v.samples) {
			sum += int(v.samples[v.pos])
			v.pos++
			live = append(live, v)
		}
	}
	a.voices = live
	if sum > 30000 {
		sum = 30000
	}
	if sum < -30000 {
		sum = -30000
	}
	return int16(sum)
}

// Play reproduce un buffer. minGap limita la repetición del mismo sonido. Se
// puede llamar aunque el motor aún no esté listo: la voz queda encolada.
func (a *Out) Play(name string, samples []int16, minGap time.Duration) {
	if len(samples) == 0 {
		return
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	if t, ok := a.lastPlay[name]; ok && minGap > 0 && time.Since(t) < minGap {
		a.mu.Unlock()
		return
	}
	a.lastPlay[name] = time.Now()
	if len(a.voices) >= 12 {
		a.voices = a.voices[1:]
	}
	a.voices = append(a.voices, &audioVoice{samples: samples})
	a.mu.Unlock()
}

// Available indica si la salida de audio está lista.
func (a *Out) Available() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ok
}

// Close detiene el motor.
func (a *Out) Close() {
	a.mu.Lock()
	a.closed = true
	p, pr, pw := a.player, a.pipeR, a.pipeW
	a.mu.Unlock()
	if pw != nil {
		_ = pw.Close()
	}
	if pr != nil {
		_ = pr.Close()
	}
	if p != nil {
		p.Close()
	}
	time.Sleep(30 * time.Millisecond)
}

// ---------- síntesis de efectos ----------

type Bank struct {
	M map[string][]int16
}

var (
	soundsOnce sync.Once
	soundsInst *Bank
)

func Sounds() *Bank {
	soundsOnce.Do(func() {
		b := &Bank{M: map[string][]int16{}}
		b.M["shot"] = Sweep(900, 220, 90, 0.5, true)
		b.M["bangBig"] = Noise(420, 1.0, 90)
		b.M["bangMid"] = Noise(260, 0.9, 220)
		b.M["bangSmall"] = Noise(160, 0.8, 420)
		b.M["thrust"] = Noise(70, 0.45, 120)
		b.M["teleport"] = Sweep(200, 1800, 260, 0.45, false)
		b.M["shield"] = Sweep(500, 900, 140, 0.35, false)
		b.M["shieldOff"] = Sweep(900, 400, 160, 0.3, false)
		b.M["missile"] = Sweep(300, 1200, 220, 0.4, false)
		b.M["mineDrop"] = Sweep(700, 300, 90, 0.35, true)
		b.M["mineArm"] = Sweep(1200, 1200, 60, 0.3, true)
		b.M["mineBoom"] = Noise(520, 1.0, 110)
		b.M["enemy"] = Arp([]float64{180, 240, 180, 240}, 90, 0.4)
		b.M["enemyShot"] = Sweep(700, 200, 110, 0.4, true)
		b.M["railWarn"] = Sweep(300, 1600, 480, 0.4, false)
		b.M["railgun"] = Noise(600, 1.0, 60)
		b.M["lethal"] = Arp([]float64{1400, 1900, 2400, 3000}, 60, 0.35)
		// "pew" del arma láser: barrido agudo y corto, distinto del disparo normal
		b.M["laser"] = Sweep(2600, 180, 80, 0.45, true)
		b.M["boss"] = Arp([]float64{110, 82, 110, 146, 98}, 150, 0.55)
		b.M["hyper"] = Sweep(120, 2600, 900, 0.5, false)
		b.M["mystery"] = Arp([]float64{520, 392, 311, 233}, 220, 0.32)
		b.M["extra"] = Arp([]float64{660, 880, 1320}, 90, 0.45)
		b.M["gameover"] = Arp([]float64{440, 330, 220, 130}, 220, 0.55)
		// muerte de una nave: barrido grave + ruido (más contundente que bangBig)
		b.M["shipDie"] = append(Sweep(760, 60, 520, 0.9, true), Noise(360, 0.85, 90)...)
		// escudo reflectante: ping metálico al devolver una bala…
		b.M["reflect"] = Arp([]float64{1500, 2200, 1750, 2600}, 45, 0.32)
		// golpe de casco contra bloque: metálico y seco
		b.M["hullHit"] = append(Arp([]float64{240, 170, 120}, 80, 0.4), Noise(200, 0.5, 50)...)
		// materializarse (teletransporte de llegada): barrido ascendente con trino
		b.M["materialize"] = append(Sweep(160, 1500, 420, 0.5, false),
			Arp([]float64{1200, 1800, 2400, 3000}, 55, 0.35)...)
		// …y latigazo grave cuando devuelve el railgun
		b.M["reflectRail"] = append(Arp([]float64{1200, 300, 900, 200}, 70, 0.45), Noise(420, 0.9, 70)...)
		b.M["start"] = Arp([]float64{330, 440, 660}, 120, 0.45)
		b.M["alert"] = Arp([]float64{880, 1174, 880}, 130, 0.5)
		// muerte de cada jefe: un sonido característico por cada uno (rotan)
		bossDies := [][]int16{
			Arp([]float64{220, 165, 110, 82}, 200, 0.6),
			Sweep(1800, 120, 420, 0.55, true),
			Arp([]float64{160, 240, 360, 540}, 110, 0.5),
			Sweep(90, 1500, 380, 0.5, false),
			Arp([]float64{700, 560, 420, 300, 200}, 150, 0.55),
			Arp([]float64{520, 392, 587, 311, 466}, 160, 0.5),
		}
		for i, s := range bossDies {
			b.M[fmt.Sprintf("bossDie%d", i+1)] = s
		}
		for i := 0; i < 10; i++ { // pitido de carga del railgun (ascendente)
			f := 380 + float64(i)*95
			b.M[fmt.Sprintf("charge%d", i)] = Arp([]float64{f}, 70, 0.28)
		}
		soundsInst = b
	})
	return soundsInst
}

func (b *Bank) Play(name string, gap time.Duration) {
	Shared().Play(name, b.M[name], gap)
}

func Sweep(f0, f1, ms float64, amp float64, square bool) []int16 {
	n := int(Rate * ms / 1000)
	out := make([]int16, n)
	phase := 0.0
	for i := 0; i < n; i++ {
		t := float64(i) / float64(n)
		f := f0 + (f1-f0)*t
		phase += 2 * math.Pi * f / Rate
		v := math.Sin(phase)
		if square {
			if v > 0 {
				v = 1
			} else {
				v = -1
			}
		}
		env := 1 - t
		out[i] = int16(v * env * amp * 15000)
	}
	return out
}

func Noise(ms, amp, freq float64) []int16 {
	n := int(Rate * ms / 1000)
	out := make([]int16, n)
	last := 0.0
	alpha := math.Min(1, freq/Rate*6)
	for i := 0; i < n; i++ {
		t := float64(i) / float64(n)
		last += (rand.Float64()*2 - 1 - last) * alpha
		env := math.Pow(1-t, 1.6)
		out[i] = int16(last * env * amp * 15000)
	}
	return out
}

func Arp(freqs []float64, msEach, amp float64) []int16 {
	var out []int16
	for _, f := range freqs {
		out = append(out, Sweep(f, f, msEach, amp, true)...)
	}
	return out
}
