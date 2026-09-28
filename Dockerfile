# Stage 1: Build binary
FROM golang:alpine AS builder

WORKDIR /app
RUN apk add --no-cache build-base git

COPY go.mod go.sum* ./
RUN go mod download || true

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/bin/server cmd/server/main.go

# Stage 2: Production scratch image
FROM alpine:3.20

WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/bin/server /app/server

EXPOSE 8080 8081 8082
VOLUME ["/app/data"]

ENTRYPOINT ["/app/server"]
CMD ["-port", "8080", "-dir", "/app/data"]
