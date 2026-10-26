.PHONY: install-tools
install-tools: ## Download the tools declared in go.mod
	@go mod download
	@go build -o /dev/null github.com/robertkrimen/godocdown/godocdown
