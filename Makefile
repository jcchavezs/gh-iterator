.PHONY: install-tools
install-tools: ## Install tools
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.5.0
	@$(MAKE) -C internal/skill-iterating-repositories install-tools

check-tool-%: ## Check if a tool is installed
	@which $* > /dev/null || (echo "Install $* with 'make install-tools'"; exit 1 )

.PHONY: lint
lint: check-tool-golangci-lint ## Run linter
	@golangci-lint run ./...

.PHONY: test
test: ## Run tests
	@go test ./...

.PHONY: build-examples
build-examples: ## Build the examples
	@mkdir -p bin/examples
	@for example in examples/*; do \
		[ -d "$$example" ] || continue; \
		echo "Building $$example"; \
		(cd "$$example" && go build -o ../../bin/examples/`basename $$example`) || exit 1; \
	done

.PHONY: generate-skill
generate-skill: ## Regenerate the godoc and examples table of the iterating-repositories skill
	@go build -C internal/skill-iterating-repositories -o $(CURDIR)/bin/skill-iterating-repositories .
	@./bin/skill-iterating-repositories

.PHONY: generate
generate: generate-skill ## Run all generate tasks

.PHONY: all
all: generate lint test build-examples
