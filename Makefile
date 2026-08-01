.PHONY: build test fmt vet run-example clean tag

VERSION ?= dev
BINARY := loadcannon

build: sync-examples
	go build -ldflags "-s -w -X main.Version=$(VERSION)" -o bin/$(BINARY) ./cmd/loadcannon

# internal/examples/data/ is a go:embed copy of scenarios/ (embed can't
# reference files outside its package directory). Keep them identical.
sync-examples:
	cp scenarios/*.json internal/examples/data/

test: fmt vet build
	./bin/$(BINARY) version

fmt:
	gofmt -l . | tee /tmp/gofmt-out; test ! -s /tmp/gofmt-out

vet:
	go vet ./...

run-example: build
	LOADTEST_TOKEN=dummy ./bin/$(BINARY) validate --scenario scenarios/example-public.json || true

clean:
	rm -rf bin loadcannon-out

# Usage: make tag VERSION=v1.0.0
tag:
	@test "$(VERSION)" != "dev" || (echo "set VERSION=vX.Y.Z" && exit 1)
	git tag -a $(VERSION) -m "$(VERSION)"
	git push origin $(VERSION)
