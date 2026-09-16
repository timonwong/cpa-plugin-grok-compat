PLUGIN_NAME := cpa-plugin-grok-compat
BIN_DIR := bin

UNAME_S := $(shell uname -s)
ifeq ($(OS),Windows_NT)
PLUGIN_EXT := dll
else ifeq ($(UNAME_S),Darwin)
PLUGIN_EXT := dylib
else
PLUGIN_EXT := so
endif

.PHONY: build test clean

build:
	mkdir -p $(BIN_DIR)
	go build -buildmode=c-shared -o $(BIN_DIR)/$(PLUGIN_NAME).$(PLUGIN_EXT) .
	rm -f $(BIN_DIR)/$(PLUGIN_NAME).h

test:
	go test ./...

clean:
	rm -rf $(BIN_DIR)
