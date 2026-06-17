@_:
  just --list

test:
    go test ./...

tidy:
    go mod tidy

fix:
    golangci-lint run --fix

lint:
    golangci-lint run

check: test lint

build:
    go build -o agent-sessions .

install:
    go install .

clean:
    rm -rf agent-sessions agent-sessions.exe dist/

build-dev:
    go build -ldflags "-X github.com/zigai/agent-sessions/internal/cli.version=dev -X github.com/zigai/agent-sessions/internal/cli.commit=$(git rev-parse --short HEAD) -X github.com/zigai/agent-sessions/internal/cli.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o agent-sessions .

release-dry-run:
    goreleaser release --snapshot --clean

_release-check:
    #!/usr/bin/env sh
    set -e
    if [ -n "$(git status --porcelain)" ]; then
        echo "Error: uncommitted changes. Commit or stash first." >&2
        exit 1
    fi
    branch=$(git branch --show-current)
    if [ "$branch" != "master" ]; then
        echo "Error: not on master branch (on $branch)" >&2
        exit 1
    fi
    git fetch origin master --tags
    local_head=$(git rev-parse HEAD)
    remote_head=$(git rev-parse origin/master)
    if [ "$local_head" != "$remote_head" ]; then
        echo "Error: local master differs from origin/master. Pull or push first." >&2
        exit 1
    fi
    latest_tag=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
    if [ -n "$latest_tag" ]; then
        tag_commit=$(git rev-parse "$latest_tag"^{})
        if [ "$local_head" = "$tag_commit" ]; then
            echo "Error: HEAD is already tagged as $latest_tag. Make new commits first." >&2
            exit 1
        fi
    fi

release-patch: _release-check
    #!/usr/bin/env sh
    set -e
    latest=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
    major=$(echo "$latest" | sed 's/v//' | cut -d. -f1)
    minor=$(echo "$latest" | sed 's/v//' | cut -d. -f2)
    patch=$(echo "$latest" | sed 's/v//' | cut -d. -f3)
    new="v${major}.${minor}.$((patch + 1))"
    echo "Releasing $new (was $latest)"
    git tag "$new"
    git push origin "$new"

release-minor: _release-check
    #!/usr/bin/env sh
    set -e
    latest=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
    major=$(echo "$latest" | sed 's/v//' | cut -d. -f1)
    minor=$(echo "$latest" | sed 's/v//' | cut -d. -f2)
    new="v${major}.$((minor + 1)).0"
    echo "Releasing $new (was $latest)"
    git tag "$new"
    git push origin "$new"

release-major: _release-check
    #!/usr/bin/env sh
    set -e
    latest=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
    major=$(echo "$latest" | sed 's/v//' | cut -d. -f1)
    new="v$((major + 1)).0.0"
    echo "Releasing $new (was $latest)"
    git tag "$new"
    git push origin "$new"

alias release := release-patch
