APP_NAME=audio-extractor
BINARY=bin/$(APP_NAME)

.PHONY: build run test clean docker-build docker-up docker-down templ fmt

build:
	@mkdir -p bin
	go build -o $(BINARY) ./cmd/web

run:
	go run ./cmd/web

test:
	go test ./...

clean:
	rm -rf bin
	rm -rf uploads/* outputs/*

docker-build:
	docker-compose build

docker-up:
	docker-compose up

docker-down:
	docker-compose down

templ:
	templ generate

fmt:
	go fmt ./...
