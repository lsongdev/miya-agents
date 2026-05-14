package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/lsongdev/openai-go/openai"
	"github.com/lsongdev/openai-go/router"
)

func main() {
	r := router.NewRouter()

	r.AddProvider(&router.Provider{
		Name:    "openai",
		Type:    router.ProviderTypeOpenAI,
		BaseURL: "https://api.openai.com",
		APIKey:  os.Getenv("OPENAI_API_KEY"),
	})

	r.AddProvider(&router.Provider{
		Name:             "anthropic",
		Type:             router.ProviderTypeAnthropic,
		BaseURL:          "https://api.anthropic.com",
		APIKey:           os.Getenv("ANTHROPIC_API_KEY"),
		DefaultMaxTokens: 4096,
	})

	r.OnRequest(func(ctx router.RequestContext, chatReq *openai.ChatCompletionRequest) (string, error) {
		switch chatReq.Model {
		case "gpt-4":
			chatReq.Model = "gpt-4-turbo"
			return "openai", nil
		case "gpt-4o":
			chatReq.Model = "gpt-4o"
			return "openai", nil
		case "claude-sonnet":
			chatReq.Model = "claude-3-7-sonnet-20250219"
			return "anthropic", nil
		case "deepseek":
			chatReq.Model = "deepseek-chat"
			return "openai", nil
		}
		return "", fmt.Errorf("unknown model: %s", chatReq.Model)
	})

	addr := ":8080"
	if v := os.Getenv("ADDR"); v != "" {
		addr = v
	}
	log.Printf("Router listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
