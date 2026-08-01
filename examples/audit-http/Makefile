# mcgate audit + HTTP example
# Prerequisites: Node.js, npx; run server-everything streamableHttp for HTTP tool

.PHONY: run build clean help

BINARY := bin/audit-http-example
MODULE := github.com/tochemey/mcgate

help:
	@echo "mcgate audit + HTTP example"
	@echo ""
	@echo "Targets:"
	@echo "  run   - Run the example (start server-everything streamableHttp in another terminal)"
	@echo "  build - Build the binary"
	@echo "  clean - Remove built binary"

run:
	go run $(MODULE)/examples/audit-http

build:
	@mkdir -p bin
	go build -o $(BINARY) $(MODULE)/examples/audit-http

clean:
	rm -rf bin
