package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/go-resty/resty/v2"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/pkoukk/tiktoken-go"
	"gopkg.in/yaml.v2"
)

var (
	openaiAPIKey string
	serverURL    string
	webhookURL   string
	useWebhook   string
	modelMap     = make(map[int64]string) // Stores the model for each chat ID
	modeMap      = make(map[int64]string) // Stores the mode for each chat ID
	models       []string                 // List of available models from the YAML file
	modelInfo    map[string]ModelInfo     // Detailed model info from the YAML file
	chatModes    map[string]ChatMode      // Detailed chat modes from the YAML file
)

type ModelConfig struct {
	AvailableTextModels []string             `yaml:"available_text_models"`
	Info                map[string]ModelInfo `yaml:"info"`
}

type ModelInfo struct {
	Type                     string         `yaml:"type"`
	Name                     string         `yaml:"name"`
	Description              string         `yaml:"description"`
	PricePer1000InputTokens  float64        `yaml:"price_per_1000_input_tokens"`
	PricePer1000OutputTokens float64        `yaml:"price_per_1000_output_tokens"`
	Scores                   map[string]int `yaml:"scores"`
}

type ChatMode struct {
	Name           string `yaml:"name"`
	WelcomeMessage string `yaml:"welcome_message"`
	PromptStart    string `yaml:"prompt_start"`
	ParseMode      string `yaml:"parse_mode"`
}

func main() {
	telegramToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	openaiAPIKey = os.Getenv("OPENAI_API_KEY")
	serverURL = os.Getenv("SERVER_URL")
	webhookURL = os.Getenv("WEBHOOK_URL")
	useWebhook = os.Getenv("USE_WEBHOOK")

	validateEnvVars(telegramToken, openaiAPIKey, serverURL)

	// Load models and chat modes configuration
	err := loadModelConfig("/app/config/models.yml")
	if err != nil {
		log.Fatalf("Failed to load model configuration: %v", err)
	}
	err = loadChatModesConfig("/app/config/chat_modes.yml")
	if err != nil {
		log.Fatalf("Failed to load chat modes configuration: %v", err)
	}

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

func loadModelConfig(filename string) error {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	var config ModelConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return err
	}

	// Filter out deprecated models
	for _, model := range config.AvailableTextModels {
		if model != "text-davinci-003" {
			models = append(models, model)
		}
	}
	modelInfo = config.Info

	return nil
}

func loadChatModesConfig(filename string) error {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(data, &chatModes)
	if err != nil {
		return err
	}

	return nil
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
		} else if update.CallbackQuery != nil {
			handleCallbackQuery(bot, update.CallbackQuery)
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
	case text == "/mode":
		showChatModesHandle(bot, update.Message.Chat.ID)
	case strings.HasPrefix(text, "Model: "):
		setModel(bot, update.Message.Chat.ID, strings.TrimPrefix(text, "Model: "))
	default:
		response, parseMode := getChatGPTResponse(update.Message.Chat.ID, text)
		sendMessage(bot, update.Message.Chat.ID, response, parseMode)
	}
}

func handleCallbackQuery(bot *tgbotapi.BotAPI, callbackQuery *tgbotapi.CallbackQuery) {
	data := callbackQuery.Data
	chatID := callbackQuery.Message.Chat.ID
	switch {
	case strings.HasPrefix(data, "set_chat_mode|"):
		chatMode := strings.TrimPrefix(data, "set_chat_mode|")
		setChatMode(bot, chatID, chatMode)
		sendWelcomeMessage(bot, chatID) // Return to main menu after selecting chat mode
	}
}

func setModel(bot *tgbotapi.BotAPI, chatID int64, model string) {
	if !isValidModel(model) {
		sendMessage(bot, chatID, "❌ Invalid model selected.", "HTML")
		return
	}
	modelMap[chatID] = model
	sendMessage(bot, chatID, "Model set to "+model, "HTML")
}

func isValidModel(model string) bool {
	for _, m := range models {
		if m == model {
			return true
		}
	}
	return false
}

func sendWelcomeMessage(bot *tgbotapi.BotAPI, chatID int64) {
	currentMode := getCurrentChatMode(chatID)
	msg := fmt.Sprintf("👋 Welcome! I am your ChatGPT bot. You can use the following commands:\n\nCurrent mode: %s", currentMode.Name)
	sendMessageWithKeyboard(bot, chatID, msg, mainMenuKeyboard(chatID), "HTML")
}

func sendHelpMessage(bot *tgbotapi.BotAPI, chatID int64) {
	msg := "ℹ️ Here is a list of commands you can use:\n/start - Start the bot\n/help - Show this help message\n/status - Show bot status\n/settings - Show settings\n/mode - Select chat mode"
	sendMessageWithKeyboard(bot, chatID, msg, mainMenuKeyboard(chatID), "HTML")
}

func sendStatusMessage(bot *tgbotapi.BotAPI, chatID int64) {
	model := getCurrentModel(chatID)
	msg := "📊 All systems are operational.\nCurrent model: " + model
	sendMessageWithKeyboard(bot, chatID, msg, mainMenuKeyboard(chatID), "HTML")
}

func sendSettingsMenu(bot *tgbotapi.BotAPI, chatID int64) {
	var keyboardRows [][]tgbotapi.KeyboardButton

	for _, model := range models {
		keyboardRows = append(keyboardRows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(fmt.Sprintf("Model: %s", model)),
		))
	}
	keyboardRows = append(keyboardRows, tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("🔙 Back")))

	keyboard := tgbotapi.NewReplyKeyboard(keyboardRows...)
	sendMessageWithKeyboard(bot, chatID, "⚙️ Select a model:", keyboard, "HTML")
}

func showChatModesHandle(bot *tgbotapi.BotAPI, chatID int64) {
	var keyboardRows [][]tgbotapi.KeyboardButton

	for _, info := range chatModes {
		keyboardRows = append(keyboardRows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(fmt.Sprintf("%s", info.Name)),
		))
	}
	keyboardRows = append(keyboardRows, tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("🔙 Back")))

	keyboard := tgbotapi.NewReplyKeyboard(keyboardRows...)
	sendMessageWithKeyboard(bot, chatID, "⚙️ Select a chat mode:", keyboard, "HTML")
}

func setChatMode(bot *tgbotapi.BotAPI, chatID int64, mode string) {
	chatMode, exists := chatModes[mode]
	if !exists {
		sendMessage(bot, chatID, "❌ Invalid chat mode selected.", "HTML")
		return
	}
	modeMap[chatID] = mode
	sendMessage(bot, chatID, chatMode.WelcomeMessage, chatMode.ParseMode)
}

func getChatGPTResponse(chatID int64, message string) (string, string) {
	client := resty.New()
	model := getCurrentModel(chatID)
	mode := getCurrentChatMode(chatID)

	messages := []map[string]string{
		{"role": "system", "content": mode.PromptStart},
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
		return "An error occurred while processing your request.", mode.ParseMode
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
		return "An error occurred while processing your request.", mode.ParseMode
	}

	log.Printf("OpenAI API response status: %d", resp.StatusCode())
	log.Printf("OpenAI API response body: %s", resp.String())

	if resp.StatusCode() == 429 {
		return "❌ You exceeded your current quota. Please check your plan and billing details.", mode.ParseMode
	}

	if responseBody.Error.Message != "" {
		log.Printf("OpenAI API error: %s", responseBody.Error.Message)
		return "❌ An error occurred while processing your request.", mode.ParseMode
	}

	if len(responseBody.Choices) > 0 {
		answer := responseBody.Choices[0].Message.Content

		// Count tokens for the answer
		_, nOutputTokens, err = countTokensFromMessages(nil, answer, model)
		if err != nil {
			log.Printf("Error counting output tokens: %v", err)
			return "An error occurred while processing your request.", mode.ParseMode
		}
		log.Printf("Output tokens: %d", nOutputTokens)

		return formatAsTelegramCode(answer), mode.ParseMode
	}

	return "❌ I couldn't process your request.", mode.ParseMode
}

func getCurrentModel(chatID int64) string {
	model, exists := modelMap[chatID]
	if !exists {
		return "gpt-3.5-turbo" // Default model
	}
	return model
}

func getCurrentChatMode(chatID int64) ChatMode {
	mode, exists := modeMap[chatID]
	if !exists {
		return chatModes["assistant"] // Default chat mode
	}
	return chatModes[mode]
}

func sendMessage(bot *tgbotapi.BotAPI, chatID int64, text string, parseMode string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = parseMode
	bot.Send(msg)
}

func sendMessageWithKeyboard(bot *tgbotapi.BotAPI, chatID int64, text string, keyboard tgbotapi.ReplyKeyboardMarkup, parseMode string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = parseMode
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func mainMenuKeyboard(chatID int64) tgbotapi.ReplyKeyboardMarkup {
	currentMode := getCurrentChatMode(chatID).Name
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
			tgbotapi.NewKeyboardButton(fmt.Sprintf("/mode (%s)", currentMode)),
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
	case "text-davinci-003":
		tokensPerMessage = 4
		tokensPerName = -1
	default:
		return 0, 0, fmt.Errorf("unknown model: %s", model)
	}

	nInputTokens := 0
	if messages != nil {
		for _, message := range messages {
			nInputTokens += tokensPerMessage
			if content, ok := message["content"]; ok {
				tokens := encoding.Encode(content, []string{}, []string{})
				nInputTokens += len(tokens)
			}
			if name, ok := message["name"]; ok {
				tokens := encoding.Encode(name, []string{}, []string{})
				nInputTokens += tokensPerName
				nInputTokens += len(tokens)
			}
		}
	}

	nInputTokens += 2

	tokens := encoding.Encode(answer, []string{}, []string{})
	nOutputTokens := 1 + len(tokens)

	return nInputTokens, nOutputTokens, nil
}
