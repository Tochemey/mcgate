# mcgate ingress example
# Demonstrates the MCP Streamable HTTP ingress handler

.PHONY: run build clean help

BINARY := bin/ingress-example
MODULE := github.com/tochemey/mcgate

help:
	@echo "mcgate ingress example"
	@echo ""
	@echo "Targets:"
	@echo "  run   - Run the example (requires Node.js and npx)"
	@echo "  build - Build the binary"
	@echo "  clean - Remove built binary"

run:
	go run $(MODULE)/examples/ingress

build:
	@mkdir -p bin
	go build -o $(BINARY) $(MODULE)/examples/ingress

clean:
	rm -rf bin
