// Command server sirve el cliente web (WASM) y hace de servidor de juego.
//
// Es un único ejecutable: sirve index.html + game.wasm y atiende el WebSocket
// del juego (`/ws`), donde corre la simulación autoritativa (ver game.go).
package main

import (
	"flag"
	"log"
	"mime"
	"net"
	"net/http"
	"strings"
)

func main() {
	addr := flag.String("addr", ":8099", "dirección de escucha")
	dir := flag.String("dir", "web", "directorio con index.html, wasm_exec.js y game.wasm")
	best := flag.String("best", defaultBestPath(), "fichero donde se guarda el récord (MAX)")
	flag.Parse()

	loadBest(*best)

	// WebAssembly.instantiateStreaming exige el MIME correcto; si el servidor no
	// lo manda, el navegador lo rechaza (el index.html tiene un plan B, pero
	// mejor mandarlo bien).
	_ = mime.AddExtensionType(".wasm", "application/wasm")

	// El juego multijugador: servidor autoritativo (ver game.go).
	http.Handle("/ws", wsHandler(newLobby()))

	fs := http.FileServer(http.Dir(*dir))
	http.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// durante el spike no queremos cachés: cada build debe verse al recargar
		w.Header().Set("Cache-Control", "no-store")
		fs.ServeHTTP(w, r)
	}))

	log.Printf("cliente web en http://localhost%s  (sirviendo %s)", *addr, *dir)
	// La IP de LAN, para no tener que buscarla (y para no copiar una vieja): se
	// saca de la interfaz por la que sale la red. Si no se puede, no pasa nada.
	if ip := lanIP(); ip != "" {
		log.Printf("desde la LAN:  http://%s%s", ip, portOf(*addr))
	}
	log.Fatal(http.ListenAndServe(*addr, logRequests(http.DefaultServeMux)))
}

// portOf devuelve el ":puerto" de la dirección de escucha (":8099" → ":8099").
func portOf(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i:]
	}
	return ":8099"
}

// lanIP es la IPv4 de la interfaz por la que sale la red. Se pregunta al sistema
// (UDP "conectar" no envía nada, solo elige la ruta) en vez de listar interfaces:
// así se ignoran las VPN y los túneles, que no valen para jugar en casa.
func lanIP() string {
	c, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer c.Close()
	a, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok || a.IP.IsLoopback() {
		return ""
	}
	return a.IP.String()
}

// logRequests deja en el log solo lo que no son ficheros estáticos, para no
// llenar la consola con cada recarga.
func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			log.Printf("websocket: %s", r.RemoteAddr)
		}
		h.ServeHTTP(w, r)
	})
}
