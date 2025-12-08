package routers

import (
	resthandlers "github.com/Bombaclath97/mensa-telegram-bot/handlers/rest"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NotifyAllUsersRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.AllowContentType("application/json"))
	r.Post("/sendMessage", resthandlers.NotifyAllUsersHandler)
	return r
}
