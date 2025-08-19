# Makefile for git-user — colorful self-documented help
APPNAME := git-user
BINDIR := bin
MAIN := ./cmd/$(APPNAME)/main.go
.DEFAULT_GOAL := help

.PHONY: all build run install fmt vet clean init help banner

### Build & Run
build: ## Build the binary into ./bin/
	@mkdir -p $(BINDIR)
	@go build -o $(BINDIR)/$(APPNAME) $(MAIN)
	@echo "Built \033[1;36m$(BINDIR)/$(APPNAME)\033[0m ✅"

run: build ## Build and run the TUI
	@./$(BINDIR)/$(APPNAME)

install: ## Install globally into GOPATH/bin
	@go install ./...

### Code Quality
fmt: ## Run go fmt on all sources
	@go fmt ./...

vet: ## Run go vet on all sources
	@go vet ./...

### Database Init
init: build ## Bootstrap DB from current git config
	@./$(BINDIR)/$(APPNAME) --init-db
	@echo "\033[1;36mDatabase initialized from current git config ✅\033[0m"

### Cleanup
clean: ## Remove built binaries
	@rm -rf $(BINDIR)

### ASCII Banner (left-aligned)
banner:
	@printf "\033[1;35m"
	@echo " ▄▄ • ▪  ▄▄▄▄▄▄• ▄▌.▄▄ · ▄▄▄ .▄▄▄"
	@echo "▐█ ▀ ▪██ •██  █▪██▌▐█ ▀. ▀▄.▀·▀▄ █·"
	@echo "▄█ ▀█▄▐█· ▐█.▪█▌▐█▌▄▀▀▀█▄▐▀▀▪▄▐▀▀▄"
	@echo "▐█▄▪▐█▐█▌ ▐█▌·▐█▄█▌▐█▄▪▐█▐█▄▄▌▐█•█▌"
	@echo "·▀▀▀▀ ▀▀▀ ▀▀▀  ▀▀▀  ▀▀▀▀  ▀▀▀ .▀  ▀"
	@printf "\033[0m\n"
	@printf "  \033[1;36mgit-user — manage git identities\033[0m\n\n"

### Help
help: ## Show this help (you are here)
	@$(MAKE) banner
	@awk ' \
		BEGIN { FS=":.*##"; ORS=""; } \
		/^[#]{3}[[:space:]]/ { \
			gsub(/^###[[:space:]]*/,""); \
			printf "\n\033[1m\033[33m%s\033[0m\n", $$0; \
		} \
		/^[a-zA-Z0-9_.-]+:.*##/ { \
			gsub(/^[[:space:]]*/,"", $$1); \
			gsub(/^[[:space:]]*/,"", $$2); \
			printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2; \
		}' $(MAKEFILE_LIST)
	@echo ""
	@echo "\033[2mTip:\033[0m use \`make <target>\` (e.g., \`make run\`)."
