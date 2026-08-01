# mcgate full-config example
# Demonstrates the majority of gateway configuration options

.PHONY: run build clean help

BINARY := bin/full-config-example
MODULE := github.com/tochemey/mcgate

help:
	@echo "mcgate full-config example"
	@echo ""
	@echo "Targets:"
	@echo "  run   - Run the example (start server-everything streamableHttp for HTTP tool)"
	@echo "  build - Build the binary"
	@echo "  clean - Remove built binary"

run:
	go run $(MODULE)/examples/full-config

build:
	@mkdir -p bin
	go build -o $(BINARY) $(MODULE)/examples/full-config

clean:
	rm -rf bin
