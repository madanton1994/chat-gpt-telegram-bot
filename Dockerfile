# Stage 1: Build the Go application
FROM golang:1.21.6-alpine AS build

# Create and set the working directory
WORKDIR /app

# Install necessary packages
RUN apk add --no-cache bash coreutils

# Copy go.mod and go.sum files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire application code
COPY . .

# Build the application
RUN go build -o /app/telegram-chatgpt-bot

# Stage 2: Create a minimal image with the built binary
FROM alpine:3.18.2

# Create and set the working directory
WORKDIR /app

# Copy the built binary from the build stage
COPY --from=build /app/telegram-chatgpt-bot /app/telegram-chatgpt-bot

# Copy the models.yml file
COPY --from=build /app/config/models.yml /app/config/models.yml

# Ensure the binary is executable
RUN chmod +x /app/telegram-chatgpt-bot

# Expose the port the application will run on
EXPOSE 8080

# Run the binary
CMD ["/app/telegram-chatgpt-bot"]
