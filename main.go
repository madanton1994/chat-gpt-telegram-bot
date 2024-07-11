package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/go-resty/resty/v2"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
)

var openaiAPIKey string
var serverURL string
var db *sql.DB

func main() {
	telegramToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	openaiAPIKey = os.Getenv("OPENAI_API_KEY")
	serverURL = os.Getenv("SERVER_URL")
	webhookURL := os.Getenv("WEBHOOK_URL")
	useWebhook := os.Getenv("USE_WEBHOOK")
	databaseURL := os.Getenv("DATABASE_URL")

	if telegramToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is not set")
	}
	if openaiAPIKey == "" {
		log.Fatal("OPENAI_API_KEY is not set")
	}
	if serverURL == "" {
		log.Fatal("SERVER_URL is not set")
	}
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	var err error
	db, err = sql.Open("postgres", databaseURL)
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		log.Fatalf("Error connecting to the database: %v", err)
	}

	runMigrations(databaseURL)

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

func runMigrations(databaseURL string) {
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		log.Fatalf("Could not create database driver: %v", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://migrations",
		"postgres", driver)
	if err != nil {
		log.Fatalf("Could not start migration: %v", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("Could not apply migrations: %v", err)
	}
}

func handleUpdate(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	if update.Message != nil {
		text := update.Message.Text

		if strings.HasPrefix(text, "/model") {
			handleModelChange(bot, update)
		} else {
			response := getChatGPTResponse(update.Message.Chat.ID, text)

			msg := tgbotapi.NewMessage(update.Message.Chat.ID, EscapeMarkdownV2(response))
			msg.ParseMode = "MarkdownV2"

			bot.Send(msg)
		}
	}
}

func handleModelChange(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	args := strings.Fields(update.Message.Text)
	if len(args) < 2 {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Usage: /model <model-name>")
		bot.Send(msg)
		return
	}

	model := args[1]

	_, err := db.Exec("INSERT INTO chat_models (chat_id, model) VALUES ($1, $2) ON CONFLICT (chat_id) DO UPDATE SET model = $2", update.Message.Chat.ID, model)
	if err != nil {
		log.Printf("Error updating model: %v", err)
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Failed to update model")
		bot.Send(msg)
		return
	}

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Model changed to "+model)
	bot.Send(msg)
}

func getChatGPTResponse(chatID int64, message string) string {
	client := resty.New()

	var model string
	err := db.QueryRow("SELECT model FROM chat_models WHERE chat_id = $1", chatID).Scan(&model)
	if err != nil {
		if err == sql.ErrNoRows {
			model = "gpt-3.5-turbo" // Установите модель по умолчанию
		} else {
			log.Printf("Error querying model: %v", err)
			return "An error occurred while processing your request."
		}
	}

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
	}{}

	_, err = client.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("Authorization", "Bearer "+openaiAPIKey).
		SetBody(requestBody).
		SetResult(&responseBody).
		Post(serverURL + "/v1/chat/completions")

	if err != nil {
		log.Printf("Error: %v", err)
		return "An error occurred while processing your request."
	}

	if len(responseBody.Choices) > 0 {
		return EscapeMarkdownV2(responseBody.Choices[0].Message.Content)
	}

	return "I couldn't process your request."
}

// EscapeMarkdownV2 экранирует специальные символы для использования в MarkdownV2
func EscapeMarkdownV2(text string) string {
	specialChars := "_*[]()~`>#+-=|{}.!"
	escapedText := strings.Builder{}

	for _, char := range text {
		if strings.ContainsRune(specialChars, char) {
			escapedText.WriteRune('\\')
		}
		escapedText.WriteRune(char)
	}

	return escapedText.String()
}

func formatCode(text string) string {
	if strings.Contains(text, "```") {
		text = strings.ReplaceAll(text, "```", "```")
	}
	return text
}
