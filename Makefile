install-tools: ## Install tools
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.5.0

check-tool-%:
	@which $* > /dev/null || (echo "Install $* with 'make install-tools'"; exit 1 )

lint: check-tool-golangci-lint
	@golangci-lint run ./...

test:
	@go test ./...

build-examples: ## Build examples
	@for dir in examples/*/; do \
		name=$$(basename $$dir); \
		echo "Building $$name..."; \
		go build -o ./bin/examples/$$name ./$$dir || exit 1; \
	done

.PHONY: build-examples