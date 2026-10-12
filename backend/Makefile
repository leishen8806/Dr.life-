APP_ENV ?= dev
LOG_LEVEL ?= info
export APP_ENV LOG_LEVEL

DB_ENV ?= .env
DB_COMPOSE = docker compose -p drlife-dev --env-file $(DB_ENV) -f ../docker-compose.dev.yml
GOOSE = go run github.com/pressly/goose/v3/cmd/goose@v3.28.0

.PHONY: run test vet build docker db-up db-down db-migrate db-rollback db-reset db-bootstrap

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

db-up:
	$(DB_COMPOSE) up -d --wait postgres redis

db-down:
	$(DB_COMPOSE) down

db-migrate:
	@sh -c 'set -a; . ./$(DB_ENV); set +a; $(GOOSE) -dir migrations postgres "$$MIGRATION_DATABASE_URL" up'

db-rollback:
	@sh -c 'set -a; . ./$(DB_ENV); set +a; $(GOOSE) -dir migrations postgres "$$MIGRATION_DATABASE_URL" down'

db-reset:
	@sh -c 'set -a; . ./$(DB_ENV); set +a; [ "$$APP_ENV" = dev ] || { echo "db-reset is allowed only when APP_ENV=dev" >&2; exit 1; }; $(DB_COMPOSE) down -v; $(DB_COMPOSE) up -d --wait postgres redis; $(GOOSE) -dir migrations postgres "$$MIGRATION_DATABASE_URL" up'

db-bootstrap:
	@sh -c 'set -a; . ./$(DB_ENV); set +a; go run ./scripts/db-bootstrap'
