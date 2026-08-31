.PHONY: build web-build dev test integration clean

build: web-build
	go build -o ku-dump ./cmd/ku-dump

web-build:
	npm --prefix web install
	npm --prefix web run build

dev: web-build
	go run ./cmd/ku-dump

test:
	go test ./...

integration:
	go test -tags integration ./integration/...

clean:
	rm -f ku-dump
