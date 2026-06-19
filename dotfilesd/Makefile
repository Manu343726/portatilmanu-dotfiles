# dotfilesd - dotfiles runtime daemon & CLI
#
# Targets:
#   build          Build daemon and CLI binaries
#   install        Install binaries to ~/.local/bin/
#   proto          Regenerate protocol buffer code
#   deps           Install Go dependencies
#   clean          Remove build artifacts
#   setup          Full install: build + install + service + start
#   service-install  Install systemd user service
#   service-start    Enable and start the daemon
#   service-stop     Stop and disable the daemon
#   service-logs     View daemon logs
#

BINARY_DIR  := $(HOME)/.local/bin
DAEMON_BIN  := $(BINARY_DIR)/dotfilesd
CLI_BIN     := $(BINARY_DIR)/dotfilesctl
SERVICE_DIR := $(HOME)/.config/systemd/user
SERVICE_FILE := $(SERVICE_DIR)/dotfilesd.service
GO          := go
GOFLAGS     := -ldflags="-s -w"
CMD_DIR     := ./cmd

export PATH := $(HOME)/go/bin:$(PATH)

.PHONY: all build install proto deps clean setup service-install service-start service-stop service-logs help

all: build

# -- Build -------------------------------------------------------------------

build: $(DAEMON_BIN) $(CLI_BIN)

$(DAEMON_BIN):
	@mkdir -p $(BINARY_DIR)
	$(GO) build $(GOFLAGS) -o $@ $(CMD_DIR)/dotfilesd

$(CLI_BIN):
	@mkdir -p $(BINARY_DIR)
	$(GO) build $(GOFLAGS) -o $@ $(CMD_DIR)/dotfilesctl

build-dev:
	$(GO) build -o $(DAEMON_BIN) $(CMD_DIR)/dotfilesd
	$(GO) build -o $(CLI_BIN) $(CMD_DIR)/dotfilesctl

# -- Install ----------------------------------------------------------------

install: build
	@echo "binaries installed to $(BINARY_DIR)/"

# -- Proto ------------------------------------------------------------------

PROTO_SRC := proto/dotfilesd/v1/dotfilesdv1/service.proto
PROTO_OUT := proto/dotfilesd/v1/dotfilesdv1/service.pb.go
CONNECT_OUT := proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect/service.connect.go

proto: $(PROTO_OUT) $(CONNECT_OUT)

$(PROTO_OUT) $(CONNECT_OUT): $(PROTO_SRC)
	PATH="$(HOME)/go/bin:$(PATH)" protoc \
		--go_out=. --go_opt=paths=source_relative \
		--connect-go_out=. --connect-go_opt=paths=source_relative \
		$<

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

# -- Setup ------------------------------------------------------------------

setup: deps build install service-start
	@echo ""
	@echo "dotfilesd setup complete!"
	@echo "  daemon:   $(DAEMON_BIN) (running)"
	@echo "  cli:      $(CLI_BIN)"
	@echo ""
	@echo "quick test: dotfilesctl ping"

# -- Clean ------------------------------------------------------------------

clean:
	rm -f $(DAEMON_BIN) $(CLI_BIN)
	@echo "binaries removed."

clean-all: clean
	rm -rf -- $(PROTO_OUT) $(CONNECT_OUT)
	@echo "proto generated files removed."

# -- Help -------------------------------------------------------------------

help:
	@sed -n 's/^# //p; s/^#$$//p' $(MAKEFILE_LIST)
