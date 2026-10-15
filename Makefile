# Developer tasks. Everything here works offline with the Go toolchain and
# Node (for the UI unit tests); nothing is downloaded or installed.

GO        ?= go
NODE      ?= node
BIN       ?= atlas
# -p 2 keeps peak memory modest on small machines.
GOTEST    ?= $(GO) test -p 2

.PHONY: all build test test-go test-web vet fmt-check lint-curriculum cross fixtures serve-dev clean

all: fmt-check vet test build

build:
	$(GO) build -o $(BIN) ./cmd/atlas

test: test-go test-web

test-go:
	$(GOTEST) ./...

# The glob is quoted so Node expands it; a bare directory argument is not
# accepted by every Node version.
test-web:
	$(NODE) --test 'web/test/*.test.mjs'

vet:
	$(GO) vet ./...

fmt-check:
	@out="$$(gofmt -l cmd internal curriculum web fixtures)"; \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

lint-curriculum: build
	$(abspath $(BIN)) curriculum lint

# Linux arm64 is a full target; darwin must compile (snapshots and fixtures
# only); riscv64 is best effort.
cross:
	GOOS=linux GOARCH=arm64 $(GO) build ./...
	GOOS=linux GOARCH=riscv64 $(GO) build ./...
	GOOS=darwin GOARCH=arm64 $(GO) build ./...

# Regenerate fixtures/snapshots/<root>.json from every fixture root, the same
# way the golden test in cmd/atlas does: default redaction, and the time
# recorded in each root's _meta.json ("now"), which a fixture root always
# uses, so the output is byte-for-byte reproducible. Review the diff before
# committing. live-sample.json is not regenerated here: it is a capture of a
# real machine ("atlas collect -o fixtures/snapshots/live-sample.json"),
# which must be read through for identifying strings before it is committed;
# TestCommittedSnapshotsAreRedacted only checks what the patterns know.
fixtures: build
	@for root in fixtures/roots/*/; do \
		[ -f "$$root/_meta.json" ] || continue; \
		name=$$(basename "$$root"); \
		echo "fixtures/snapshots/$$name.json"; \
		$(abspath $(BIN)) collect --root "$$root" -o "fixtures/snapshots/$$name.json" || exit 1; \
		chmod 644 "fixtures/snapshots/$$name.json"; \
	done

# Serve the interface from web/ on disk instead of the embedded copy, so
# edits show on reload. Override the address with ADDR=127.0.0.1:PORT.
ADDR ?= 127.0.0.1:7373
serve-dev: build
	ATLAS_WEB_DIR=web $(abspath $(BIN)) serve --addr $(ADDR)

clean:
	rm -f $(BIN)
