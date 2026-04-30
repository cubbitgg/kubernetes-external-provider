REGISTRY             ?= docker.io/gigiozzz
VERSION              ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS              := -ldflags="-s -w -X main.version=$(VERSION)"
GOLANGCI_LINT_VERSION ?= v2.1.6

PROVISIONER_IMG  = $(REGISTRY)/local-disk-provisioner:$(VERSION)
NODE_SCANNER_IMG = $(REGISTRY)/local-disk-node-scanner:$(VERSION)
WEBHOOK_IMG      = $(REGISTRY)/local-disk-webhook:$(VERSION)

MODULES = commonlib cmd-drivers node-scanner provisioner webhook

.PHONY: all build build-provisioner build-scanner build-webhook \
        docker-build docker-push docker-save docker-load \
        test unit-test lint lint-install check clean \
        e2e e2e-run

all: build

## Build all binaries locally
build: build-provisioner build-scanner build-webhook

build-provisioner:
	cd provisioner && CGO_ENABLED=0 GOOS=linux go build $(LDFLAGS) -o ../bin/provisioner ./cmd

build-scanner:
	cd node-scanner && CGO_ENABLED=0 GOOS=linux go build $(LDFLAGS) -o ../bin/node-scanner ./cmd

build-webhook:
	cd webhook && CGO_ENABLED=0 GOOS=linux go build $(LDFLAGS) -o ../bin/webhook ./cmd

## Build all Docker images
docker-build: docker-build-provisioner docker-build-scanner docker-build-webhook

docker-build-provisioner:
	docker build -f provisioner/Dockerfile -t $(PROVISIONER_IMG) .

docker-build-scanner:
	docker build -f node-scanner/Dockerfile -t $(NODE_SCANNER_IMG) .

docker-build-webhook:
	docker build -f webhook/Dockerfile -t $(WEBHOOK_IMG) .

## Push all Docker images
docker-push: docker-push-provisioner docker-push-scanner docker-push-webhook

docker-push-provisioner:
	docker push $(PROVISIONER_IMG)

docker-push-scanner:
	docker push $(NODE_SCANNER_IMG)

docker-push-webhook:
	docker push $(WEBHOOK_IMG)

## Run all tests across all modules
test:
	@for m in $(MODULES); do \
		echo "=== Testing $$m ===" && \
		(cd $$m && go test ./... -v -race) || exit 1; \
	done

## Run unit tests for modules with test files (cmd-drivers, provisioner, webhook)
unit-test:
	@for m in cmd-drivers provisioner webhook; do \
		echo "=== Unit Testing $$m ===" && \
		(cd $$m && go test ./... -v -race) || exit 1; \
	done

## Install golangci-lint if not already present
lint-install:
	@which golangci-lint > /dev/null 2>&1 || \
		go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

## Run linter for all modules
lint: lint-install
	@for m in $(MODULES); do \
		echo "=== Linting $$m ===" && \
		(cd $$m && golangci-lint run ./...) || exit 1; \
	done

## Run lint + go vet for all modules
check: lint
	@for m in $(MODULES); do \
		(cd $$m && go vet ./...) || exit 1; \
	done

## Save provisioner and webhook images to a tar archive for CI transfer
docker-save: docker-build-provisioner docker-build-webhook
	docker save $(PROVISIONER_IMG) $(WEBHOOK_IMG) -o /tmp/docker-images.tar

## Load images from tar archive (produced by docker-save)
docker-load:
	docker load -i /tmp/docker-images.tar

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
