# mcgate filesystem example
# Prerequisites: Node.js and npx (for @modelcontextprotocol/server-filesystem)

.PHONY: run build run-macos help

BINARY := bin/filesystem-example
MODULE := github.com/tochemey/mcgate

help:
	@echo "mcgate filesystem example"
	@echo ""
	@echo "Targets:"
	@echo "  run       - Run the example (default root: current directory)"
	@echo "  build     - Build the binary"
	@echo "  run-macos - Run with MCP_FS_ROOT=/private/tmp (for /tmp on macOS)"
	@echo "  clean     - Remove built binary"

run:
	go run $(MODULE)/examples/filesystem

build:
	@mkdir -p bin
	go build -o $(BINARY) $(MODULE)/examples/filesystem

run-macos:
	MCP_FS_ROOT=/private/tmp go run $(MODULE)/examples/filesystem

clean:
	rm -rf bin
