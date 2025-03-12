FROM golang:1.24.0-alpine3.21 AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o mensa-bot

# Start a new stage from scratch
FROM alpine:3.9.6  

COPY --from=build /app/mensa-bot /usr/local/bin/mensa-bot

EXPOSE 8080

CMD ["mensa-bot"]