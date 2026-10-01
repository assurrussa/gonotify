.DEFAULT_GOAL := check
ifneq ($(strip $(GO_SHARED_CACHE_ROOT)),)
export GOCACHE ?= $(GO_SHARED_CACHE_ROOT)/build
export GOMODCACHE ?= $(GO_SHARED_CACHE_ROOT)/mod
export GOLANGCI_LINT_CACHE ?= $(GO_SHARED_CACHE_ROOT)/lint
endif
# Lazy evaluation: anonymous probe targets must not run Go before isolation.
GO_MODULE = $(shell go list -m)
GO_FILES = $(shell find . -type f -name '*.go' -not -path './tmp/*' -not -path './vendor/*')
VERSION ?=
export VERSION

.PHONY: check ci-check tidy tidy-check generate fmt lint vet test test-race test-trimpath bench-all cover-html public-probe-test public-consumer-local public-consumer-published publish-readiness release-readiness

check: tidy generate fmt vet lint test test-race test-trimpath cover-html public-probe-test public-consumer-local

ci-check: tidy-check public-probe-test
	go test -mod=readonly -count=1 ./...

tidy:
	go mod tidy

tidy-check:
	go mod tidy -diff

generate:
	go generate ./...

fmt:
	go fmt ./...
	gofumpt -l -w $(GO_FILES)
	gci write -s standard -s default -s "prefix($(GO_MODULE))" .

lint:
	golangci-lint run -v --fix --timeout=5m ./...

vet:
	go vet ./...

test:
	go test ./...

test-race:
	go test -race -count=5 ./...

test-trimpath:
	go test -mod=readonly -trimpath -race -count=1 ./templates

bench-all:
	go test -bench=. -benchmem ./...

cover-html:
	@go test -coverprofile=./coverage.text -covermode=atomic $(shell go list ./...)
	@go tool cover -html=./coverage.text -o ./cover.html && rm ./coverage.text

public-probe-test:
	bash scripts/test-public-consumer.sh

public-consumer-local:
	bash scripts/public-consumer.sh local

public-consumer-published:
	bash scripts/public-consumer.sh published "$$VERSION"

publish-readiness: check
	@git diff --exit-code

release-readiness: publish-readiness public-consumer-published
