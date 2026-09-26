GO ?= go
TEST_FLAGS ?= -count=1

.PHONY: all build test vet fmt tidy clean

all: fmt vet build test

build:
	$(GO) build ./...

test:
	$(GO) test $(TEST_FLAGS) ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

tidy:
	cd control-plane && $(GO) mod tidy

clean:
	$(GO) clean -cache
	rm -f video-auto-cut.db video-auto-cut.db-wal video-auto-cut.db-shm
