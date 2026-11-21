package sim

import (
	"math"
	"testing"
)

// TestClienteServidorDisparo: las balas las crea SOLO el servidor. Mientras el
// jugador mantiene el disparo, el cliente ve exactamente las balas del servidor
// (una por cadencia, ni una más) y no se inventa ninguna.
func TestClienteServidorDisparo(t *testing.T) {
	srv := New()
	srv.w, srv.h = 1280, 960
	srv.SetArena(ArenaRocks)
	srv.SetPvP(true)
	srv.restart()
	sp := srv.AddPlayer("yo")
	sp.ship.alive, sp.ship.invuln = true, 0

	cli := New()
	if err := cli.Load(srv.Snapshot()); err != nil {
		t.Fatal(err)
	}
	cp := cli.PlayerByID(sp.ID)
	if cp == nil {
		t.Fatal("el cliente no encuentra su jugador")
	}

	// el jugador mantiene el disparo (como si tuviera la tecla pulsada)
	srv.SetKey(sp, KeySpace, true)
	cli.SetKey(cp, KeySpace, true)

	maxCli, maxSrv := 0, 0
	prevSrv := 0
	worstSpawn := 0.0
	for tick := 0; tick < 180; tick++ { // ~2,9 s
		// el servidor recibe la pose del cliente (como por la red)
		srv.SetAimPose(sp, cp.ship.x, cp.ship.y, cp.ship.angle)
		srv.Step(0.016)
		if err := cli.Load(srv.Snapshot()); err != nil {
			t.Fatal(err)
		}
		cli.Extrapolate(0.016, cp)
		cli.StepLocalShip(cp, 0.016) // la nave sí se predice; las balas no

		mine, srvOwn := 0, 0
		for _, b := range cli.bullets {
			if b.owner == cp.ID {
				mine++
			}
		}
		for _, b := range srv.bullets {
			if b.owner == sp.ID {
				srvOwn++
			}
		}
		if mine > maxCli {
			maxCli = mine
		}
		if srvOwn > maxSrv {
			maxSrv = srvOwn
		}
		// al aparecer una bala nueva del servidor, ¿a qué distancia está de la
		// nave que ve el cliente? Tiene que salir pegada al morro (~14-20 px)
		if srvOwn > prevSrv {
			var newest astBullet
			for _, b := range srv.bullets {
				if b.owner == sp.ID {
					newest = b
				}
			}
			if d := math.Hypot(newest.x-cp.ship.x, newest.y-cp.ship.y); d > worstSpawn {
				worstSpawn = d
			}
		}
		prevSrv = srvOwn
	}

	shots, _ := srv.ShotsFired()
	if shots < 3 || shots > 7 {
		t.Errorf("en 2,9 s con cadencia ~0,57 s salen 4-6 disparos, salieron %d", shots)
	}
	if maxCli > maxSrv {
		t.Errorf("el cliente no debe tener más balas que el servidor: %d vs %d", maxCli, maxSrv)
	}
	if maxSrv == 0 {
		t.Error("el servidor debería haber disparado (si no, no habría daño)")
	}
	t.Logf("disparos: %d · máx. balas cliente: %d · servidor: %d · peor salida de bala: %.0f px de la nave", shots, maxCli, maxSrv, worstSpawn)
	if worstSpawn > 45 {
		t.Errorf("la bala sale demasiado lejos de la nave del cliente: %.0f px", worstSpawn)
	}
}

// TestDisparoDesdeLaPose: el disparo sale de la POSE que manda el cliente (el
// morro que el jugador ve), no de la nave del servidor. La del servidor va un
// viaje por detrás, y como el giro se predice en el cliente, disparar desde ahí
// dejaba la bala desalineada justo mientras se gira.
func TestDisparoDesdeLaPose(t *testing.T) {
	g := New()
	g.w, g.h = 1280, 960
	g.SetArena(ArenaRocks)
	g.restart()
	p := g.AddPlayer("ana")
	p.ship.alive, p.ship.invuln = true, 0
	p.ship.x, p.ship.y, p.ship.angle = 400, 400, math.Pi/2
	p.ship.vx, p.ship.vy = 0, 0

	// el cliente ve su nave un poco por delante y apuntando a 0 (recto a la
	// derecha): con el giro predicho, la diferencia de ángulo es lo normal
	const poseX, poseY, poseA = 430.0, 370.0, 0.0
	g.SetAimPose(p, poseX, poseY, poseA)
	g.SetAimLead(p, 0) // sin adelanto, para medir exacto

	g.fireLocked(p)
	if len(g.bullets) != 1 {
		t.Fatalf("debería haber 1 bala, hay %d", len(g.bullets))
	}
	b := g.bullets[0]
	if got := math.Atan2(b.vy, b.vx); math.Abs(got-poseA) > 1e-9 {
		t.Errorf("la bala apunta a %.3f rad, debería apuntar como la pose (%.3f)", got, poseA)
	}
	if d := math.Hypot(b.x-poseX, b.y-poseY); math.Abs(d-14) > 1e-9 {
		t.Errorf("la bala sale a %.1f px de la pose, debería salir a 14 (el morro)", d)
	}
}
