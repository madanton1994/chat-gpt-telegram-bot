FROM golang:1.21.6-alpine

WORKDIR /app

# Копируем файлы go.mod и go.sum для загрузки зависимостей
COPY go.mod ./
COPY go.sum ./
RUN go mod download

# Копируем остальные исходные файлы
COPY . .

# Собираем бинарник
RUN go build -o /telegram-chatgpt-bot

# Открываем порт для прослушивания
EXPOSE 8080

# Запускаем бинарник
CMD ["/telegram-chatgpt-bot"]