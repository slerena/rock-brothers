# Rock Brothers

> Proyecto: `rock-brothers` (antes `asteroids-mp`). El nombre del **juego** que
> se ve en pantalla es **ROCK BROTHERS**.

**Rock Brothers**: porte del juego **Asteroids** de Pandora MINI a proyecto aparte, para hacerlo
multijugador (coop + PvP, arena compartida) con el cliente **en el navegador**.

El juego es una copia de `pandora-mini-4/internal/asteroids`, así que
`pandora-mini-4` no se toca.

## Cómo se ejecuta

```bash
# Nativo (ventana de Fyne)
make run

# Web + servidor de juego (esto es el multijugador)
make wasm                             # compila web/game.wasm (el cliente)
go build -o bin/server ./cmd/server   # compila el servidor
./bin/server -addr :8099 -dir web     # sirve el cliente Y simula las partidas
# (o `make serve`, que hace `go run ./cmd/server` en el 8099)
```

Con el servidor levantado, en el navegador:

```
http://localhost:8099/?name=ana                     # tu máquina
http://192.168.1.43:8099/?name=bob                  # LAN (la IP del que sirve)
```

La IP del que sirve se saca con `ipconfig getifaddr en0` (macOS, interfaz por la
que sale la red) o, mejor, **la dice el propio servidor al arrancar**:

```
cliente web en http://localhost:8099  (sirviendo web)
desde la LAN:  http://192.168.1.43:8099
```

Así no hay que copiar una IP vieja a mano (cambia con el DHCP). El servidor
escucha en `*:8099`, así que vale cualquier interfaz; si desde la LAN no responde,
mira el cortafuegos del Mac antes que el código.

En la sala se elige coop / todos-contra-todos, arena (asteroides / campo fijo), y
se añaden bots con su nivel. **Tras cada compilación hay que recargar con
Cmd+Shift+R**: el `game.wasm` se cachea y si no se recarga se juega con el viejo.

Al cargar sale la **pantalla de bienvenida** (el título *ROCK BROTHERS* con
asteroides a la deriva de fondo y el resumen de controles). Hay que hacer clic en
ella porque el navegador **no da teclado ni deja sonar audio sin un gesto del
usuario**: ese clic es el gesto. El fondo es un canvas aparte (`#titleBg`),
decorativo, y su animación **se para** al entrar al juego.

**Tests** (sin servidor, no hace falta navegador):

```bash
go test -count=1 ./...        # toda la suite
```

Hay tests que **pintan a PNG** para revisar cosas visuales a mano (muerte +
teletransporte, tabla final, grietas): se guardan en el directorio temporal del
sistema (`$TMPDIR/ast_*.png`). El navegador headless con `--virtual-time-budget`
**no** es fiable para WebSocket/rAF: para verificar de verdad se usan los tests de
Go, los del servidor, y estos renders.

Parámetros de la URL (útiles para medir y probar):

| Parámetro | Para qué |
|---|---|
| `?name=ana` | nombre visible (se guarda en una cookie) |
| `?mode=coop\|pvp` | entra directamente a ese tipo de partida |
| `?scale=3` | resolución del lienzo (640×480 × scale). Solo nitidez: no agranda nada |
| `?glow=1` | pases del halo de fósforo (1 = sin halo, más barato) |
| `?autostart=1` | se salta la pantalla de arranque (pruebas automáticas) |

En nativo, lo mismo por entorno: `AST_RES=1 AST_GLOW=3 make run`.

**Operativo**: `rock-brothers` **no es un repo git** (nada commiteado; no hay
`git diff` que consulte). El servidor de pruebas se suele dejar corriendo en
segundo plano en el puerto 8099.

## Arquitectura

```
cmd/game/            # mismo binario para nativo (Fyne) y web (canvas del navegador)
  main.go            # espera al primer gesto y elige la plataforma
  run_native.go      # ventana de Fyne
  run_wasm.go        # game.RunWeb() (sin app de Fyne)
  gesture_*.go       # el gesto del usuario (solo hace falta en web)
cmd/server/          # sirve el cliente web; será también el servidor de juego
internal/sim/        # EL JUEGO: simulación pura + dibujo, sin plataforma
  sim.go             # estado y reglas (Step), y la escena (DrawFrame)
  draw.go            # interfaz `Drawer`: SetColor/MoveTo/Stroke/DrawStringAnchored…
  renderopts.go      # resolución/halo, medibles
  i18n.go            # en/es/fr
internal/game/       # LA PLATAFORMA: pegamento con Fyne / canvas / audio
  widget.go          # widget de Fyne (raster, teclado, foco) + Open (nativo)
  loop.go            # bucle a 60 Hz (Step + present) y audio
  draw.go            # ggDrawer (gg, CPU) — nativo
  draw_canvas_wasm.go# canvasDrawer (canvas 2D, GPU) — web
  present.go         # repinta el widget de Fyne
  present_wasm.go    # dibuja en el canvas #game
  input_wasm.go      # teclado del DOM (Fyne no traduce las letras en web)
  web_wasm.go        # RunWeb: arranca sin app de Fyne
internal/audiokit/   # motor de audio (oto), copiado tal cual
web/                 # index.html + wasm_exec.js + game.wasm (generados)
```

La clave: **la simulación no conoce la plataforma**. `internal/sim` no importa
Fyne, ni gg, ni `syscall/js`, ni el motor de audio: recibe los nombres de tecla
como texto y dibuja contra la interfaz `Drawer`. Eso es lo que permite que el
mismo juego corra en el cliente nativo, en el navegador y **en el servidor**
(llamando solo a `Step`, sin dibujar ni sonar).

En el cliente, `Drawer` tiene dos implementaciones: `ggDrawer` (fogleman/gg,
**rasterizado por CPU**) en nativo, y `canvasDrawer` (el **canvas 2D del
navegador**, GPU) en web. La escena es la misma en las dos.

## Por qué así (conclusiones del spike, medidas)

1. **WebAssembly no penaliza** el rasterizado: nativo y wasm costaban casi lo
   mismo por píxel (~5 ms/MPix). El problema nunca fue wasm.
2. **`gg` es CPU**: a 1280×960 en wasm son ~13 ms por frame, que saturan el hilo
   del navegador y **cuelgan la pestaña**. Por eso en web no se usa.
3. **El lienzo de Fyne no sirve como superficie de juego en web**: su driver
   crea el canvas del tamaño de la ventana × `devicePixelRatio` (4,3 MPix en un
   portátil Retina) y no se puede elegir: o nítido y lento, o rápido y borroso.
   De ahí el canvas propio, de resolución fija y elegida por nosotros.
4. Con el canvas 2D el coste baja ~5× (100 % → 20 % de CPU a 1280×960) y es
   **nítido a cualquier resolución**, porque son vectores.
5. **Trampas encontradas y ya resueltas** (no reintroducir):
   - En web, el driver de Fyne no traduce **las letras**: `keyToName()` devuelve
     `KeyUnknown` para A-Z. Por eso el teclado se lee del DOM
     (`internal/game/input_wasm.go`).
   - `oto` no arranca hasta que hay un **gesto del usuario**: sin él el juego va
     mudo (y el motor a medias). De ahí la pantalla de arranque.
   - La **transformación del canvas persiste entre frames**: si `drawFrame`
     aplica translate+scale sin resetear, se multiplican y el dibujo acaba fuera
     del lienzo. `presentCanvas` la resetea en cada frame.
   - El juego usaba el tamaño de **dispositivo** como unidades, así que en Retina
     dibujaba todo a la mitad. Ahora **el mundo son unidades lógicas** (el tamaño
     que ocupa el lienzo en CSS), así que las naves y el HUD tienen siempre su
     tamaño de siempre —como en la ventana nativa— y lo que crece con la ventana
     es el campo de juego. El lienzo tiene más píxeles (device) que se dibujan
     escalados: `?scale` es solo nitidez, no tamaño. (En multijugador el tamaño
     del mundo lo fijará el servidor y el cliente letterboxeará si hace falta.)
6. **El `dt` del juego es fijo** (`update(0.016)`): a menos de 60 fps el juego va
   a cámara lenta. En multijugador lo resolverá el tick del servidor.

## Multijugador (servidor autoritativo)

**Funcionando**: varios navegadores en la **misma arena** contra un servidor que
simula. El servidor es la única autoridad (tick fijo a 60 Hz, RNG suyo), así que
no hay desincronización ni trampas: el cliente solo manda teclas y su pose, y
pinta lo que recibe (snapshots a 30 Hz).

Lo que hay montado, de un vistazo:

- **Sala de espera** en la web: tipo de partida (coop / todos contra todos),
  arena (asteroides / campo fijo), bots con nivel (torpe / medio / bueno) y
  nombre. Hasta **8 jugadores** por sala (los bots hasta llenarla).
- **Fase de partida + cuenta atrás + tabla final** (12 s, o se salta con R).
- **Dos arenas**: asteroides (con agujero negro y oleadas) y campo fijo (duelo
  entre jugadores con estructura destructible que esconde power-ups).
- **Armas y power-ups completos** (ver "Jugabilidad"): escudo reflectante, láser,
  minas, misiles, railgun, rayo letal, asistentes, y munición con power-ups.
- **Bots** que juegan con las mismas reglas (son `sim.Player` normales pilotados
  por el servidor con `SetKey`), en tres niveles.
- **Latencia**: el servidor mide el RTT de cada jugador (ping/pong), lo enseña en
  la sala y lo usa para compensar el disparo.

### Probarlo

```bash
make wasm && go build -o bin/server ./cmd/server && ./bin/server -addr :8099 -dir web
```

Y abre **dos pestañas** (o dos navegadores):

```
http://localhost:8099/?name=ana
http://localhost:8099/?name=bob&mode=pvp
```

Abajo a la izquierda sale la sala (`coop · ana (tú), bob`). Si el servidor no
está, el juego arranca en **solitario** sin decir nada.

Parámetros: `?name=` y `?mode=coop|pvp` (además de los de render).

### Cómo funciona

```
  navegador                        cmd/server
  ─────────                        ──────────
  tecla  ── MsgInput (JSON) ──▶   encola
                                  tick 60 Hz: sim.Step(0.016)
                                  Snapshot() ── MsgSnapshot ──▶  sim.Load() + pintar
```

- `internal/proto`: el protocolo (tramas binarias con 1 byte de tipo; control en
  JSON, estado en bytes de `sim`).
- `cmd/server/game.go`: salas + WebSocket (`/ws`) + el bucle de juego.
- `internal/sim/snapshot.go`: `Snapshot()`/`Load()`, serialización gob con
  structs espejo (los campos de las entidades son privados). ~5 KB por frame.
- `internal/game/net_wasm.go`: cliente (WebSocket, input, aplicar snapshots).

El mundo lo fija el servidor (**el mismo campo para todos**); el cliente escala
su lienzo a ese campo, no al tamaño de su ventana.

### Suavizado (el cliente no va a saltos)

El estado llega a 60 Hz, así que entre dos snapshots el cliente:

- **Extrapola** el mundo (`sim.Extrapolate`): avanza las posiciones con las
  velocidades que ya trae, solo lo visual (ni colisiones ni IA).
- **Predice tu nave** (`sim.StepLocalShip`): movimiento lineal **y giro** al
  instante (empuje, deriva, rozamiento, gravedad). El giro se predice porque no
  hacerlo se notaba perezoso; para que el disparo siga saliendo del morro que ves,
  el cliente manda su **pose** (posición y ángulo, 60 Hz) y **el servidor dispara
  desde ahí**, no desde su propia nave (`fireLocked` → `aimFor`). Como la pose
  tarda medio viaje de red, el servidor la **adelanta** lo que mide el ping de ese
  jugador (`SetAimLead`, RTT/2 + un tick). Al llegar el snapshot la posición se
  acerca un 8 % a la del servidor (corrección suave); el ángulo es **siempre el
  del cliente**. La invulnerabilidad la lleva el servidor, para que no se gaste al
  doble.
- **Las balas NO se predicen**: las crea solo el servidor (predecirlas daba
  ráfagas y disparos que no hacían daño). Una pulsación = un disparo del servidor.

### Jugabilidad (reglas actuales)

- **Dos modos**: cooperativo (todos contra las rocas y las naves enemigas) y
  **todos contra todos** (`pvp`). En coop **no hay fuego amigo**: disparar a un
  compañero no le hace nada (esto no es un bug).
- **Arenas**: `rocks` (asteroides, con agujero negro) y `fixed` (campo fijo: una
  estructura de bloques en espejo, sin agujero ni oleadas; es un duelo). La arena
  fija lleva **power-ups escondidos** dentro de los bloques: al romperlos salen.
- **Vidas**: 5 por partida. En coop la partida acaba cuando no le quedan a nadie;
  en PvP cuando solo queda uno.
- **Muerte**: suena, suelta trozos, y la nave tarda **1,9 s** en recomponerse con
  un **teletransporte** en el sitio elegido (con destello y sonido propios, del
  color del jugador). Al reaparecer: **4,5 s de invulnerabilidad** y el sitio se
  elige lejos de bloques, jugadores y **minas** (y se limpia un radio de 150 px).
- **Casco**: chocar con un bloque **no mata**: quita un punto de casco (3 y
  revientas), te saca del bloque y te da un rebote, con chispas y sonido. El casco
  se repara solo (1 punto cada 6 s) y se pone a tope al reaparecer.
- **Armas**: bala+escudo (base), láser, rayos, minas, misiles, railgun y rayo
  letal. El **rayo letal (L) solo hace daño a asteroides**, nunca a jugadores.
- **Misiles**: se empieza con **uno** y **no se recargan**; el power-up da 2. En
  **'vs' persiguen siempre naves** (nunca rocas ni bloques) y matan; en
  **cooperativo van más lentos** y sí persiguen asteroides. Se pueden **reventar a
  tiros** (basta que el disparo pase cerca).
- **Power-ups**: flotan en la arena (de todos, disputables), duran 36 s y
  **parpadean los últimos 10**. Solo aparecen si le sirven a alguien. El de escudo
  da 100 puntos (tope 500) y con escudo permanente el anillo es **dorado
  reflectante** (rebota balas y railgun).
- **Power-ups recogidos**: son tuyos hasta que mueres (al morir se pierden escudo
  y power-ups de pantalla, pero se conservan armas y munición).

### Pendiente (siguiente)

- **Interpolar por entidad** en el cliente (por id) y poder bajar los snapshots a
  20 Hz: ahora van a 30 Hz y con extrapolación, suficiente.
- **Predicción local de las balas propias** si vuelve a notarse desalineación al
  disparar (habría que listar aparte las balas propias y no pintar las del
  servidor del mismo dueño).
- **Reconexión** y aviso al expulsar naves abandonadas (se quitan, pero sin
  aviso).
- **Espectador en coop** al quedarse sin vidas (ahora se sigue viendo el campo).

Notas de diseño ya cerradas: agujero negro que mata solo a quien lo toca, nivel
que se reinicia solo si mueren todos, rocas que avanzan para todos y power-ups
flotantes disputables (una vez cogidos son de ese jugador).

Cada jugador tiene su **color de nave** para distinguirse. Son 8 colores (verde,
morado, cian, ámbar, rosa, blanco, naranja, azul) y el color se asigna por índice
(`tintOf`), así que no hay que mandarlo por la red; el cliente del hall
(`web/index.html`) usa la misma lista.

Cuando la sala se queda **sin jugadores se para y se tira**: la siguiente partida
empieza limpia (si no, arrastraba rocas partidas, más niveles y naves muertas de
la sesión anterior).

### Valores de ajuste (los números que se han ido tocando)

Todos juntos, para no tenerlos que buscar por el código. Constantes con nombre: si
se toca una de movimiento hay que tocar **las dos** (servidor y predicción del
cliente) o basta usar la constante.

| Qué | Constante | Valor |
|---|---|---|
| Giro de la nave | `ShipTurnRate` | 4,40 rad/s |
| Tope de velocidad | `shipSpeedMax` | 340 px/s |
| Empuje / rozamiento (por paso de 16 ms) | `shipThrust` / `shipFriction` | 489 / 0,983 |
| Cadencia de disparo (normal/rápida) | `fireIntervalBase` | 0,66 / 1,15 / 1,1 s |
| Bala normal / láser | `bulletSpeed` / `laserSpeed` | 430 / 1800 |
| Misil en cooperativo / en "vs" | `missileSpeed` / `missileSpeedPvP` | 150 / 206 |
| Misiles al empezar (sin recarga) | `missileStock` | 1 |
| Misiles que da el power-up | `missilePowerupAmmo` | 2 |
| Minas al empezar / por power-up | `mineStock` / `ammoPowerup` | 5 / 10 |
| Vidas por partida | `livesPerMatch` | 5 |
| Casco (golpes de bloque) / reparación | `shipHullMax` / `hullRegenEvery` | 3 / 6 s |
| Radio libre de minas al reaparecer | `mineClearRadius` | 150 px |
| Radio bala→nave (PvP) / bala→misil | — / `bulletMissileHitR` | 20 / 20 px |
| Power-up: duración / parpadeo | `powerupTTL` / `powerupBlink` | 36 / 10 s |
| Power-up: cada cuánto sale | `powerupSpawnEvery` | 14 s |
| Escudo: tope / por power-up | `permShieldCap` / `permShieldPer` | 500 / 100 |
| Reaparición: retardo / invulnerabilidad | `respawnDelay` / `shipInvulnSpawn` | 1,9 / 4,5 s |
| Bloques del campo fijo: vida | `fixedBlockHP` | 3 |
| Jugadores por sala (bots hasta llenarla) | `maxPlayers` | 8 |

Relaciones útiles para ajustar:

- **Deriva** (lo que resbala al soltar el motor) ≈ `shipSpeedMax * 0,016 / (1 - shipFriction)`.
- Para bajar la deriva sin bajar el tope: subir `shipFriction` y subir `shipThrust`
  a la vez, manteniendo `shipThrust / (1 - shipFriction)` constante.
- El mundo del servidor es `ArenaW*2 × ArenaH*2` = **1280×960**; `ArenaW/ArenaH`
  (640×480) son solo la referencia de la rejilla.

### Lecciones transversales (no solo de este juego)

- **Una función que nadie llama no arregla nada.** El bug más caro de la sesión
  fue `aimFor`: existía, estaba probada y no la usaba nadie. Antes de dar algo por
  arreglado, comprobar que el camino está **conectado** (un test que falle sin el
  arreglo).
- **Un ajuste silencioso es peor que un error.** `str.replace` sobre constantes
  que `gofmt` ha alineado falla **sin avisar** (cambian los espacios). Usar regex
  con `\s*` y **verificar con `grep` después**.
- **Los tests con geometría, en números, no en PNG.** El diagnóstico visual con
  `DrawFrame` engaña (`ApplyRenderOpts` aplica escala) y costó un rato. Para
  medir posiciones/ángulos, imprimir valores y comparar.
- **Ojo con las medidas que cruzan los bordes**: el campo da la vuelta (wrap), así
  que la "distancia recta" miente; sumar el recorrido paso a paso.
- **Servidor autoritativo = el cliente solo pinta**, y lo que se predice en el
  cliente (giro, movimiento propio) hay que compensarlo en el servidor (pose,
  adelanto de lag), o se ve desalineado "a veces" según la latencia.
- **En los tests de simulación, `sim.New()` crea un jugador local (id 0)** que en
  PvP cuenta como **enemigo**: para probar targeting (misiles, radio de impacto)
  hay que hacer `g.players = nil` antes de añadir los jugadores de prueba.
- **Nunca dejar `g.rocks` vacío en un test**: un campo sin rocas se interpreta
  como "nivel superado" y dispara el hiperespacio (congela lo que estés midiendo).
  Usar **una roca lejana** (`fixed: true` si no quieres que estorbe).
- **`sim.New()` no mide el mundo hasta `SetSize`/`SetArena`**: los jugadores
  creados antes se colocan "a ojo" y luego se reparten. En tests, `SetSize` antes
  y después de `restart()` para que el mundo quede como el del servidor.

### Trampas que costaron (y que hay que recordar)

- **La tabla del final se quedaba sin salir (a veces)**. `netTick` solo aplicaba
  los snapshots si la fase era `playing` (la idea era no extrapolar en la sala).
  Pero el snapshot que trae el **fin de partida** (`over=true`) llegaba a veces
  **después** del mensaje de fase `over`, así que se quedaba en la cola sin
  aplicar y el cliente nunca veía el `over`: la tabla no salía. Es una carrera
  (~1 de cada 6 partidas). Ahora el estado del servidor se aplica **siempre**;
  lo que se salta en la sala/cuenta atrás es solo la extrapolación.
- **"En PvP siguen saliendo 5 misiles"**. El tope por modo se aplicaba al crear
  el jugador y al arrancar la partida, pero la **recarga** seguía comparando
  contra la constante `missileStock` (5): reponía hasta 5 aunque empezaras con 1.
  Regla: cualquier sitio que mire la munición de misiles debe usar
  `missileStockLocked()`.
- **"Jugar otra partida" (R) rompía la sala**. La tecla R viajaba al servidor y
  la simulación del servidor la interpretaba como "reiniciar partida": arrancaba
  una partida nueva que nadie veía (el servidor solo avanza el mundo cuando la
  fase es `playing`) y sonaba el arranque. Ahora el servidor **no pasa R a la
  simulación**: en la tabla del final la usa para saltársela y volver a la sala
  de espera, y el cliente en multijugador dice "pulsa R para volver a la sala".
- **El adelanto de la pose era fijo (0,05 s) y la red no lo es**. En localhost
  sobraba (el disparo salía por delante) y en LAN faltaba: por eso se veía
  desalineado "a veces". Ahora el servidor mide el RTT con el ping/pong y le
  pasa a la simulación `RTT/2 + un tick` por jugador (`SetAimLead`).
- **Balas que se veían tocar y no explotaban**. El cliente extrapola las naves
  (las dibuja algo por delante) y el radio de impacto era 13 px: subido primero a
  17 y luego a **20** (la silueta de la nave llega a ~18 px del centro), y el del
  misil contra nave a 18. Además, desde entonces los impactos sueltan chispas.
- **El disparo salía de la nave del SERVIDOR, no de la pose del cliente**. Toda
  la maquinaria estaba puesta (el cliente manda su pose a 60 Hz, el servidor la
  guarda con `SetAimPose`) pero **`fireLocked` nunca la consultaba**: creaba la
  bala con `s.x, s.y, s.angle` del servidor. Como el **giro se predice en el
  cliente**, en cuanto girabas la nave del servidor iba por detrás y la bala
  salía desplazada y girada. Ahora `fireLocked` dispara desde `aimFor` (la pose
  + el adelanto de lag). Los bots no mandan pose: `aimFor` devuelve su propia
  nave y no cambia nada. Lección: **una función que nadie llama no arregla
  nada**; el test `TestDisparoDesdeLaPose` lo fija ahora.

- **Los power-ups se esfumaban solos**. `killShip` hacía `g.powerups = nil`
  ("los que quedan flotando no son nuestros", herencia del modo de un jugador).
  En multijugador los power-ups de la arena son de todos: con bots muriendo todo
  el rato no duraban ni cinco segundos de los ~36 que se supone. Ya no se borran.
- **Reaparecer encima de una mina**. Las minas matan aunque estés invulnerable,
  así que si te tocaba reaparecer donde había una, morías sin poder hacer nada.
  Ahora el sitio de reaparición también **evita** las minas al elegirse y se
  **limpia** (radio 150 px) al materializarse.
- **El teletransporte parecía no tener dueño**. Los haces ya usaban el color del
  jugador, pero el destello grande y el núcleo eran **blancos** y se comían el
  color. Ahora el destello y el núcleo van teñidos con el color del jugador
  (aclarado hacia el blanco para que brille), así se ve de quién es la nave que
  se está recomponiendo.
- **Reventar un misil a tiros era muy exigente**. Se miraba solo el punto final
  de la bala con un radio de 12 px. Ahora se mira **todo el recorrido del frame**
  (`distToSegment`) con radio 20 (`bulletMissileHitR`): vale con pasarles cerca.
- **La bala se iba de lado aunque saliera del morro**. La bala **heredaba la
  velocidad de la nave** (`+ s.vx, + s.vy`): yendo a la deriva, su trayectoria se
  torcía respecto al morro hasta **30°** a velocidad alta. De ahí el "sale
  desalineado" que se veía unas veces sí y otras no (dependía de cuánto
  derivaras). Ahora la bala sale **siempre** en la dirección en la que apunta la
  nave (`TestDisparoAlineadoConLaNave`, que prueba 4 ángulos derivando a tope).
- **Demasiada deriva**. El rozamiento estaba a mano (0.992) en el servidor y en la
  predicción del cliente, y `shipThrust` igual (230). Ahora son constantes
  compartidas (`shipThrust`, `shipFriction`) para que no se puedan separar.
  Ajuste final: tope **340 px/s**, rozamiento **0.983** y empuje **489**. La
  deriva al soltar queda en **~296 px** (venía de ~600). Al subir el rozamiento
  hay que subir el empuje a la vez (`shipThrust ≈ S·(1-f)/dt`), o el tope baja;
  el precio es que la nave llega al tope algo antes (más reactiva), que es justo
  lo que se busca al quitar deriva. `TestDerivaMasCorta` mide la punta, el tiempo hasta el tope y
  la deriva, y calcula lo esperado desde las constantes.
  OJO al medir deriva en un test: `ArenaW/ArenaH` (640×480) son **la mitad** del
  mundo del servidor (1280×960), y la nave da la vuelta por los bordes; hay que
  sumar velocidad×paso, no la distancia recta.
- **No se veían los impactos ni las grietas**. Las grietas de los bloques del
  campo fijo se pintaban en `{0x18,0x30,0x44}` (azul casi negro) sobre fondo
  negro: invisibles. Ahora van del **mismo color que el borde del bloque**
  (`0x7a,0xd0,0xff`). Y los disparos **sueltan chispas** al impactar
  (`g.sparks`) contra rocas, naves o misiles, para que el golpe se vea.
- **Chocar con un bloque mataba de una vez**. Ahora la nave tiene **casco: 3
  golpes** (`shipHullMax`). El golpe quita un punto, **saca la nave del bloque**
  (`pushOutOf`) y le da un rebote suave (`bounceOff`), con chispas y sonido
  (`hullHit`); al tercero revienta. El casco se **repara solo** cada 6 s
  (`hullRegenEvery`) y se pone a tope al reaparecer. Se enseña en el HUD
  (`HULL 3/3`, en rojo cuando queda 1).
- **El HUD era siempre el del jugador 0**. En multijugador, un cliente que no
  fuera el 0 veía las armas y el escudo de otro. Ahora el cliente dice quién es
  (`SetLocalID`, con el `You` del hello) y el HUD enseña lo suyo.
- Además, el radio de impacto bala→nave en PvP sube de 17 a **20 px** (la
  silueta de la nave llega a ~18 del centro).
- **Misiles, reglas nuevas** (a petición):
  - Se empieza con **UN misil en los dos modos** y **no se recargan** (fuera la
    recarga automática de los 5). Los demás salen de los power-ups.
  - El **power-up de misiles da 2** (`missilePowerupAmmo`); el de minas sigue
    dando 10 (`ammoPowerup`).
  - **En 'vs' el misil va SIEMPRE a por las naves**: nunca a asteroides ni a las
    estructuras del campo fijo (`nearestEnemyFor` corta y devuelve solo
    jugadores). Y mata de un impacto.
  - **En cooperativo va MÁS LENTO** (`missileSpeed` 150) que en 'vs'
    (`missileSpeedPvP` 206) y allí sí persigue asteroides (y al jefe).
  - La extrapolación del cliente usa `missileSpeedLocked()`, no una constante:
    con velocidades distintas por modo, una constante hacía que el misil dibujado
    se adelantara/atrasara unos píxeles.

### Añadir o cambiar algo (receta)

El servidor es la única autoridad, así que casi todo se toca en `internal/sim`:

1. **Reglas/valores**: constante con nombre en `internal/sim/sim.go` (o `world.go`
   si es de arena/mundo) y se usa desde el `Step`. Nada de números sueltos.
2. Si el valor afecta al **movimiento de la nave**, hay que usarlo en `Step`
   (servidor) **y** en `StepLocalShip` (predicción del cliente): si se separan, el
   cliente predice distinto y la nave "tira" en cada snapshot.
3. Si el estado nuevo tiene que **llegar al cliente** (para el HUD o para pintarlo),
   hay que añadirlo al códec: `gobPlayer`/`gobState` en `internal/sim/snapshot.go`
   (structs espejo), y copiarlo a `playerRender` en `DrawFrame`. Si no, el cliente
   lo verá siempre a cero (le pasó al casco).
4. Si hace falta un **sonido**, se añade al banco (`internal/audiokit/audio.go`) y
   se dispara con `g.playSound(...)`: el servidor no tiene audio, los sonidos se
   **retransmiten** al cliente (`MsgSounds`) y el cliente los reproduce.
5. **Test que falle sin el cambio** (los tests son la red de seguridad: ver el
   índice de abajo). Y si es algo visual, un test que pinte a PNG.
6. `gofmt -w internal cmd && go test -count=1 ./... && make wasm && go build -o
   bin/server ./cmd/server`, reiniciar el servidor y **Cmd+Shift+R** en el navegador.

### Índice de tests (qué cubre cada zona)

- **Netcode/cliente**: `TestClienteServidorDisparo` (una pulsación = una bala),
  `TestDisparoDesdeLaPose`, `TestDisparoAlineadoConLaNave`, `TestStepLocalShipSoloEseJugador`,
  `TestExtrapolateMueveSoloPosiciones`, `TestSnapshotRoundTrip`, `TestSnapshotLoadGarbage`.
- **Disparo/armas**: `TestCadenceAndLevelClock`, `TestLaserLongerAndFaster`,
  `TestLaserRangeMatchesBullet`, `TestDisparoDoble`, `TestWeaponPowerupsAreExclusive`,
  `TestMaxRapidFireIsX8`, `TestProyectilNoAtraviesa`.
- **Misiles/minas**: `TestMisiles`, `TestMisilVelocidades`, `TestMisilesPvP`.
- **PvP/coop**: `TestPvPFuegoAmigo`, `TestPvPUltimoEnPie`, `TestMarcador`.
- **Daño/escudos**: `TestEscudoReflectante`, `TestReboteDelRailgun`, `TestShieldCostHardness`,
  `TestShieldHardMult`, `TestRamDamageScalesWithSpeed`, `TestCascoContraBloques`,
  `TestCascoLlegaAlCliente`.
- **Muerte/reaparición**: `TestMuerteDejaAnimacion`, `TestMuerteViajaEnElSnapshot`,
  `TestAparicionLejos`, `TestReaparecerLimpiaMinas`, `TestPreviewMuerteYTeleport`.
- **Power-ups**: `TestPowerupUseful`, `TestPowerupUtil`, `TestNoSueltaPowerupsInutiles`,
  `TestNearestPowerupSkipsUseless`, `TestPowerupsSobrevivenALaMuerte`, `TestRocasSinPowerup`,
  `TestCompanionDropCadence`.
- **Arenas/niveles**: `TestCampoFijo`, `TestCampoFijoArena`, `TestDibujoCampoFijo`,
  `TestNavesConColorDistinto`, `TestSiluetasDistintas`, `TestEnemyHP`, `TestHoleAccel`.
- **Movimiento**: `TestDerivaMasCorta`, `TestAddPlayerAndInput`.
- **Servidor/salas**: `TestTwoPlayersShareArena`, `TestFaseFinal`, `TestRSaltaLaTabla`,
  `TestBots`, `TestLimiteDeJugadores`, `TestSoundsAreRelayed`.
- **Bots**: `TestNivelesDeBot`, `TestBotsUsanSusTrastos`, `TestBotPersigue`, `TestBotEsquiva`.
- **Render**: `TestRenderConContenido`, `TestRenderEscalas`, `TestRenderSinMedidas`,
  `TestPreviewTablaFinal`.

### El renombrado (asteroids-mp → rock-brothers)

El **directorio** del proyecto y el **módulo Go** pasaron de `asteroids-mp` a
`rock-brothers` (el nombre visible del juego es *ROCK BROTHERS*). Al hacerlo:

- Las importaciones pasan a `rock-brothers/internal/...` y `go.mod` dice
  `module rock-brothers`. En Go el módulo no tiene por qué llamarse como la
  carpeta, pero así no hay dos nombres distintos.
- El récord (`best.json`) vivía en la carpeta de configuración del usuario con el
  nombre viejo (`.../asteroids-mp/`), así que `loadBest` **migra** el fichero a
  `.../rock-brothers/` la primera vez que arranca (si no, se perdía el MAX).
- El **nombre visible** no depende de esto: sale de `web/index.html` y de la
  entrada i18n `"Rock Brothers"`.
