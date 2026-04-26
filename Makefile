REGISTRY   ?= docker.io/gigiozzz
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS    := -ldflags="-s -w -X main.version=$(VERSION)"

PROVISIONER_IMG  = $(REGISTRY)/local-disk-provisioner:$(VERSION)
NODE_SCANNER_IMG = $(REGISTRY)/local-disk-node-scanner:$(VERSION)
WEBHOOK_IMG      = $(REGISTRY)/local-disk-webhook:$(VERSION)

.PHONY: all build build-provisioner build-scanner build-webhook \
        docker-build docker-push test lint clean \
        e2e e2e-run

all: build

## Build all binaries locally
build: build-provisioner build-scanner build-webhook

build-provisioner:
	CGO_ENABLED=0 GOOS=linux go build $(LDFLAGS) -o bin/provisioner ./cmd/provisioner

build-scanner:
	CGO_ENABLED=0 GOOS=linux go build $(LDFLAGS) -o bin/node-scanner ./cmd/node-scanner

build-webhook:
	CGO_ENABLED=0 GOOS=linux go build $(LDFLAGS) -o bin/webhook ./cmd/webhook

## Build all Docker images
docker-build: docker-build-provisioner docker-build-scanner docker-build-webhook

docker-build-provisioner:
	docker build -f Dockerfile.provisioner -t $(PROVISIONER_IMG) .

docker-build-scanner:
	docker build -f Dockerfile.node-scanner -t $(NODE_SCANNER_IMG) .

docker-build-webhook:
	docker build -f Dockerfile.webhook -t $(WEBHOOK_IMG) .

## Push all Docker images
docker-push: docker-push-provisioner docker-push-scanner docker-push-webhook

docker-push-provisioner:
	docker push $(PROVISIONER_IMG)

docker-push-scanner:
	docker push $(NODE_SCANNER_IMG)

docker-push-webhook:
	docker push $(WEBHOOK_IMG)

## Run tests
test:
	go test ./... -v -race

## Run linter (requires golangci-lint)
lint:
	golangci-lint run ./...

## Remove built binaries
clean:
	rm -rf bin/

## Run kuttl e2e tests: builds images, spins up kind cluster, deploys, runs tests
## Override VERSION to tag images differently (e.g. make e2e VERSION=v1.2.3).
## Default is 'latest', which matches the image refs in testdata/00-common-setup/00-deploy.yaml.
e2e: VERSION = latest
e2e: docker-build-provisioner docker-build-webhook
	E2E_VERSION=$(VERSION) KUTTL_TEST=true go test ./tests/e2e/... -v -timeout 300s -count=1

## Run e2e tests without rebuilding images (for faster iteration)
e2e-run:
	KUTTL_TEST=true go test ./tests/e2e/... -v -timeout 300s -count=1
