# BBC 1Xtra -> Spotify (Claude-powered scraper)
#
# Credentials can be supplied either in the web UI at http://localhost:8080
# or via these environment variables (all optional):
#   ANTHROPIC_API_KEY   used for the Claude scraping step
#   SPOTIFY_ID          Spotify app Client ID
#   SPOTIFY_SECRET      Spotify app Client Secret
#
# Your Spotify app must list http://localhost:8080/callback as a Redirect URI.

BINARY := 1xtra-spotify

.PHONY: run build vet tidy clean help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-8s\033[0m %s\n", $$1, $$2}'

run: ## Start the web app on http://localhost:8080
	go run .

build: ## Compile the binary
	go build -o $(BINARY) .

vet: ## Run go vet
	go vet ./...

tidy: ## Tidy go.mod / go.sum
	go mod tidy

clean: ## Remove the built binary
	rm -f $(BINARY)
