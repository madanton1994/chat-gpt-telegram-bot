FROM golang:1.21.6-alpine

RUN mkdir -p /app
WORKDIR /app

RUN apk add --no-cache bash

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY . .

COPY wait-for-it.sh /wait-for-it.sh

RUN go build -o /app/telegram-chatgpt-bot

RUN ls -la /app
RUN file /app/telegram-chatgpt-bot

EXPOSE 8080

CMD ["sh", "/wait-for-it.sh", "db:5432", "--", "/app/telegram-chatgpt-bot"]