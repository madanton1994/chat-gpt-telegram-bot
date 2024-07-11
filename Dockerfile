FROM golang:1.21.6-alpine

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN apk add --no-cache bash && go mod download

COPY . .

COPY wait-for-it.sh /wait-for-it.sh

RUN go build -o telegram-chatgpt-bot

EXPOSE 8080

CMD ["/wait-for-it.sh", "db:5432", "--", "/app/telegram-chatgpt-bot"]