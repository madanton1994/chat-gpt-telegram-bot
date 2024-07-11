FROM golang:1.21.6-alpine

RUN mkdir -p /app
WORKDIR /app

RUN apk add --no-cache bash coreutils

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY . .

RUN go build -o /app/telegram-chatgpt-bot

RUN chmod +x /app/telegram-chatgpt-bot

RUN ls -la /app

EXPOSE 8080

CMD ["/app/telegram-chatgpt-bot"]