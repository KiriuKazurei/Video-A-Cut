GO ?= go
TEST_FLAGS ?= -count=1

.PHONY: all build test vet fmt tidy clean

all: fmt vet build test

build:
	$(GO) -C control-plane build ./...

test:
	$(GO) -C control-plane test $(TEST_FLAGS) ./...

vet:
	$(GO) -C control-plane vet ./...

fmt:
	$(GO) -C control-plane fmt ./...

tidy:
	$(GO) -C control-plane mod tidy

clean:
	$(GO) -C control-plane clean
