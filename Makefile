.PHONY: all build test bench clean

all: build test

build:
	go build -o bin/server cmd/server/main.go

test:
	go test -v -race ./pkg/... ./tests/...

bench:
	go test -bench=. -benchmem ./pkg/... ./tests/benchmarks

clean:
	rm -rf bin/ data/
