package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
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

	log.Printf("Using OpenAI API Key: %s", openaiAPIKey)

	var err error
	db, err = sql.Open("postgres", databaseURL)
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	defer db.Close()

	log.Println("Connecting to the database...")
	err = db.Ping()
	if err != nil {
		log.Fatalf("Error connecting to the database: %v", err)
	}
	log.Println("Successfully connected to the database.")

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
	log.Println("Running database migrations...")
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
	log.Println("Database migrations completed successfully.")
}

func handleUpdate(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	if update.Message != nil {
		text := update.Message.Text

		switch text {
		case "🚀 Start":
			sendWelcomeMessage(bot, update.Message.Chat.ID)
		case "ℹ️ Help":
			sendHelpMessage(bot, update.Message.Chat.ID)
		case "📊 Status":
			sendStatusMessage(bot, update.Message.Chat.ID)
		case "⚙️ Settings":
			sendSettingsMessage(bot, update.Message.Chat.ID)
		case "💬 Chats":
			sendChatList(bot, update.Message.Chat.ID)
		default:
			response := getChatGPTResponse(update.Message.Chat.ID, text)
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, response)
			msg.ParseMode = "HTML"
			bot.Send(msg)
			saveChatHistory(update.Message.Chat.ID, text)
		}
	}
}

func sendWelcomeMessage(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "Welcome! I am your ChatGPT bot. You can use the following commands:")
	msg.ReplyMarkup = mainMenuKeyboard()
	bot.Send(msg)
}

func sendHelpMessage(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "Here is a list of commands you can use:")
	bot.Send(msg)
}

func sendStatusMessage(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "All systems are operational.")
	bot.Send(msg)
}

func sendSettingsMessage(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "Settings options will be available soon.")
	bot.Send(msg)
}

func sendChatList(bot *tgbotapi.BotAPI, chatID int64) {
	log.Println("Fetching chat list...")

	rows, err := db.Query("SELECT DISTINCT chat_id FROM chat_history")
	if err != nil {
		log.Printf("Error fetching chat list: %v", err)
		msg := tgbotapi.NewMessage(chatID, "Error fetching chat list.")
		bot.Send(msg)
		return
	}
	defer rows.Close()

	var chatIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			log.Printf("Error scanning chat ID: %v", err)
			continue
		}
		chatIDs = append(chatIDs, id)
	}

	if len(chatIDs) == 0 {
		msg := tgbotapi.NewMessage(chatID, "No chats found.")
		bot.Send(msg)
		return
	}

	var chatList strings.Builder
	chatList.WriteString("Active chats:\n")
	for _, id := range chatIDs {
		chatList.WriteString("- Chat ID: ")
		chatList.WriteString(strconv.FormatInt(id, 10))
		chatList.WriteString("\n")
	}

	msg := tgbotapi.NewMessage(chatID, chatList.String())
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
		return "You exceeded your current quota. Please check your plan and billing details."
	}

	if responseBody.Error.Message != "" {
		log.Printf("OpenAI API error: %s", responseBody.Error.Message)
		return "An error occurred while processing your request."
	}

	if len(responseBody.Choices) > 0 {
		return formatAsTelegramCode(responseBody.Choices[0].Message.Content)
	}

	return "I couldn't process your request."
}

func saveChatHistory(chatID int64, message string) {
	_, err := db.Exec("INSERT INTO chat_history (chat_id, message) VALUES ($1, $2)", chatID, message)
	if err != nil {
		log.Printf("Error saving chat history: %v", err)
	}
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
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("💬 Chats"),
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
