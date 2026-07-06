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
#   plugin-build   Build a plugin (usage: make plugin-build PLUGIN=weather)
#   plugin-build-all Build all plugins from the plugins directory
#   plugin-clean   Clear the plugin binary cache
#   clean-all      Remove build artifacts + proto + plugin cache
#

BINARY_DIR  := $(HOME)/.local/bin
DAEMON_BIN  := $(BINARY_DIR)/dotfilesd
CLI_BIN     := $(BINARY_DIR)/dotfilesctl
TUIDIAG_BIN := $(BINARY_DIR)/tui-diag
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

build: proto
	@mkdir -p $(BINARY_DIR)
	$(GO) build $(GOFLAGS) -o $(DAEMON_BIN) $(CMD_DIR)/dotfilesd
	$(GO) build $(GOFLAGS) -o $(CLI_BIN) $(CMD_DIR)/dotfilesctl
	$(GO) build $(GOFLAGS) -o $(TUIDIAG_BIN) $(CMD_DIR)/tui-diag
	@mkdir -p $(dir $(HASH_FILE))
	@echo "$(BUILD_HASH)" > $(HASH_FILE)
	@echo "  built daemon cli tui-diag (hash: $(BUILD_HASH))"

# -- Install (fast: skip if hash unchanged, auto-restarts daemon) ------------

install:
	@h=$$(git rev-parse HEAD 2>/dev/null || echo "dev"); \
	if [ -f $(HASH_FILE) ] && [ "$$(cat $(HASH_FILE))" = "$$h" ] && [ -x $(DAEMON_BIN) ] && [ -x $(CLI_BIN) ] && [ -x $(TUIDIAG_BIN) ]; then \
		echo "  binaries up to date (hash: $$h)"; \
	else \
		echo "  building (hash: $$h)..."; \
		$(GO) build $(GOFLAGS) -o $(DAEMON_BIN) $(CMD_DIR)/dotfilesd; \
		$(GO) build $(GOFLAGS) -o $(CLI_BIN) $(CMD_DIR)/dotfilesctl; \
		$(GO) build $(GOFLAGS) -o $(TUIDIAG_BIN) $(CMD_DIR)/tui-diag; \
		echo "$$h" > $(HASH_FILE); \
		$(MAKE) service-restart; \
	fi
	@mkdir -p $(HOME)/.config/dotfilesd $(HOME)/.config/dotfilesctl
	@echo "  installed"

# -- Proto ------------------------------------------------------------------

PROTO_DIR := proto/dotfilesd/v1/dotfilesdv1
PROTO_SRCS := $(wildcard $(PROTO_DIR)/*.proto)
PROTO_PB_GO := $(patsubst $(PROTO_DIR)/%.proto, $(PROTO_DIR)/%.pb.go, $(PROTO_SRCS))
PROTO_CONNECT_DIR := $(PROTO_DIR)/dotfilesdv1connect
PROTO_CONNECT_GO := $(patsubst $(PROTO_DIR)/%.proto, $(PROTO_CONNECT_DIR)/%.connect.go, $(PROTO_SRCS))
PROTO_DOCS_DIR := docs/proto
PROTO_DOCS := $(addprefix $(PROTO_DOCS_DIR)/, $(PROTO_SRCS:.proto=.md))

proto: $(PROTO_PB_GO) $(PROTO_CONNECT_GO) proto-docs

protoc-gen-docs:
	$(GO) build -o $(HOME)/go/bin/protoc-gen-docs ./cmd/protoc-gen-docs

$(PROTO_PB_GO) $(PROTO_CONNECT_GO): $(PROTO_SRCS)
	PATH="$(HOME)/go/bin:$(PATH)" protoc \
		--proto_path=. \
		--go_out=. --go_opt=paths=source_relative \
		--connect-go_out=. --connect-go_opt=paths=source_relative \
		$(PROTO_SRCS)

proto-docs: $(PROTO_SRCS)
	@mkdir -p $(PROTO_DOCS_DIR)
	PATH="$(HOME)/go/bin:$(PATH)" protoc \
		--proto_path=. \
		--docs_out=$(PROTO_DOCS_DIR) \
		$(PROTO_SRCS)
	@echo "  proto docs generated -> $(PROTO_DOCS_DIR)"
	@echo "  removing markdown docs from build output..."
	@# Remove docs for protobuf well-known types that protoc includes.
	rm -rf $(PROTO_DOCS_DIR)/google/
	@# Remove any docs outside of our proto tree (from proto_path scanning).
	@for f in $(PROTO_DOCS_DIR)/*.md; do [ -f "$$f" ] && rm "$$f"; done 2>/dev/null || true
	@# Remove _docs.go and _docs.json files (only plugins need the Go embed; this dir is just docs).
	@find $(PROTO_DOCS_DIR) -name '*_docs.go' -o -name '*_docs.json' -delete 2>/dev/null || true

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

service-restart:
	systemctl --user restart dotfilesd.service
	@echo "service restarted."

service-logs:
	journalctl --user -u dotfilesd.service -f

# -- Tests ------------------------------------------------------------------

test:
	$(GO) test -count=1 ./internal/pkg/... ./test/e2e/...

test-verbose:
	$(GO) test -count=1 -v ./internal/pkg/... ./test/e2e/...

test-short:
	$(GO) test -short -count=1 ./internal/pkg/... ./test/e2e/...

# -- Clean ------------------------------------------------------------------

clean:
	rm -f $(DAEMON_BIN) $(CLI_BIN) $(TUIDIAG_BIN) $(HASH_FILE)
	@echo "binaries removed."

plugin-clean:
	rm -rf $(HOME)/.cache/dotfilesd/plugins/
	@echo "plugin cache cleared."

clean-all: clean plugin-clean
	rm -rf -- $(PROTO_OUT) $(CONNECT_OUT)
	@echo "proto generated files removed."

# -- Plugins ----------------------------------------------------------------

PLUGIN_DIR ?= $(HOME)/.config/dotfilesd/plugins
PLUGIN_CACHE_DIR ?= $(HOME)/.cache/dotfilesd/plugins

# Build a specific plugin: make plugin-build PLUGIN=weather
plugin-build: plugin-proto plugin-oapi-codegen
	@if [ -z "$(PLUGIN)" ]; then \
		echo "Usage: make plugin-build PLUGIN=<name>"; \
		exit 1; \
	fi
	@mkdir -p $(PLUGIN_CACHE_DIR)/$(PLUGIN)
	cd $(PLUGIN_DIR)/$(PLUGIN) && $(GO) build -o $(PLUGIN_CACHE_DIR)/$(PLUGIN)/$(PLUGIN) .
	@echo "plugin '$(PLUGIN)' built."

# Generate OpenAPI client for a specific plugin: make plugin-oapi-codegen PLUGIN=zerotier
plugin-oapi-codegen:
	@if [ -z "$(PLUGIN)" ]; then \
		echo "Usage: make plugin-oapi-codegen PLUGIN=<name>"; \
		exit 1; \
	fi
	@if [ -f "$(PLUGIN_DIR)/$(PLUGIN)/api/zerotier-central.yaml" ]; then \
		PATH="$(HOME)/go/bin:$(PATH)" oapi-codegen \
			-package ztcentral \
			-generate types,client,spec \
			-o "$(PLUGIN_DIR)/$(PLUGIN)/api/ztcentral/client.gen.go" \
			"$(PLUGIN_DIR)/$(PLUGIN)/api/zerotier-central.yaml"; \
		echo "  oapi-codegen for '$(PLUGIN)' done."; \
	else \
		if [ -d "$(PLUGIN_DIR)/$(PLUGIN)/api" ]; then \
			spec=$$(ls "$(PLUGIN_DIR)/$(PLUGIN)/api"/*.yaml 2>/dev/null | head -1); \
			if [ -n "$$spec" ]; then \
				PATH="$(HOME)/go/bin:$(PATH)" oapi-codegen \
					-package ztcentral \
					-generate types,client,spec \
					-o "$(PLUGIN_DIR)/$(PLUGIN)/api/ztcentral/client.gen.go" \
					"$$spec"; \
				echo "  oapi-codegen for '$(PLUGIN)' done."; \
			fi; \
		fi; \
	fi

# Compile proto files for a specific plugin: make plugin-proto PLUGIN=weather
plugin-proto:
	@if [ -z "$(PLUGIN)" ]; then \
		echo "Usage: make plugin-proto PLUGIN=<name>"; \
		exit 1; \
	fi
	@if [ -d "$(PLUGIN_DIR)/$(PLUGIN)/proto/$(PLUGIN)" ]; then \
		cd $(PLUGIN_DIR)/$(PLUGIN) && \
		PATH="$(HOME)/go/bin:$(PATH)" protoc \
			--proto_path=. \
			--go_out=. --go_opt=paths=source_relative \
			--connect-go_out=. --connect-go_opt=paths=source_relative \
			--docs_out=. \
			proto/$(PLUGIN)/*.proto; \
		echo "plugin '$(PLUGIN)' protos compiled."; \
	fi

# Build all plugins found in the plugins directory, respecting dependency order.
# Dependencies are inferred from go.mod `require` directives: if plugin A's
# go.mod has a `require` line pointing to another plugin directory, plugin A
# depends on that plugin and it will be built first.
plugin-build-all:
	@echo "building plugins in dependency order..."
	@rm -f /tmp/plugin-build-order.txt; \
	for dir in $(PLUGIN_DIR)/*/; do \
		name=$$(basename $$dir); \
		if [ -f "$$dir/go.mod" ]; then \
			echo "$$name" >> /tmp/plugin-build-order.txt; \
		fi; \
	done; \
	ordered=""; \
	while [ -s /tmp/plugin-build-order.txt ]; do \
		changed=0; \
		for name in $$(cat /tmp/plugin-build-order.txt); do \
			deps=""; \
			if [ -f "$(PLUGIN_DIR)/$$name/go.mod" ]; then \
				deps=$$(grep "^require\|^.*require" "$(PLUGIN_DIR)/$$name/go.mod" 2>/dev/null | grep -v "dotfilesd" | grep -v "^require (" | grep -v "^)" | sed 's/.*"\(.*\)".*/\1/' | xargs -I{} basename {} 2>/dev/null || true); \
			fi; \
			satisfied=1; \
			for dep in $$deps; do \
				dep_name=$$(basename $$dep); \
				if echo " $$ordered " | grep -qv " $$dep_name "; then \
					if echo " $$ordered " | grep -qv " $$dep_name$$"; then \
						satisfied=0; \
					fi; \
				fi; \
			done; \
			if [ $$satisfied -eq 1 ]; then \
				$(MAKE) plugin-build PLUGIN=$$name; \
				ordered="$$ordered $$name"; \
				changed=1; \
				sed -i "/^$$name$$/d" /tmp/plugin-build-order.txt 2>/dev/null || true; \
			fi; \
		done; \
		if [ $$changed -eq 0 ]; then \
			echo "  WARNING: circular or unsatisfied dependencies, building remaining as-is"; \
			for name in $$(cat /tmp/plugin-build-order.txt); do \
				$(MAKE) plugin-build PLUGIN=$$name; \
			done; \
			break; \
		fi; \
	done
	@echo "all plugins built."

# -- Help -------------------------------------------------------------------

help:
	@sed -n 's/^# //p; s/^#$$//p' $(MAKEFILE_LIST)
