package main

import (
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/go-resty/resty/v2"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var openaiAPIKey string
var serverURL string

var chatHistories = make(map[int64][]string)
var chatHistoriesMutex sync.Mutex

func main() {
	telegramToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	openaiAPIKey = os.Getenv("OPENAI_API_KEY")
	serverURL = os.Getenv("SERVER_URL")
	webhookURL := os.Getenv("WEBHOOK_URL")
	useWebhook := os.Getenv("USE_WEBHOOK")

	if telegramToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is not set")
	}
	if openaiAPIKey == "" {
		log.Fatal("OPENAI_API_KEY is not set")
	}
	if serverURL == "" {
		log.Fatal("SERVER_URL is not set")
	}

	log.Printf("Using OpenAI API Key: %s", openaiAPIKey)

	bot, err := tgbotapi.NewBotAPI(telegramToken)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)

	if useWebhook == "true" && webhookURL != "" {
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

		for update := range updates {
			if update.Message != nil {
				handleUpdate(bot, update)
			}
		}
	} else {
		u := tgbotapi.NewUpdate(0)
		u.Timeout = 60

		updates := bot.GetUpdatesChan(u)

		for update := range updates {
			if update.Message != nil {
				handleUpdate(bot, update)
			}
		}
	}
}

func handleUpdate(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	if update.Message != nil {
		text := update.Message.Text
		log.Printf("Received message: %s", text)

		if strings.HasPrefix(text, "/") {
			handleCommand(bot, update.Message.Chat.ID, text)
		} else if text == "🔙 Back" {
			sendMainMenu(bot, update.Message.Chat.ID)
		} else {
			response := getChatGPTResponse(update.Message.Chat.ID, text)
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, response)
			msg.ParseMode = "HTML"
			bot.Send(msg)
			saveChatHistory(update.Message.Chat.ID, text)
		}
	}
}

func handleCommand(bot *tgbotapi.BotAPI, chatID int64, text string) {
	switch text {
	case "/start":
		sendWelcomeMessage(bot, chatID)
	case "/help":
		sendHelpMessage(bot, chatID)
	case "/status":
		sendStatusMessage(bot, chatID)
	case "/settings":
		sendSettingsMenu(bot, chatID)
	default:
		msg := tgbotapi.NewMessage(chatID, "Unknown command. Use /help to see available commands.")
		bot.Send(msg)
	}
}

func sendWelcomeMessage(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "👋 Welcome! I am your ChatGPT bot. You can use the following commands:")
	msg.ReplyMarkup = mainMenuKeyboard()
	bot.Send(msg)
}

func sendHelpMessage(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "ℹ️ Here is a list of commands you can use:\n/start - Start the bot\n/help - Show this help message\n/status - Show bot status\n/settings - Show settings")
	msg.ReplyMarkup = mainMenuKeyboard()
	bot.Send(msg)
}

func sendStatusMessage(bot *tgbotapi.BotAPI, chatID int64) {
	model := getCurrentModel(chatID)
	msg := tgbotapi.NewMessage(chatID, "📊 All systems are operational.\nCurrent model: "+model)
	msg.ReplyMarkup = mainMenuKeyboard()
	bot.Send(msg)
}

func sendSettingsMenu(bot *tgbotapi.BotAPI, chatID int64) {
	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Model: gpt-3.5-turbo"),
			tgbotapi.NewKeyboardButton("Model: gpt-4"),
		),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("🔙 Back")),
	)

	msg := tgbotapi.NewMessage(chatID, "⚙️ Select a model:")
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func getChatGPTResponse(chatID int64, message string) string {
	client := resty.New()

	model := getCurrentModel(chatID)

	requestBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": message},
		},
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
		return formatAsTelegramCode(responseBody.Choices[0].Message.Content)
	}

	return "❌ I couldn't process your request."
}

func saveChatHistory(chatID int64, message string) {
	chatHistoriesMutex.Lock()
	defer chatHistoriesMutex.Unlock()
	chatHistories[chatID] = append(chatHistories[chatID], message)
}

func getCurrentModel(chatID int64) string {
	chatHistoriesMutex.Lock()
	defer chatHistoriesMutex.Unlock()

	history := chatHistories[chatID]
	for _, msg := range history {
		if strings.HasPrefix(msg, "Model: ") {
			return strings.TrimPrefix(msg, "Model: ")
		}
	}

	return "gpt-3.5-turbo" // Default model
}

func sendMainMenu(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "Main menu")
	msg.ReplyMarkup = mainMenuKeyboard()
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
