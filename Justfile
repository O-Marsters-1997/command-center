default:
    @just --list

build:
    go build -o bin/cc ./cmd/cc

run *args:
    go run ./cmd/cc {{args}}

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
