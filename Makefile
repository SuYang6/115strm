GO ?= go

.PHONY: web build test linux-arm64 docker

web:
	cd web && npm install && npm run build

build: web
	$(GO) build -o bin/115strm ./cmd/server

test:
	$(GO) test ./...

linux-arm64: web
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags="-s -w" -o bin/115strm-linux-arm64 ./cmd/server

docker:
	docker compose -f deploy/docker-compose.yml build
