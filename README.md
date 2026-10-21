# ai-chatbot

Voice assistant for the rental business. The owner speaks a question in Malayalam, Hindi,
English or another Indian language, and gets the answer back as text and as speech in the
same language, computed by the rental app.

```
audio ─► Sarvam speech-to-text (detects language)
      ─► Claude, using the rental app's report endpoints as tools
      ─► Sarvam text-to-speech in the spoken language
```

The chatbot has no database access. Every figure comes from the rental app
(`go-clean-code-eccomerce`) through its read-only `/irrl/chat-reports/*` endpoints, so the
app's own business rules (order totals, invoice amounts, statuses) always apply. When a rule
changes in the rental app, the chatbot picks it up without any change here.

## Run

```bash
cp .env.example .env      # fill in the rental app URL and the API keys
go build -o ai-chatbot ./cmd/server
./ai-chatbot
```

On a small server (1–2 GB RAM) the Anthropic SDK can run the compiler out of memory. Either
add swap, build with `go build -p 1 ...`, or build on another machine and copy the binary:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ai-chatbot ./cmd/server
```

The binary only needs `.env` next to it, or the same variables in the environment.

`BACKEND_API_KEY` must equal `CHATBOT_API_KEY` in the rental app's `pkg/config/.env`; the
rental app rejects report requests without it, and disables the reports when it is unset.

## API

`POST /irrl/transcribe` — multipart form with the recording in field `audio` (max 10 MB).

```json
{
  "transcript": "ഏറ്റവും കൂടുതൽ ഓർഡർ ചെയ്ത ഉപഭോക്താവ് ആരാണ്?",
  "language_code": "ml-IN",
  "reply_language_code": "ml-IN",
  "answer": "…",
  "audio_base64": "UklGRi…",
  "audio_mime": "audio/wav"
}
```

Play it in the browser with `new Audio(\`data:${r.audio_mime};base64,${r.audio_base64}\`).play()`.
If a later step fails, the fields that did complete are still returned, plus `error`.

`GET /health` — checks that the rental app's reports answer with this service's key.

## Adding a question type

1. Add a read-only report to the rental app in `pkg/irrl/chat_reports.go`, reusing its
   service logic, under `/irrl/chat-reports/<name>`.
2. Add a tool for it in `internal/chat/chat.go` with `newReportTool`, with a description
   that tells Claude when to use it.

## Layout

| Path | Purpose |
|---|---|
| `cmd/server` | entry point |
| `internal/api` | HTTP handler |
| `internal/sarvam` | speech-to-text and text-to-speech |
| `internal/chat` | Claude tool loop and system prompt |
| `internal/backend` | client for the rental app's report endpoints |
| `internal/config` | settings from environment / `.env` |
