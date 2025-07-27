## Multi-stage Dockerfile for pilreg CLI

# Builder stage uses official Go image to compile a static binary
FROM golang:1.24-alpine AS builder
WORKDIR /src

# Cache and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire project and build
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /pilreg ./cmd/pilreg

# Final stage: lightweight Alpine image
FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /pilreg /usr/local/bin/pilreg
ENTRYPOINT ["pilreg"]
CMD ["--help"]
