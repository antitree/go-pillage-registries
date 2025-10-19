## Multi-stage Dockerfile for pilreg CLI

# Builder stage uses official Go image to compile a static binary
FROM golang:1.24-alpine@sha256:8f8959f38530d159bf71d0b3eb0c547dc61e7959d8225d1599cf762477384923 AS builder
WORKDIR /src

# Cache and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire project and build
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /pilreg ./cmd/pilreg

# Final stage: lightweight Alpine image
FROM alpine:latest@sha256:4b7ce07002c69e8f3d704a9c5d6fd3053be500b7f1c69fc0d80990c2ad8dd412
RUN apk add --no-cache ca-certificates
COPY --from=builder /pilreg /usr/local/bin/pilreg
ENTRYPOINT ["pilreg"]
CMD ["--help"]
