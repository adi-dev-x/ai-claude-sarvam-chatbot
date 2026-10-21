package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"aichatbot/internal/api"
	"aichatbot/internal/backend"
	"aichatbot/internal/chat"
	"aichatbot/internal/config"
	"aichatbot/internal/sarvam"
)

func main() {
	cfg, err := config.Load(".env")
	if err != nil {
		log.Fatal(err)
	}

	reports := backend.NewClient(cfg.BackendURL, cfg.BackendAPIKey)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := reports.Ping(ctx); err != nil {
		// Not fatal: the rental app may start after the chatbot. /health reports it.
		log.Printf("warning: rental app reports not reachable at %s: %v", cfg.BackendURL, err)
	} else {
		log.Printf("connected to rental app reports at %s", cfg.BackendURL)
	}
	cancel()

	bot, err := chat.New(cfg.AnthropicAPIKey, reports)
	if err != nil {
		log.Fatal(err)
	}
	handler := api.NewHandler(sarvam.NewClient(cfg.SarvamAPIKey), bot, reports)

	server := &http.Server{
		Addr:              cfg.Host + ":" + cfg.Port,
		Handler:           handler.Routes(cfg.AllowedOrigin),
		ReadHeaderTimeout: 10 * time.Second,
		// Speech-to-text, the Claude tool loop and text-to-speech can take a while on broad questions.
		WriteTimeout: 3 * time.Minute,
	}
	log.Printf("ai-chatbot listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}
