// Package game implementa el juego "Asteroids" de Pandora MINI (huevo de
// pascua). Es autocontenido a propósito: solo depende de Fyne, gg y oto, con su
// propio audio, caché de fuentes e i18n (en/es/fr), para poder forkearlo aparte.
//
// Uso:
//
//	asteroids.Tr("es")                 // idioma del HUD (opcional)
//	asteroids.Open(app, iconResource)  // abre la ventana del juego
package game
