# web/dist is gitignored (W13): build the frontend once before go build, or
# the binary ships an empty UI (the API still works).

.PHONY: all web build test check clean

all: build

web: ## build the SPA into web/dist (embedded via go:embed)
	cd web && vp install && vp run build

build: web ## build the keep binary with the embedded UI
	go build -o keep .

test:
	go test ./...

check:
	go vet ./...
	cd web && vp check

clean:
	rm -f keep
	find web/dist -mindepth 1 ! -name .gitkeep -delete
