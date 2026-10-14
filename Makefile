test:
	@echo "Running tests (with -race flag on)"
	@go test ./... -race

generate:
	@echo "Generating commands code: runtime, doc, etc."
	@go generate gen/aws/generators/main.go

# arm64 by default: Apple Silicon and Graviton are the machines this is built for.
# Override for anything else, e.g. `make build GOARCH=amd64`, or with GOARCH in the
# environment. Only the binary is cross-built: make would otherwise export GOARCH to
# every recipe, and generate and test have to run on the host.
GOARCH ?= arm64
unexport GOARCH

build: generate test
	@echo "Building application binary for $$(go env GOOS)/$(GOARCH)"
	@GOARCH=$(GOARCH) go build -trimpath -o awless-ro
