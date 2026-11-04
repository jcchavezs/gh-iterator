# Go modules in this repository. ./... skips nested modules, so each one is visited.
MODULES := . exec/otelexec

.PHONY: install-tools
install-tools: ## Install tools
	@curl -sSfL https://golangci-lint.run/install.sh | sh -s v2.15.0

check-tool-%: ## Check if a tool is installed
	@command -v $* > /dev/null || test -x ./bin/$* || (echo "Install $* with 'make install-tools'"; exit 1 )

.PHONY: lint
lint: check-tool-golangci-lint ## Run linters
	@root=$$(pwd); tool=$$(command -v golangci-lint || echo $$root/bin/golangci-lint); \
	for m in $(MODULES); do \
		echo "Linting $$m..."; \
		(cd $$m && $$tool run ./...) || exit 1; \
	done

.PHONY: test
test: ## Run tests
	@for m in $(MODULES); do \
		echo "Testing $$m..."; \
		(cd $$m && go test -race ./...) || exit 1; \
	done

.PHONY: tidy
tidy: ## Run go mod tidy in every module
	@for m in $(MODULES); do \
		(cd $$m && go mod tidy) || exit 1; \
	done

.PHONY: build-examples
build-examples: ## Build examples
	@mkdir -p ./bin/examples
	@for dir in examples/*/; do \
		name=$$(basename $$dir); \
		echo "Building example $$name..."; \
		go build -o ./bin/examples/$$name ./$$dir || exit 1; \
	done
	@echo "Building example otelexec-traced..."
	@root=$$(pwd); cd exec/otelexec && go build -o $$root/bin/examples/otelexec-traced ./examples/traced
