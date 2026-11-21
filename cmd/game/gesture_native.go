//go:build !wasm

package main

// waitForUserGesture no hace nada en nativo: allí no hay políticas de autoplay.
func waitForUserGesture() {}
