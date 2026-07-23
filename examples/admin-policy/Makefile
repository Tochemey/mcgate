# portcullis admin API and pluggable policy example
# Demonstrates GetGatewayStatus, GetToolStatus, ListSessions, DrainTool,
# ResetCircuit, and per-tenant PolicyEvaluator.

.PHONY: run build clean help

BINARY := bin/admin-policy-example
MODULE := github.com/tochemey/portcullis

help:
	@echo "portcullis admin API + pluggable policy example"
	@echo ""
	@echo "Targets:"
	@echo "  run   - Run the example (requires Node.js and npx)"
	@echo "  build - Build the binary"
	@echo "  clean - Remove built binary"

run:
	go run $(MODULE)/examples/admin-policy

build:
	@mkdir -p bin
	go build -o $(BINARY) $(MODULE)/examples/admin-policy

clean:
	rm -rf bin
