// Package api exposes the voice chatbot over HTTP.
package api

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"aichatbot/internal/backend"
	"aichatbot/internal/chat"
	"aichatbot/internal/sarvam"
)

const maxUploadBytes = 10 << 20

type Handler struct {
	sarvam  *sarvam.Client
	bot     *chat.Bot
	reports *backend.Client
}

func NewHandler(sarvamClient *sarvam.Client, bot *chat.Bot, reports *backend.Client) *Handler {
	return &Handler{sarvam: sarvamClient, bot: bot, reports: reports}
}

// Routes registers the endpoints, wrapped in CORS for allowedOrigin.
func (h *Handler) Routes(allowedOrigin string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ai/irrl/transcribe", h.transcribe)
	mux.HandleFunc("GET /ai/health", h.health)
	return withCORS(mux, allowedOrigin)
}

type transcribeResponse struct {
	Transcript        string `json:"transcript"`
	LanguageCode      string `json:"language_code"`
	ReplyLanguageCode string `json:"reply_language_code"`
	Answer            string `json:"answer"`
	AudioBase64       string `json:"audio_base64"`
	AudioMime         string `json:"audio_mime"`
	Error             string `json:"error,omitempty"`
}

// transcribe accepts a multipart "audio" file, transcribes it via Sarvam, answers the
// spoken question from the rental app's reports and returns the answer as text and as speech
// in the language that was spoken.
func (h *Handler) transcribe(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)

	file, header, err := r.FormFile("audio")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Audio file required")
		return
	}
	defer file.Close()

	audio, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Unable to read audio")
		return
	}

	result, err := h.sarvam.Transcribe(audio, header.Filename)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	// Reply in the spoken language when Sarvam can voice it, otherwise in English.
	replyCode := result.LanguageCode
	replyLanguage, ok := sarvam.SpeechLanguages[replyCode]
	if !ok {
		replyCode, replyLanguage = "en-IN", sarvam.SpeechLanguages["en-IN"]
	}

	response := transcribeResponse{
		Transcript:        result.Transcript,
		LanguageCode:      result.LanguageCode,
		ReplyLanguageCode: replyCode,
		AudioMime:         "audio/wav",
	}

	log.Panicln("thissss is the response---", response)
	answer, err := h.bot.Answer(r.Context(), result.Transcript, replyLanguage)
	if err != nil {
		log.Printf("chat failed: %v", err)
		response.Error = err.Error()
		writeJSON(w, http.StatusOK, response)
		return
	}
	response.Answer = answer

	audioBase64, err := h.sarvam.TextToSpeech(answer, replyCode)
	if err != nil {
		log.Printf("text-to-speech failed: %v", err)
		response.Error = err.Error()
		writeJSON(w, http.StatusOK, response)
		return
	}
	response.AudioBase64 = audioBase64
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := h.reports.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func withCORS(next http.Handler, allowedOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if allowedOrigin != "*" {
			w.Header().Add("Vary", "Origin")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
