default:
    @just --list

build:
    go build -o bin/cc ./cmd/cc

run *args:
    go run ./cmd/cc {{args}}

# Start the Postgres the app and the tests connect to.
up:
    docker compose up -d --wait

down:
    docker compose down

db_url := env_var_or_default("CC_DATABASE_URL", "postgres://cc:cc@localhost:5432/cc?sslmode=disable")
goose := "go run github.com/pressly/goose/v3/cmd/goose@v3.27.3 -dir internal/cc/migrations postgres"

migrate-status:
    {{goose}} "{{db_url}}" status

migrate-up:
    {{goose}} "{{db_url}}" up

migrate-down:
    {{goose}} "{{db_url}}" down

migrate-redo:
    {{goose}} "{{db_url}}" redo

migrate-create name:
    go run github.com/pressly/goose/v3/cmd/goose@v3.27.3 -dir internal/cc/migrations create {{name}} sql

# There is no goose Down for 0001_init.sql, so a hard reset drops the volume and replays Up.
migrate-reset:
    docker compose down -v
    just up
    just migrate-up

test:
    go test ./...

test-e2e:
    go test -tags=e2e ./e2e/...

lint:
    docker run --rm \
        -v "$(pwd):/app" \
        -v command-center-golangci-lint-cache:/root/.cache \
        -v command-center-go-mod-cache:/root/go/pkg/mod \
        -w /app \
        golangci/golangci-lint:latest \
        golangci-lint run ./...

fmt:
    go fmt ./...

tidy:
    go mod tidy

# Rebuild the committed internal/cc/assets/dist/app.css. Needs bun.
assets:
    cd web && bun install && bun run build

# Regenerate the committed internal/cc/ccdb from internal/cc/queries and the schema.
sqlc:
    go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate

clean:
    rm -rf bin

check-conflicts:
    #!/usr/bin/env bash
    branch=$(git rev-parse --abbrev-ref HEAD)
    if [ "$branch" = "main" ]; then exit 0; fi
    if ! git fetch -q origin main; then
        echo "error: could not fetch origin main" >&2
        exit 1
    fi
    out=$(git merge-tree --write-tree --name-only origin/main HEAD)
    status=$?
    if [ "$status" -eq 0 ]; then exit 0; fi
    if [ "$status" -ne 1 ]; then exit "$status"; fi
    echo "error: branch has conflicts with origin/main:" >&2
    echo "$out" | tail -n +2 | awk '/^$/{exit} {print}' >&2
    exit 1

ci:
    just check-conflicts
    just build
    golangci-lint run ./...
    just test
    just test-e2e
