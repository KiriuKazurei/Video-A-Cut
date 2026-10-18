GO ?= go
TEST_FLAGS ?= -count=1

.PHONY: all build test test-managed vet fmt tidy clean

all: fmt vet build test

build:
	$(GO) -C control-plane build ./...

test:
	$(GO) -C control-plane test $(TEST_FLAGS) ./...

test-managed:
	pwsh -NoProfile -File scripts/TestWorkspace.ps1 -Action Run -Suite go

vet:
	$(GO) -C control-plane vet ./...

fmt:
	$(GO) -C control-plane fmt ./...

tidy:
	$(GO) -C control-plane mod tidy

clean:
	$(GO) -C control-plane clean
