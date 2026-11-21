package sim

import (
	"bytes"
	"testing"
)

// TestSnapshotRoundTrip comprueba que un snapshot se puede cargar en otro Sim y
// que volver a serializarlo da exactamente los mismos bytes (ida y vuelta
// estable). Además llena la escena para que la serialización cubra rocas,
// enemigos, jefe, power-ups, agujero negro, compañeros y armas.
func TestSnapshotRoundTrip(t *testing.T) {
	g := New()
	g.w, g.h = ArenaW, ArenaH
	g.AddPlayer("dos")

	p0, p1 := g.Player(0), g.Player(1)
	g.KeyDownFor(p0, KeySpace) // dispara (forma balas y partículas)
	g.KeyDownFor(p1, KeyUp)    // empuja (mueve la nave)

	// Unos cuantos segundos de simulación "normal".
	for i := 0; i < 300; i++ {
		g.Step(0.016)
	}

	// Poblamos lo que el azar no garantiza tan pronto: oleada, jefe, power-ups,
	// agujero y un estado de armas variado.
	g.mu.Lock()
	g.spawnWave()
	if g.boss == nil {
		g.spawnBoss()
	}
	g.spawnPowerup(120, 90, powerupOrb)
	g.spawnPowerup(200, 140, powerupLife)
	g.spawnPowerup(260, 200, powerupCompanion)
	g.spawnPowerup(300, 260, powerupMissiles)
	g.hole = &astHole{x: ArenaW / 2, y: ArenaH / 2, r: 30, age: 1.5}
	g.mu.Unlock()

	// Un par de pasos para que todo se mueva y se generen estelas/partículas.
	for i := 0; i < 5; i++ {
		g.Step(0.016)
	}

	b1 := g.Snapshot()
	if len(b1) == 0 {
		t.Fatal("Snapshot devolvió 0 bytes")
	}
	t.Logf("snapshot de una escena cargada: %d bytes (%d rocas, %d enemigos, %d partículas)",
		len(b1), len(g.rocks), len(g.enemies), len(g.parts))

	g2 := New()
	if err := g2.Load(b1); err != nil {
		t.Fatalf("Load: %v", err)
	}
	b2 := g2.Snapshot()
	if !bytes.Equal(b1, b2) {
		t.Fatalf("el snapshot no es ida y vuelta estable: %d bytes vs %d bytes", len(b1), len(b2))
	}

	// Campos clave.
	if len(g2.Players()) != 2 {
		t.Fatalf("Load debe dejar 2 jugadores, hay %d", len(g2.Players()))
	}
	if got, want := len(g2.rocks), len(g.rocks); got != want {
		t.Errorf("nº de rocas: %d, se esperaba %d", got, want)
	}
	if got, want := len(g2.enemies), len(g.enemies); got != want {
		t.Errorf("nº de enemigos: %d, se esperaba %d", got, want)
	}
	if got, want := len(g2.powerups), len(g.powerups); got != want {
		t.Errorf("nº de power-ups: %d, se esperaba %d", got, want)
	}
	for i := 0; i < 2; i++ {
		a, b := g.Player(i), g2.Player(i)
		if a == nil || b == nil {
			t.Fatalf("jugador %d ausente tras Load", i)
		}
		if a.Name != b.Name || a.lives != b.lives || a.score != b.score {
			t.Errorf("jugador %d difiere: (%q,%d,%d) vs (%q,%d,%d)",
				i, a.Name, a.lives, a.score, b.Name, b.lives, b.score)
		}
		if a.ship.alive != b.ship.alive {
			t.Errorf("jugador %d: vivo %v vs %v", i, a.ship.alive, b.ship.alive)
		}
	}
	if g2.level != g.level {
		t.Errorf("nivel: %d, se esperaba %d", g2.level, g.level)
	}
	if (g2.boss != nil) != (g.boss != nil) {
		t.Errorf("jefe: hay=%v, se esperaba hay=%v", g2.boss != nil, g.boss != nil)
	}
	if (g2.hole != nil) != (g.hole != nil) {
		t.Errorf("agujero: hay=%v, se esperaba hay=%v", g2.hole != nil, g.hole != nil)
	}
}

// TestSnapshotLoadGarbage comprueba que Load rechaza bytes que no son un
// snapshot (sin entrar en pánico).
func TestSnapshotLoadGarbage(t *testing.T) {
	g := New()
	g.w, g.h = ArenaW, ArenaH

	cases := [][]byte{
		nil,
		{},
		[]byte("esto no es un snapshot"),
		{snapshotMagic},                   // cabecera correcta pero sin payload
		{snapshotMagic, 0xff, 0x00, 0x01}, // cabecera correcta y basura detrás
		{0x00, 0x01, 0x02, 0x03},          // cabecera incorrecta
	}
	for i, b := range cases {
		if err := g.Load(b); err == nil {
			t.Errorf("caso %d: Load debería devolver error", i)
		}
	}
}
