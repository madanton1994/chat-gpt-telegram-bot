package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
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
			handleStartCommand(bot, update)
		case "ℹ️ Help":
			handleHelpCommand(bot, update)
		case "📊 Status":
			handleStatusCommand(bot, update)
		case "⚙️ Settings":
			handleSettingsCommand(bot, update)
		case "💬 Chats":
			handleChatsCommand(bot, update)
		case "🔙 Back to Menu":
			handleStartCommand(bot, update)
		default:
			if strings.HasPrefix(text, "/model") {
				handleModelChange(bot, update)
			} else if strings.HasPrefix(text, "/continue_") {
				handleContinueChatCommand(bot, update)
			} else {
				response := getChatGPTResponse(update.Message.Chat.ID, text)
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, formatCodeMarkdown(response))
				msg.ParseMode = "MarkdownV2"
				bot.Send(msg)
			}
		}
	}
}

func handleStartCommand(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	message := "Welcome! I am your ChatGPT bot. You can use the following commands:"
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message)
	msg.ReplyMarkup = mainMenuKeyboard()
	bot.Send(msg)
}

func handleHelpCommand(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	message := "Here are the commands you can use:"
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message)
	msg.ReplyMarkup = mainMenuKeyboard()
	bot.Send(msg)
}

func handleStatusCommand(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	var model string
	err := db.QueryRow("SELECT model FROM chat_models WHERE chat_id = $1", update.Message.Chat.ID).Scan(&model)
	if err != nil {
		if err == sql.ErrNoRows {
			model = "default (gpt-3.5-turbo)"
		} else {
			log.Printf("Error querying model: %v", err)
			model = "unknown"
		}
	}
	message := "Bot is running.\nCurrent model: " + model
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message)
	bot.Send(msg)
}

func handleSettingsCommand(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	var model string
	err := db.QueryRow("SELECT model FROM chat_models WHERE chat_id = $1", update.Message.Chat.ID).Scan(&model)
	if err != nil {
		if err == sql.ErrNoRows {
			model = "gpt-3.5-turbo"
		} else {
			log.Printf("Error querying model: %v", err)
			model = "unknown"
		}
	}
	message := "Settings:\n" +
		"Current model: " + model + "\n" +
		"Use /model <model-name> to change the model."
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message)
	bot.Send(msg)
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

func handleChatsCommand(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	rows, err := db.Query("SELECT DISTINCT chat_id FROM chat_models")
	if err != nil {
		log.Printf("Error fetching chat list: %v", err)
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Failed to fetch chat list")
		bot.Send(msg)
		return
	}
	defer rows.Close()

	var chatIDs []int64
	for rows.Next() {
		var chatID int64
		if err := rows.Scan(&chatID); err != nil {
			log.Printf("Error scanning chat ID: %v", err)
			continue
		}
		chatIDs = append(chatIDs, chatID)
	}

	if len(chatIDs) == 0 {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "No chats found")
		bot.Send(msg)
		return
	}

	message := "Here are the chats you can continue:"
	keyboard := tgbotapi.NewInlineKeyboardMarkup()
	for _, chatID := range chatIDs {
		buttonText := "Chat ID " + strconv.FormatInt(chatID, 10)
		callbackData := "/continue_" + strconv.FormatInt(chatID, 10)
		button := tgbotapi.NewInlineKeyboardButtonData(buttonText, callbackData)
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, tgbotapi.NewInlineKeyboardRow(button))
	}

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message)
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func handleContinueChatCommand(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	chatIDStr := strings.TrimPrefix(update.Message.Text, "/continue_")
	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		log.Printf("Error parsing chat ID: %v", err)
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Invalid chat ID")
		bot.Send(msg)
		return
	}

	// Assuming here we would load chat history if needed
	// For simplicity, we'll just send a message that the chat is continued
	message := "Continuing chat with ID " + strconv.FormatInt(chatID, 10)
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, message)
	msg.ReplyMarkup = backToMenuKeyboard()
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
		return "An error occurred: " + responseBody.Error.Message
	}

	if len(responseBody.Choices) > 0 {
		return formatCodeMarkdown(responseBody.Choices[0].Message.Content)
	}

	return "I couldn't process your request."
}

func formatCodeMarkdown(text string) string {
	var sb strings.Builder
	inCodeBlock := false
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			if inCodeBlock {
				sb.WriteString("\n```")
				inCodeBlock = false
			} else {
				sb.WriteString("```\n")
				inCodeBlock = true
			}
		} else {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}
	return sb.String()
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

func backToMenuKeyboard() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🔙 Back to Menu"),
		),
	)
}
