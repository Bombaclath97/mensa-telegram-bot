# Pure stdlib, no cgo — unlike the bot (which needs cgo for sqlite), this can be a static
# binary on scratch.
FROM golang:1.25-bookworm AS build

WORKDIR /src
COPY go.mod ./
COPY main.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mock-mensa-api .

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/mock-mensa-api /app/mock-mensa-api

EXPOSE 8081
USER nonroot:nonroot
ENTRYPOINT ["/app/mock-mensa-api"]
