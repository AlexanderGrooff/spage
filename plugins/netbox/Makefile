# NetBox Inventory Plugin Makefile
PLUGIN_NAME = spage-inventory-plugin-netbox
BUILD_DIR = ../../build/plugins
INSTALL_DIR = /usr/local/lib/spage/plugins

.PHONY: all build install clean test

all: build

build:
	@echo "Building NetBox inventory plugin..."
	@mkdir -p $(BUILD_DIR)
	go build -buildmode=plugin -o $(BUILD_DIR)/$(PLUGIN_NAME).so .
	@echo "Plugin built successfully: $(BUILD_DIR)/$(PLUGIN_NAME).so"

install: build
	@echo "Installing NetBox inventory plugin..."
	@sudo mkdir -p $(INSTALL_DIR)
	@sudo cp $(BUILD_DIR)/$(PLUGIN_NAME).so $(INSTALL_DIR)/
	@echo "Plugin installed to: $(INSTALL_DIR)/$(PLUGIN_NAME).so"

clean:
	@echo "Cleaning build artifacts..."
	@rm -f $(BUILD_DIR)/$(PLUGIN_NAME).so
	@echo "Clean complete"

test:
	@echo "Testing NetBox plugin..."
	go test -v .
	@echo "Tests complete"

dev-install: build
	@echo "Installing to local plugins directory..."
	@mkdir -p ../../plugins
	@cp $(BUILD_DIR)/$(PLUGIN_NAME).so ../../plugins/
	@echo "Plugin installed for development: ../../plugins/$(PLUGIN_NAME).so"

# Development targets
fmt:
	go fmt ./...

vet:
	go vet ./...

lint: fmt vet

# Show plugin info
info:
	@echo "Plugin: $(PLUGIN_NAME)"
	@echo "Build directory: $(BUILD_DIR)"
	@echo "Install directory: $(INSTALL_DIR)"
	@if [ -f "$(BUILD_DIR)/$(PLUGIN_NAME).so" ]; then \
		echo "Build status: Built"; \
		ls -la $(BUILD_DIR)/$(PLUGIN_NAME).so; \
	else \
		echo "Build status: Not built"; \
	fi
