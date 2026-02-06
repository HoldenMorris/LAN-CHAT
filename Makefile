BINARY  = lan-chat
SRC     = main.go
ARGS   ?=

.PHONY: build run vet fmt clean tidy

build: ## Build the binary
	go build -o $(BINARY) $(SRC)

run: ## Run the app (e.g. make run ARGS="--pass=secret alice")
	go run $(SRC) $(ARGS)

vet: ## Run go vet
	go vet ./...

fmt: ## Format code with gofmt
	gofmt -w .

clean: ## Remove build artifacts and logs
	rm -f $(BINARY) debug.log

tidy: ## Tidy go.mod and go.sum
	go mod tidy

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'
