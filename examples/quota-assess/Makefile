# mcgate quota-assess example
# Assesses tenant quota enforcement (RPM + ConcurrentSessions)

.PHONY: run build clean help

BINARY := bin/quota-assess-example
MODULE := github.com/tochemey/mcgate

help:
	@echo "mcgate quota-assess example"
	@echo ""
	@echo "Targets:"
	@echo "  run   - Run tenant quota assessment (RPM + ConcurrentSessions enforcement)"
	@echo "  build - Build the binary"
	@echo "  clean - Remove built binary"

run:
	go run $(MODULE)/examples/quota-assess

build:
	@mkdir -p bin
	go build -o $(BINARY) $(MODULE)/examples/quota-assess

clean:
	rm -rf bin
