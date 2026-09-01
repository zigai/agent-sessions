golangci_lint_version := "v2.13.2"
golangci_lint := "go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@" + golangci_lint_version
actionlint_version := "v1.7.12"
actionlint := "go run github.com/rhysd/actionlint/cmd/actionlint@" + actionlint_version
goreleaser_version := "v2.13.3"

_:
    @just help

# List available commands
help:
    @just --list

# Run tests
test:
    go test ./...

# Run a selected Go test package with optional arguments
test-package package="./..." args="":
    go test {{ args }} {{ package }}

# Run the complete integration suite
test-integration:
    #!/usr/bin/env sh
    set -eu
    if ! command -v tmux >/dev/null 2>&1; then
        echo "Error: tmux is required to run integration tests. Install tmux and retry." >&2
        exit 1
    fi
    go test -count=1 -v -tags=integration ./...

# Validate release artifacts
test-release-artifacts artifact_dir="dist" published_dir="" negative_controls="false":
    AHT_ARTIFACT_DIR="{{ artifact_dir }}" AHT_PUBLISHED_ARTIFACT_DIR="{{ published_dir }}" AHT_ARTIFACT_NEGATIVE_CONTROLS="{{ negative_controls }}" go test -count=1 -tags=integration ./internal/systemtest -run '^TestReleaseArtifacts$'

# Validate release artifacts through the workflow contract
verify-artifact artifact_dir="dist" published_dir="" negative_controls="false":
    just test-release-artifacts "{{ artifact_dir }}" "{{ published_dir }}" "{{ negative_controls }}"
# Run tests and display coverage
coverage:
    #!/usr/bin/env sh
    set -e
    coverage_file=$(mktemp)
    trap 'rm -f "$coverage_file"' EXIT
    go test -coverprofile="$coverage_file" ./...
    go tool cover -func="$coverage_file"

# Run tests with the race detector
race:
    go test -race ./...

# Tidy dependencies
tidy:
    go mod tidy

# Apply automatic fixes
fix:
    {{ golangci_lint }} run --fix

# Check code for lint issues
lint:
    {{ golangci_lint }} run

# Check Justfile formatting
lint-just:
    just --fmt --check

# Validate GitHub Actions workflows
lint-actions:
    {{ actionlint }} .github/workflows/ci.yml .github/workflows/release.yml
# Run all non-mutating checks
check: lint test race
    go mod tidy -diff
    go build -o /dev/null .

# Run the canonical pull request source gate
verify-pr: check lint-just lint-actions test-integration

# Build the project
build:
    go build -o aht .

# Install the project
install:
    go install .

# Remove build artifacts
clean:
    rm -rf aht aht.exe dist/

# Build with local development version metadata
build-dev:
    go build -ldflags "-X github.com/zigai/aht/internal/cli.version=dev -X github.com/zigai/aht/internal/cli.commit=$(git rev-parse --short HEAD) -X github.com/zigai/aht/internal/cli.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o aht .

_goreleaser-version-check:
    #!/usr/bin/env sh
    set -eu
    expected='{{ goreleaser_version }}'
    expected=${expected#v}
    actual=$(goreleaser --version | sed -n 's/^GitVersion:[[:space:]]*//p')
    if [ "$actual" != "$expected" ]; then
        echo "Error: GoReleaser version mismatch: expected $expected, got ${actual:-<missing>}." >&2
        exit 1
    fi

# Run a dry-run release
release-dry-run: _goreleaser-version-check
    goreleaser release --snapshot --clean

# Build and upload a draft release
release-draft: _goreleaser-version-check
    goreleaser release --clean

# Build and validate a release snapshot
verify-main: release-dry-run
    just verify-artifact dist "" true

# Run the canonical tagged-release source gate
verify-release: verify-pr

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

# Release a new patch version
release-patch: _release-check _goreleaser-version-check
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

# Release a new minor version
release-minor: _release-check _goreleaser-version-check
    #!/usr/bin/env sh
    set -e
    latest=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
    major=$(echo "$latest" | sed 's/v//' | cut -d. -f1)
    minor=$(echo "$latest" | sed 's/v//' | cut -d. -f2)
    new="v${major}.$((minor + 1)).0"
    echo "Releasing $new (was $latest)"
    git tag "$new"
    git push origin "$new"

# Release a new major version
release-major: _release-check _goreleaser-version-check
    #!/usr/bin/env sh
    set -e
    latest=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
    major=$(echo "$latest" | sed 's/v//' | cut -d. -f1)
    new="v$((major + 1)).0.0"
    echo "Releasing $new (was $latest)"
    git tag "$new"
    git push origin "$new"

alias release := release-patch

alias cov := coverage
