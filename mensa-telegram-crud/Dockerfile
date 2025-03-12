FROM golang:1.24.0-alpine3.21 AS build

RUN apk add --no-cache --update gcc g++ 

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o mensa-crud

# Start a new stage from scratch
FROM alpine:3.9.6  

WORKDIR /app

COPY --from=build /app/mensa-crud /usr/local/bin/mensa-crud

EXPOSE 8080

CMD ["mensa-crud"]