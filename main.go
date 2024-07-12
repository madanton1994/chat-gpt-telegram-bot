package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/go-resty/resty/v2"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/pkoukk/tiktoken-go"
)

var (
	openaiAPIKey string
	serverURL    string
	modelMap     = make(map[int64]string) // Stores the model for each chat ID
)

func main() {
	telegramToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	openaiAPIKey = os.Getenv("OPENAI_API_KEY")
	serverURL = os.Getenv("SERVER_URL")
	webhookURL := os.Getenv("WEBHOOK_URL")
	useWebhook := os.Getenv("USE_WEBHOOK")

	validateEnvVars(telegramToken, openaiAPIKey, serverURL)

	bot, err := tgbotapi.NewBotAPI(telegramToken)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)

	if useWebhook == "true" && webhookURL != "" {
		setupWebhook(bot, webhookURL)
	} else {
		setupPolling(bot)
	}
}

func validateEnvVars(vars ...string) {
	for _, v := range vars {
		if v == "" {
			log.Fatalf("%s is not set", v)
		}
	}
}

func setupWebhook(bot *tgbotapi.BotAPI, webhookURL string) {
	webhookConfig, err := tgbotapi.NewWebhook(webhookURL)
	if err != nil {
		log.Fatal(err)
	}
	_, err = bot.Request(webhookConfig)
	if err != nil {
		log.Fatal(err)
	}

	updates := bot.ListenForWebhook("/")
	go http.ListenAndServe(":8080", nil)
	log.Printf("Listening on :8080")

	processUpdates(bot, updates)
}

func setupPolling(bot *tgbotapi.BotAPI) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)
	processUpdates(bot, updates)
}

func processUpdates(bot *tgbotapi.BotAPI, updates tgbotapi.UpdatesChannel) {
	for update := range updates {
		if update.Message != nil {
			handleUpdate(bot, update)
		}
	}
}

func handleUpdate(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	if update.Message == nil {
		return
	}
	text := update.Message.Text
	log.Printf("Received message: %s", text)

	switch {
	case text == "🚀 Start":
		sendWelcomeMessage(bot, update.Message.Chat.ID)
	case text == "ℹ️ Help":
		sendHelpMessage(bot, update.Message.Chat.ID)
	case text == "📊 Status":
		sendStatusMessage(bot, update.Message.Chat.ID)
	case text == "⚙️ Settings":
		sendSettingsMenu(bot, update.Message.Chat.ID)
	case text == "🔙 Back":
		sendWelcomeMessage(bot, update.Message.Chat.ID)
	case strings.HasPrefix(text, "Model: "):
		setModel(bot, update.Message.Chat.ID, strings.TrimPrefix(text, "Model: "))
	default:
		response := getChatGPTResponse(update.Message.Chat.ID, text)
		sendMessage(bot, update.Message.Chat.ID, response)
	}
}

func setModel(bot *tgbotapi.BotAPI, chatID int64, model string) {
	modelMap[chatID] = model
	sendMessage(bot, chatID, "Model set to "+model)
}

func sendWelcomeMessage(bot *tgbotapi.BotAPI, chatID int64) {
	msg := "👋 Welcome! I am your ChatGPT bot. You can use the following commands:"
	sendMessageWithKeyboard(bot, chatID, msg, mainMenuKeyboard())
}

func sendHelpMessage(bot *tgbotapi.BotAPI, chatID int64) {
	msg := "ℹ️ Here is a list of commands you can use:\n/start - Start the bot\n/help - Show this help message\n/status - Show bot status\n/settings - Show settings"
	sendMessageWithKeyboard(bot, chatID, msg, mainMenuKeyboard())
}

func sendStatusMessage(bot *tgbotapi.BotAPI, chatID int64) {
	model := getCurrentModel(chatID)
	msg := "📊 All systems are operational.\nCurrent model: " + model
	sendMessageWithKeyboard(bot, chatID, msg, mainMenuKeyboard())
}

func sendSettingsMenu(bot *tgbotapi.BotAPI, chatID int64) {
	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Model: gpt-3.5-turbo"),
			tgbotapi.NewKeyboardButton("Model: gpt-3.5-turbo-16k"),
			tgbotapi.NewKeyboardButton("Model: gpt-4"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Model: gpt-4o"),
			tgbotapi.NewKeyboardButton("Model: gpt-4-1106-preview"),
			tgbotapi.NewKeyboardButton("Model: gpt-4-vision-preview"),
		),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("🔙 Back")),
	)
	sendMessageWithKeyboard(bot, chatID, "⚙️ Select a model:", keyboard)
}

func getChatGPTResponse(chatID int64, message string) string {
	client := resty.New()
	model := getCurrentModel(chatID)

	messages := []map[string]string{
		{"role": "user", "content": message},
	}

	requestBody := map[string]interface{}{
		"model":    model,
		"messages": messages,
	}

	responseBody := struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Param   string `json:"param"`
			Code    string `json:"code"`
		} `json:"error"`
	}{}

	log.Println("Sending request to OpenAI API...")
	log.Printf("Request body: %v", requestBody)

	// Count tokens before sending the request
	nInputTokens, nOutputTokens, err := countTokensFromMessages(messages, "", model)
	if err != nil {
		log.Printf("Error counting tokens: %v", err)
		return "An error occurred while processing your request."
	}
	log.Printf("Input tokens: %d, Output tokens: %d", nInputTokens, nOutputTokens)

	resp, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("Authorization", "Bearer "+openaiAPIKey).
		SetBody(requestBody).
		SetResult(&responseBody).
		Post(serverURL + "/v1/chat/completions")

	if err != nil {
		log.Printf("Error: %v", err)
		return "An error occurred while processing your request."
	}

	log.Printf("OpenAI API response status: %d", resp.StatusCode())
	log.Printf("OpenAI API response body: %s", resp.String())

	if resp.StatusCode() == 429 {
		return "❌ You exceeded your current quota. Please check your plan and billing details."
	}

	if responseBody.Error.Message != "" {
		log.Printf("OpenAI API error: %s", responseBody.Error.Message)
		return "❌ An error occurred while processing your request."
	}

	if len(responseBody.Choices) > 0 {
		answer := responseBody.Choices[0].Message.Content

		// Count tokens for the answer
		_, nOutputTokens, err = countTokensFromMessages(nil, answer, model)
		if err != nil {
			log.Printf("Error counting output tokens: %v", err)
			return "An error occurred while processing your request."
		}
		log.Printf("Output tokens: %d", nOutputTokens)

		return formatAsTelegramCode(answer)
	}

	return "❌ I couldn't process your request."
}

func getCurrentModel(chatID int64) string {
	model, exists := modelMap[chatID]
	if !exists {
		return "gpt-3.5-turbo" // Default model
	}
	return model
}

func sendMessage(bot *tgbotapi.BotAPI, chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	bot.Send(msg)
}

func sendMessageWithKeyboard(bot *tgbotapi.BotAPI, chatID int64, text string, keyboard tgbotapi.ReplyKeyboardMarkup) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func mainMenuKeyboard() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🚀 Start"),
			tgbotapi.NewKeyboardButton("ℹ️ Help"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 Status"),
			tgbotapi.NewKeyboardButton("⚙️ Settings"),
		),
	)
}

func formatAsTelegramCode(content string) string {
	re := regexp.MustCompile("(?s)```(.*?)```")
	return re.ReplaceAllStringFunc(content, func(m string) string {
		code := re.FindStringSubmatch(m)[1]
		return "<pre>" + code + "</pre>"
	})
}

// Function to count tokens
func countTokensFromMessages(messages []map[string]string, answer, model string) (int, int, error) {
	encoding, err := tiktoken.EncodingForModel(model)
	if err != nil {
		return 0, 0, err
	}

	var tokensPerMessage, tokensPerName int
	switch model {
	case "gpt-3.5-turbo-16k", "gpt-3.5-turbo":
		tokensPerMessage = 4
		tokensPerName = -1
	case "gpt-4", "gpt-4-1106-preview", "gpt-4-vision-preview", "gpt-4o":
		tokensPerMessage = 3
		tokensPerName = 1
	default:
		return 0, 0, fmt.Errorf("unknown model: %s", model)
	}

	nInputTokens := 0
	if messages != nil {
		for _, message := range messages {
			nInputTokens += tokensPerMessage
			if content, ok := message["content"]; ok {
				nInputTokens += len(encoding.Encode(content))
			}
			if name, ok := message["name"]; ok {
				nInputTokens += tokensPerName
				nInputTokens += len(encoding.Encode(name))
			}
		}
	}

	nInputTokens += 2

	nOutputTokens := 1 + len(encoding.Encode(answer))

	return nInputTokens, nOutputTokens, nil
}
