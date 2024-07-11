package main

import (
	"log"
	"os"

	"github.com/go-resty/resty/v2"
)

func main() {
	openaiAPIKey := os.Getenv("OPENAI_API_KEY")
	serverURL := "https://api.openai.com/v1/usage"

	client := resty.New()

	responseBody := struct {
		Usage struct {
			TotalTokens     int `json:"total_tokens"`
			RemainingTokens int `json:"remaining_tokens"`
		} `json:"usage"`
	}{}

	log.Println("Sending request to OpenAI API for usage info...")
	resp, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("Authorization", "Bearer "+openaiAPIKey).
		SetResult(&responseBody).
		Get(serverURL)

	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	log.Printf("OpenAI API response status: %d", resp.StatusCode())
	log.Printf("OpenAI API response body: %s", resp.String())

	log.Printf("Total Tokens: %d", responseBody.Usage.TotalTokens)
	log.Printf("Remaining Tokens: %d", responseBody.Usage.RemainingTokens)
}
