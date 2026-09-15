BIN := bin/fcctl
GUEST := bin/anviq-guest

.PHONY: build guest tidy test run smoke clean

build: ## Build the control-plane binary
	go build -o $(BIN) ./cmd/fcctl

guest: ## Build the static guest agent to install into the base rootfs
	CGO_ENABLED=0 GOOS=linux go build -o $(GUEST) ./cmd/anviq-guest

rootfs: ## Build kernel + base rootfs (with the guest agent) into /opt/anviq (needs root)
	sudo ./scripts/build-rootfs.sh

tidy: ## Resolve deps (needs network; populates go.sum)
	go mod tidy

test: ## Run offline unit tests (no KVM required)
	go test ./...

run: build ## Run locally (needs root, KVM, and host-setup.sh already applied)
	sudo ANVIQ_CONTROL_TOKEN=$${ANVIQ_CONTROL_TOKEN:?set a token} $(BIN)

smoke: ## Phase 1 exit criterion: boot a microVM, run a command, tear it down
	./scripts/smoke.sh

clean:
	rm -rf bin
