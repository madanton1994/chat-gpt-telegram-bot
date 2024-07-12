package main

import (
	"database/sql"
	"fmt"
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
var activeChats map[int64]int64 // stores the active chat ID for each user

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

	activeChats = make(map[int64]int64) // initialize the activeChats map

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
			sendSettingsMenu(bot, update.Message.Chat.ID)
		case "💬 Chats":
			sendChatList(bot, update.Message.Chat.ID)
		case "🆕 Create Chat":
			askForChatName(bot, update.Message.Chat.ID)
		case "❌ Delete Chat":
			sendDeleteChatMenu(bot, update.Message.Chat.ID)
		case "🔙 Back":
			sendWelcomeMessage(bot, update.Message.Chat.ID)
		default:
			if strings.HasPrefix(text, "Chat ID: ") {
				chatIDStr := strings.TrimPrefix(text, "Chat ID: ")
				chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
				if err == nil {
					activeChats[update.Message.Chat.ID] = chatID
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Switched to chat "+chatIDStr)
					bot.Send(msg)
				}
			} else if strings.HasPrefix(text, "Model: ") {
				model := strings.TrimPrefix(text, "Model: ")
				err := setChatModel(update.Message.Chat.ID, model)
				if err != nil {
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Failed to set model: "+err.Error())
					bot.Send(msg)
				} else {
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Model set to "+model)
					bot.Send(msg)
				}
			} else if strings.HasPrefix(text, "Delete Chat ID: ") {
				chatIDStr := strings.TrimPrefix(text, "Delete Chat ID: ")
				chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
				if err == nil {
					err := deleteChat(chatID)
					if err != nil {
						msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Failed to delete chat: "+err.Error())
						bot.Send(msg)
					} else {
						msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Chat "+chatIDStr+" deleted successfully.")
						bot.Send(msg)
					}
				}
			} else if update.Message.ReplyToMessage != nil && update.Message.ReplyToMessage.Text == "Please provide a name for the new chat:" {
				createNewChat(bot, update.Message.Chat.ID, text)
			} else {
				response := getChatGPTResponse(update.Message.Chat.ID, text)
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, response)
				msg.ParseMode = "HTML"
				bot.Send(msg)
				saveChatHistory(update.Message.Chat.ID, text)
			}
		}
	} else if update.Message.ReplyToMessage != nil {
		handleReply(bot, update.Message)
	}
}

func sendWelcomeMessage(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "👋 Welcome! I am your ChatGPT bot. You can use the following commands:")
	msg.ReplyMarkup = mainMenuKeyboard()
	bot.Send(msg)
}

func sendHelpMessage(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "ℹ️ Here is a list of commands you can use:")
	bot.Send(msg)
}

func sendStatusMessage(bot *tgbotapi.BotAPI, chatID int64) {
	activeChatID, ok := activeChats[chatID]
	if !ok {
		activeChatID = chatID
	}

	var model string
	err := db.QueryRow("SELECT model FROM chat_models WHERE chat_id = $1", activeChatID).Scan(&model)
	if err != nil {
		if err == sql.ErrNoRows {
			model = "gpt-3.5-turbo"
		} else {
			log.Printf("Error querying model: %v", err)
			model = "Unknown"
		}
	}

	msg := tgbotapi.NewMessage(chatID, "📊 All systems are operational.\nCurrent model: "+model)
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

func sendChatList(bot *tgbotapi.BotAPI, chatID int64) {
	log.Println("Fetching chat list...")

	rows, err := db.Query("SELECT cn.chat_id, cn.chat_name FROM chat_names cn")
	if err != nil {
		log.Printf("Error fetching chat list: %v", err)
		msg := tgbotapi.NewMessage(chatID, "❌ Error fetching chat list.")
		bot.Send(msg)
		return
	}
	defer rows.Close()

	var chatButtons []tgbotapi.KeyboardButton
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			log.Printf("Error scanning chat ID and name: %v", err)
			continue
		}
		chatButtons = append(chatButtons, tgbotapi.NewKeyboardButton(fmt.Sprintf("Chat ID: %d (%s)", id, name)))
	}

	if len(chatButtons) == 0 {
		msg := tgbotapi.NewMessage(chatID, "📭 No chats found.")
		bot.Send(msg)
		return
	}

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(chatButtons...),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🆕 Create Chat"),
			tgbotapi.NewKeyboardButton("❌ Delete Chat"),
		),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("🔙 Back")),
	)

	msg := tgbotapi.NewMessage(chatID, "💬 Active chats:")
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func sendDeleteChatMenu(bot *tgbotapi.BotAPI, chatID int64) {
	log.Println("Fetching chat list for deletion...")

	rows, err := db.Query("SELECT cn.chat_id, cn.chat_name FROM chat_names cn")
	if err != nil {
		log.Printf("Error fetching chat list: %v", err)
		msg := tgbotapi.NewMessage(chatID, "❌ Error fetching chat list.")
		bot.Send(msg)
		return
	}
	defer rows.Close()

	var chatButtons []tgbotapi.KeyboardButton
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			log.Printf("Error scanning chat ID and name: %v", err)
			continue
		}
		chatButtons = append(chatButtons, tgbotapi.NewKeyboardButton(fmt.Sprintf("Delete Chat ID: %d (%s)", id, name)))
	}

	if len(chatButtons) == 0 {
		msg := tgbotapi.NewMessage(chatID, "📭 No chats found.")
		bot.Send(msg)
		return
	}

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(chatButtons...),
		tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("🔙 Back")),
	)

	msg := tgbotapi.NewMessage(chatID, "❌ Select a chat to delete:")
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func askForChatName(bot *tgbotapi.BotAPI, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "Please provide a name for the new chat:")
	bot.Send(msg)
}

func createNewChat(bot *tgbotapi.BotAPI, chatID int64, chatName string) {
	_, err := db.Exec("INSERT INTO chat_names (chat_id, chat_name) VALUES ($1, $2)", chatID, chatName)
	if err != nil {
		log.Printf("Error creating new chat: %v", err)
		msg := tgbotapi.NewMessage(chatID, "Failed to create new chat.")
		bot.Send(msg)
		return
	}
	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("New chat created with ID: %d and name: %s", chatID, chatName))
	bot.Send(msg)
}

func deleteChat(chatID int64) error {
	_, err := db.Exec("DELETE FROM chat_history WHERE chat_id = $1", chatID)
	if err != nil {
		return err
	}
	_, err = db.Exec("DELETE FROM chat_names WHERE chat_id = $1", chatID)
	return err
}

func getChatGPTResponse(chatID int64, message string) string {
	client := resty.New()

	activeChatID, ok := activeChats[chatID]
	if !ok {
		activeChatID = chatID
	}

	var model string
	err := db.QueryRow("SELECT model FROM chat_models WHERE chat_id = $1", activeChatID).Scan(&model)
	if err != nil {
		if err == sql.ErrNoRows {
			model = "gpt-3.5-turbo" // Default model
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

func setChatModel(chatID int64, model string) error {
	_, err := db.Exec("INSERT INTO chat_models (chat_id, model) VALUES ($1, $2) ON CONFLICT (chat_id) DO UPDATE SET model = $2", chatID, model)
	return err
}

func saveChatHistory(chatID int64, message string) {
	_, err := db.Exec("INSERT INTO chat_history (chat_id, message) VALUES ($1, $2)", chatID, message)
	if err != nil {
		log.Printf("Error saving chat history: %v", err)
	}
}

func handleReply(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	originalMessage := message.ReplyToMessage.Text
	response := getChatGPTResponse(message.Chat.ID, originalMessage+" "+message.Text)
	msg := tgbotapi.NewMessage(message.Chat.ID, response)
	msg.ParseMode = "HTML"
	bot.Send(msg)
	saveChatHistory(message.Chat.ID, originalMessage+" "+message.Text)
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
