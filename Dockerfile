# Stage 1: Build binary
FROM golang:1.23.6-alpine AS builder

WORKDIR /app
RUN apk add --no-cache build-base git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=mod -ldflags="-s -w" -o /app/bin/server ./cmd/server

# Stage 2: Production image
FROM alpine:3.20

WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/bin/server /app/server

EXPOSE 8080
VOLUME ["/app/data"]

ENTRYPOINT ["/app/server"]
CMD ["-port", "8080", "-dir", "/app/data"]
