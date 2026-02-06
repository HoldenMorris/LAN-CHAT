# Plan: Makefile

## Context

The project has no Makefile. Common Go project tasks (build, run, clean, vet, fmt) are run manually. A Makefile standardizes these.

## Targets

| Target | Command | Purpose |
|---|---|---|
| `build` (default) | `go build -o lan-chat main.go` | Compile binary |
| `run` | `go run main.go $(ARGS)` | Run with args (e.g. `make run ARGS="--pass=secret alice"`) |
| `vet` | `go vet ./...` | Static analysis |
| `fmt` | `gofmt -w .` | Format code |
| `clean` | `rm -f lan-chat debug.log` | Remove build artifacts and logs |
| `tidy` | `go mod tidy` | Clean up go.mod/go.sum |
