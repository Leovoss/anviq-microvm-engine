BIN := bin/fcctl

.PHONY: build tidy run smoke clean

build: ## Build the control-plane binary
	go build -o $(BIN) ./cmd/fcctl

tidy: ## Resolve deps (needs network; populates go.sum)
	go mod tidy

run: build ## Run locally (needs root, KVM, and host-setup.sh already applied)
	sudo ANVIQ_CONTROL_TOKEN=$${ANVIQ_CONTROL_TOKEN:?set a token} $(BIN)

smoke: ## Phase 1 exit criterion: boot a microVM, run a command, tear it down
	./scripts/smoke.sh

clean:
	rm -rf bin
