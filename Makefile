GO ?= go
OAPI ?= oapi-codegen
BIN := ticktick

.PHONY: build generate test vet fmt smoke clean

build:
	$(GO) build -o $(BIN) ./cmd/ticktick

generate:
	$(OAPI) -package api -generate types,client -o api/openapi.gen.go api/openapi.yaml

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

smoke: build
	bash scripts/smoke.sh

clean:
	rm -f $(BIN)