# Build context must be the portcullis module root so that all module source is
# available. The Makefile runs: docker build -f examples/cluster/Dockerfile .
# from the module root.
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o gateway ./examples/cluster

FROM alpine:3.21
RUN addgroup -S app && adduser -S -G app app
WORKDIR /home/app
COPY --from=builder /app/gateway .
USER app
ENTRYPOINT ["./gateway"]
