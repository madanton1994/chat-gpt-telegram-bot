# Telegram Bot with OpenAI and PostgreSQL Integration

## Overview

This project implements a Telegram bot that interacts with OpenAI's GPT models and stores chat-specific model preferences in a PostgreSQL database. The bot can automatically select the previously used model for each chat, and supports both long polling and webhook interaction methods.

## Architecture

The architecture of the bot includes the following components:

1. **Telegram Bot**: Interacts with users on Telegram and processes their messages.
2. **OpenAI API**: Provides responses from different GPT models based on user queries.
3. **PostgreSQL Database**: Stores the preferred GPT model for each chat.
4. **Golang Migrate**: Manages database migrations to ensure the correct schema is in place.

### Components

- **Bot Service**: Written in Go, this service handles incoming messages, interacts with the OpenAI API, and queries the PostgreSQL database.
- **PostgreSQL**: Used to persist the preferred model for each chat.
- **Golang Migrate**: Ensures that the database schema is up-to-date.

## Setup and Installation

### Prerequisites

- Docker
- Docker Compose
- Go 1.21.6

### Environment Variables

Create a `.env` file in the project root with the following content:

```dotenv
TELEGRAM_BOT_TOKEN=your_telegram_bot_token
OPENAI_API_KEY=your_openai_api_key
SERVER_URL=https://api.openai.com
WEBHOOK_URL=https://yourdomain.com:8080
USE_WEBHOOK=true
POSTGRES_USER=your_postgres_user
POSTGRES_PASSWORD=your_postgres_password
POSTGRES_DB=your_postgres_db
DATABASE_URL=postgresql://your_postgres_user:your_postgres_password@db:5432/your_postgres_db?sslmode=disable
