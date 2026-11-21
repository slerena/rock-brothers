package sim

import (
	"fmt"
	"sync"
)

// Pequeño i18n propio del juego (inglés por defecto, español y francés) para
// que el paquete sea autocontenido y se pueda forkear sin la app principal.
var (
	langMu  sync.RWMutex
	curLang = "en"
)

func setLang(l string) {
	langMu.Lock()
	curLang = l
	langMu.Unlock()
}

// Tr fija el idioma del juego ("en"|"es"|"fr"). Déjalo en "en" si no lo usas.
func Tr(lang string) { setLang(lang) }

// Translate traduce una clave para la plataforma (p. ej. el título de la
// ventana). La escena usa tr() directamente.
func Translate(key string, args ...any) string { return tr(key, args...) }

func tr(key string, args ...any) string {
	langMu.RLock()
	l := curLang
	langMu.RUnlock()
	s := key
	switch l {
	case "es":
		if v, ok := dictES[key]; ok {
			s = v
		}
	case "fr":
		if v, ok := dictFR[key]; ok {
			s = v
		}
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

var dictES = map[string]string{
	"Rock Brothers":             "Rock Brothers",
	"TIME %s":                   "TIEMPO %s",
	"WEAPON BULLETS":            "DISPARO BALAS",
	"WEAPON LASER":              "DISPARO LÁSER",
	"COMPANION":                 "COMPAÑERO",
	"SHIELD %d/%d (S)":          "ESCUDO %d/%d (S)",
	"SHIELD %d/%d · %.1fs":      "ESCUDO %d/%d · %.1fs",
	"SHIELD %d/%d reload %.1fs": "ESCUDO %d/%d recarga %.1fs",
	"RAILGUN %d%% (G)":          "RAILGUN %d%% (G)", "RAILGUN READY (G)": "RAILGUN LISTO (G)",
	"RAILGUN CHARGING %.0f%%": "RAILGUN CARGANDO %.0f%%", "RAPID FIRE x%d": "CADENCIA x%d",
	"MISSILES %d (M)  ·  MINES %d (K)": "MISILES %d (M)  ·  MINAS %d (K)",
	"LETHAL RAY (L)":                   "RAYO LETAL (L)", "LETHAL RAY %.1fs": "RAYO LETAL %.1fs",
	"MUTE (X)": "SILENCIO (X)",
	"PLAYER":   "JUGADOR", "KILLS": "BAJAS", "DEATHS": "MUERTES",
	"HULL %d/%d":       "CASCO %d/%d",
	"ASSISTANTS %d/%d": "ASISTENTES %d/%d", "TELEPORT  (ENTER)": "TELEPORT  (ENTER)", "TELEPORT %.1fs": "TELEPORT %.1fs",
	"WASD move · SPACE fire · S shield · ENTER teleport · M/K/G/L weapons": "WASD mover · ESPACIO dispara · S escudo · ENTER teleport · M/K/G/L armas",
	"Level %d": "Nivel %d", "press R to play again": "pulsa R para volver a jugar",
	"press R to go back to the lobby": "pulsa R para volver a la sala",
	"PAUSED":                          "PAUSA", "GAME OVER": "GAME OVER",
}

var dictFR = map[string]string{
	"Rock Brothers":             "Rock Brothers",
	"TIME %s":                   "TEMPS %s",
	"WEAPON BULLETS":            "TIR BALLES",
	"WEAPON LASER":              "TIR LASER",
	"COMPANION":                 "COMPAGNON",
	"SHIELD %d/%d (S)":          "BOUCLIER %d/%d (S)",
	"SHIELD %d/%d · %.1fs":      "BOUCLIER %d/%d · %.1fs",
	"SHIELD %d/%d reload %.1fs": "BOUCLIER %d/%d recharge %.1fs",
	"RAILGUN %d%% (G)":          "RAILGUN %d%% (G)", "RAILGUN READY (G)": "RAILGUN PRÊT (G)",
	"RAILGUN CHARGING %.0f%%": "RAILGUN EN CHARGE %.0f%%", "RAPID FIRE x%d": "CADENCE x%d",
	"MISSILES %d (M)  ·  MINES %d (K)": "MISSILES %d (M)  ·  MINES %d (K)",
	"LETHAL RAY (L)":                   "RAYON LÉTAL (L)", "LETHAL RAY %.1fs": "RAYON LÉTAL %.1fs",
	"MUTE (X)": "MUET (X)",
	"PLAYER":   "JOUEUR", "KILLS": "TUÉS", "DEATHS": "MORTS",
	"HULL %d/%d":       "COQUE %d/%d",
	"ASSISTANTS %d/%d": "ASSISTANTS %d/%d", "TELEPORT  (ENTER)": "TÉLÉPORT  (ENTRÉE)", "TELEPORT %.1fs": "TÉLÉPORT %.1fs",
	"WASD move · SPACE fire · S shield · ENTER teleport · M/K/G/L weapons": "WASD déplacer · ESPACE tirer · S bouclier · ENTRÉE téléport · M/K/G/L armes",
	"Level %d": "Niveau %d", "press R to play again": "appuyez sur R pour rejouer",
	"press R to go back to the lobby": "appuyez sur R pour revenir au salon",
	"PAUSED":                          "PAUSE", "GAME OVER": "GAME OVER",
}
