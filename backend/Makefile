APP_ENV ?= dev
LOG_LEVEL ?= info
export APP_ENV LOG_LEVEL

.PHONY: run test vet build docker

run:
	go run ./cmd/server

test:
	go test ./...

vet:
	go vet ./...

build:
	mkdir -p bin
	go build -o bin/server ./cmd/server

docker:
	docker build -t drlife-backend:local .
