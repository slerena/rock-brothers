package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// Récord persistido.
//
// En multijugador la partida la lleva el servidor, así que el récord también: se
// guarda en un fichero (con el nombre de quien lo consiguió) y se manda a los
// clientes dentro del snapshot (para que el HUD muestre MAX).

type bestEntry struct {
	Best  int
	Level int
	Name  string
}

var (
	bestMu  sync.Mutex
	bestCur bestEntry
	bestAt  string // ruta del fichero
)

// loadBest lee el récord del disco (si no hay fichero, empieza en cero).
func loadBest(path string) {
	migrateBest(path) // si venimos del nombre viejo, traer el récord
	bestMu.Lock()
	bestAt = path
	bestMu.Unlock()
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var v bestEntry
	if json.Unmarshal(b, &v) != nil {
		return
	}
	bestMu.Lock()
	bestCur = v
	bestMu.Unlock()
}

// currentBest devuelve el récord actual (puntos, nivel y quién).
func currentBest() (int, int, string) {
	bestMu.Lock()
	defer bestMu.Unlock()
	return bestCur.Best, bestCur.Level, bestCur.Name
}

// saveBest apunta un récord nuevo (lo llama la simulación) y lo guarda.
func saveBest(best, level int, who string) {
	bestMu.Lock()
	if best <= bestCur.Best {
		bestMu.Unlock()
		return
	}
	bestCur = bestEntry{Best: best, Level: level, Name: who}
	path := bestAt
	bestMu.Unlock()
	if path == "" {
		return
	}
	b, err := json.Marshal(bestCur)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, b, 0o644)
}

// defaultBestPath es dónde se guarda el récord si no se dice otra cosa.
func defaultBestPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "rock-brothers-best.json"
	}
	return filepath.Join(dir, appDirName, "best.json")
}

// appDirName es la carpeta del juego dentro de la configuración del usuario.
// Antes el juego se llamaba "asteroids-mp": en loadBest se migra el récord viejo
// para no perderlo al cambiar de nombre.
const appDirName = "rock-brothers"
const legacyAppDirName = "asteroids-mp"

// migrateBest mueve el récord de la carpeta vieja a la nueva (una sola vez). Si
// no hay nada que migrar, no hace nada.
func migrateBest(newPath string) {
	if _, err := os.Stat(newPath); err == nil {
		return // ya hay récord nuevo: no se toca
	}
	old := filepath.Join(filepath.Dir(filepath.Dir(newPath)), legacyAppDirName, "best.json")
	b, err := os.ReadFile(old)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(newPath, b, 0o644)
	log.Printf("récord migrado de %s a %s", old, newPath)
}
