BIN      := bin/asteroids
WEB      := web
WASM     := $(WEB)/game.wasm
GOROOT   := $(shell go env GOROOT)
# Go reciente lo tiene en lib/wasm; los antiguos en misc/wasm.
WASM_EXEC := $(shell test -f "$(GOROOT)/lib/wasm/wasm_exec.js" \
	&& echo "$(GOROOT)/lib/wasm/wasm_exec.js" \
	|| echo "$(GOROOT)/misc/wasm/wasm_exec.js")

.PHONY: run wasm serve vet test clean

# Juego nativo (para comparar con la versión web).
run:
	mkdir -p bin
	go build -o $(BIN) ./cmd/game
	./$(BIN)

# Cliente web: el WASM más el wasm_exec.js DE TU versión de Go (tienen que
# coincidir exactamente, si no el runtime no arranca).
wasm:
	mkdir -p $(WEB)
	cp "$(WASM_EXEC)" $(WEB)/wasm_exec.js
	GOOS=js GOARCH=wasm go build -o $(WASM) ./cmd/game
	@ls -lh $(WASM) | awk '{print "wasm:", $$5}'

# Sirve el cliente en http://localhost:8099
serve:
	go run ./cmd/server

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -rf bin $(WEB)/game.wasm
