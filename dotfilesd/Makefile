# dotfilesd - dotfiles runtime daemon & CLI
#
# Targets:
#   setup          Full install: build + install + service + start
#   build          Build daemon and CLI binaries (with version hash)
#   install        Fast install: skip if hash unchanged
#   proto          Regenerate protocol buffer code
#   deps           Install Go dependencies
#   service-install  Install systemd user service
#   service-start    Enable and start the daemon
#   service-stop     Stop and disable the daemon
#   service-logs     View daemon logs
#   clean          Remove build artifacts
#

BINARY_DIR  := $(HOME)/.local/bin
DAEMON_BIN  := $(BINARY_DIR)/dotfilesd
CLI_BIN     := $(BINARY_DIR)/dotfilesctl
SERVICE_DIR := $(HOME)/.config/systemd/user
SERVICE_FILE := $(SERVICE_DIR)/dotfilesd.service
CMD_DIR     := ./cmd
GO          := go
HASH_FILE   := $(BINARY_DIR)/.dotfilesd.hash

BUILD_HASH  := $(shell git rev-parse HEAD 2>/dev/null || echo "dev")
GOFLAGS     := -ldflags="-s -w -X main.buildHash=$(BUILD_HASH)"

export PATH := $(HOME)/go/bin:$(PATH)

.PHONY: all build install proto deps clean setup service-install service-start service-stop service-logs help

all: build

# -- Setup ------------------------------------------------------------------

setup: install service-install service-start
	@echo ""
	@echo "dotfilesd setup complete!"
	@echo "  daemon:   $(DAEMON_BIN)"
	@echo "  cli:      $(CLI_BIN)"
	@echo ""
	@echo "quick test: dotfilesctl ping"

# -- Build -------------------------------------------------------------------

build:
	@mkdir -p $(BINARY_DIR)
	$(GO) build $(GOFLAGS) -o $(DAEMON_BIN) $(CMD_DIR)/dotfilesd
	$(GO) build $(GOFLAGS) -o $(CLI_BIN) $(CMD_DIR)/dotfilesctl
	@mkdir -p $(dir $(HASH_FILE))
	@echo "$(BUILD_HASH)" > $(HASH_FILE)
	@echo "  built (hash: $(BUILD_HASH))"

# -- Install (fast: skip if hash unchanged) -----------------------------------

install:
	@h=$$(git rev-parse HEAD 2>/dev/null || echo "dev"); \
	if [ -f $(HASH_FILE) ] && [ "$$(cat $(HASH_FILE))" = "$$h" ] && [ -x $(DAEMON_BIN) ] && [ -x $(CLI_BIN) ]; then \
		echo "  binaries up to date (hash: $$h)"; \
	else \
		echo "  building (hash: $$h)..."; \
		$(GO) build $(GOFLAGS) -o $(DAEMON_BIN) $(CMD_DIR)/dotfilesd; \
		$(GO) build $(GOFLAGS) -o $(CLI_BIN) $(CMD_DIR)/dotfilesctl; \
		echo "$$h" > $(HASH_FILE); \
	fi
	@mkdir -p $(HOME)/.config/dotfilesd $(HOME)/.config/dotfilesctl
	@echo "  installed"

# -- Proto ------------------------------------------------------------------

PROTO_DIR := proto/dotfilesd/v1/dotfilesdv1
PROTO_SRCS := $(wildcard $(PROTO_DIR)/*.proto)
PROTO_PB_GO := $(patsubst $(PROTO_DIR)/%.proto, $(PROTO_DIR)/%.pb.go, $(PROTO_SRCS))
PROTO_CONNECT_DIR := $(PROTO_DIR)/dotfilesdv1connect
PROTO_CONNECT_GO := $(patsubst $(PROTO_DIR)/%.proto, $(PROTO_CONNECT_DIR)/%.connect.go, $(PROTO_SRCS))

proto: $(PROTO_PB_GO) $(PROTO_CONNECT_GO)

$(PROTO_PB_GO) $(PROTO_CONNECT_GO): $(PROTO_SRCS)
	PATH="$(HOME)/go/bin:$(PATH)" protoc \
		--go_out=. --go_opt=paths=source_relative \
		--connect-go_out=. --connect-go_opt=paths=source_relative \
		$(PROTO_SRCS)

proto-tools:
	@echo "installing protoc-gen-go and protoc-gen-connect-go..."
	$(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	$(GO) install connectrpc.com/connect/cmd/protoc-gen-connect-go@latest

# -- Dependencies -----------------------------------------------------------

deps:
	$(GO) mod tidy
	$(GO) mod download

# -- Systemd Service --------------------------------------------------------

$(SERVICE_FILE):
	@mkdir -p $(SERVICE_DIR)
	@sed 's|{{BINARY_DIR}}|$(BINARY_DIR)|g' service/dotfilesd.service.template > $@

service-install: $(SERVICE_FILE)
	systemctl --user daemon-reload
	@echo "service installed. run 'make service-start' to enable."

service-start: service-install
	systemctl --user enable --now dotfilesd.service
	@echo "service started."

service-stop:
	-systemctl --user stop dotfilesd.service
	-systemctl --user disable dotfilesd.service

service-logs:
	journalctl --user -u dotfilesd.service -f

# -- Clean ------------------------------------------------------------------

clean:
	rm -f $(DAEMON_BIN) $(CLI_BIN) $(HASH_FILE)
	@echo "binaries removed."

clean-all: clean
	rm -rf -- $(PROTO_OUT) $(CONNECT_OUT)
	@echo "proto generated files removed."

# -- Help -------------------------------------------------------------------

help:
	@sed -n 's/^# //p; s/^#$$//p' $(MAKEFILE_LIST)
