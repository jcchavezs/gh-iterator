.PHONY: install-tools
install-tools: ## Install tools
	@curl -sSfL https://golangci-lint.run/install.sh | sh -s v2.15.0

check-tool-%:
	@command -v $* > /dev/null || test -x ./bin/$* || (echo "Install $* with 'make install-tools'"; exit 1 )

.PHONY: lint
lint: check-tool-golangci-lint
	@tool=$$(command -v golangci-lint || echo ./bin/golangci-lint); $$tool run ./...

.PHONY: test
test:
	@go test -race ./...

.PHONY: build-examples
build-examples: ## Build examples
	@mkdir -p ./bin/examples
	@for dir in examples/*/; do \
		name=$$(basename $$dir); \
		echo "Building example $$name..."; \
		go build -o ./bin/examples/$$name ./$$dir || exit 1; \
	done
