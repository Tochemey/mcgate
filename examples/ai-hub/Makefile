# mcgate AI Hub example
# Real-world multi-tenant AI tool hub covering ingress, egress, admin API,
# pluggable policy, credentials, audit, runtime config, and telemetry.

.PHONY: run build clean help

BINARY := bin/ai-hub-example
MODULE := github.com/tochemey/mcgate

help:
	@echo "mcgate AI Hub example"
	@echo ""
	@echo "Targets:"
	@echo "  run   - Run the example (requires Node.js and npx)"
	@echo "  build - Build the binary"
	@echo "  clean - Remove built binary"
	@echo ""
	@echo "Optional: start the HTTP tool server first:"
	@echo "  npx -y @modelcontextprotocol/server-everything streamableHttp"

run:
	go run $(MODULE)/examples/ai-hub

build:
	@mkdir -p bin
	go build -o $(BINARY) $(MODULE)/examples/ai-hub

clean:
	rm -rf bin
