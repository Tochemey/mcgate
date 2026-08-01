# Docker-based replacement for the previous Earthfile.
#
# All generation runs inside a pinned tools image (see Dockerfile.tools) so the
# host only needs Docker and Make installed.

DOCKER       ?= docker
TOOLS_IMAGE  ?= mcgate-tools:latest
TOOLS_FILE   := Dockerfile.tools
TOOLS_STAMP  := .cache/tools.stamp

UID := $(shell id -u)
GID := $(shell id -g)

DOCKER_RUN = $(DOCKER) run --rm \
	--user $(UID):$(GID) \
	-e HOME=/tmp \
	--tmpfs /tmp:exec,size=512m \
	-v $(CURDIR):/workspace \
	-w /workspace \
	$(TOOLS_IMAGE)

.DEFAULT_GOAL := help

.PHONY: help tools protogen protogen-ingress clean

help:
	@echo "Targets:"
	@echo "  tools             Build the toolchain Docker image"
	@echo "  protogen          Generate Go code for protos/egrpc into internal/egress/grpc/testdata"
	@echo "  protogen-ingress  Generate Go code for protos/mcp/v1 into internal/ingress/grpc/pb"
	@echo "  clean             Remove the tools image stamp and any stale gen/ output"

tools: $(TOOLS_STAMP)

$(TOOLS_STAMP): $(TOOLS_FILE)
	@mkdir -p $(@D)
	$(DOCKER) build -f $(TOOLS_FILE) -t $(TOOLS_IMAGE) .
	@touch $@

# Generates the egrpc test service code and binary descriptor used by the
# gRPC egress tests, then publishes them into internal/egress/grpc/testdata.
# The hand-written server.go in that directory is preserved.
protogen: $(TOOLS_STAMP)
	@rm -rf gen
	$(DOCKER_RUN) sh -c '\
		buf generate --template buf.gen.yaml --path protos/egrpc && \
		buf build --path protos/egrpc --output gen/egrpc/test_service.binpb'
	@mkdir -p internal/egress/grpc/testdata
	@cp gen/egrpc/test_service.pb.go \
	    gen/egrpc/test_service_grpc.pb.go \
	    gen/egrpc/test_service.binpb \
	    internal/egress/grpc/testdata/
	@rm -rf gen

# Generates the MCP v1 ingress code into internal/ingress/grpc/pb.
protogen-ingress: $(TOOLS_STAMP)
	@rm -rf gen
	$(DOCKER_RUN) buf generate --template buf.gen.yaml --path protos/mcp/v1
	@mkdir -p internal/ingress/grpc/pb
	@cp gen/mcp/v1/mcp_tool_service.pb.go \
	    gen/mcp/v1/mcp_tool_service_grpc.pb.go \
	    internal/ingress/grpc/pb/
	@rm -rf gen

clean:
	@rm -rf gen $(TOOLS_STAMP)
