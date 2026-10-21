// Package sarvam calls Sarvam AI speech-to-text and text-to-speech.
package sarvam

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

const (
	sttURL = "https://api.sarvam.ai/speech-to-text"
	ttsURL = "https://api.sarvam.ai/text-to-speech"

	// ttsMaxChars is the bulbul:v3 per-request text limit.
	ttsMaxChars = 2500
)

// SpeechLanguages are the languages Sarvam text-to-speech can speak, by language code.
var SpeechLanguages = map[string]string{
	"en-IN": "English",
	"hi-IN": "Hindi",
	"ml-IN": "Malayalam",
	"ta-IN": "Tamil",
	"te-IN": "Telugu",
	"kn-IN": "Kannada",
	"bn-IN": "Bengali",
	"gu-IN": "Gujarati",
	"mr-IN": "Marathi",
	"od-IN": "Odia",
	"pa-IN": "Punjabi",
}

type Transcript struct {
	Transcript   string `json:"transcript"`
	LanguageCode string `json:"language_code"`
}

type Client struct {
	apiKey string
	http   *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{apiKey: apiKey, http: &http.Client{Timeout: 60 * time.Second}}
}

// Transcribe returns the transcript in the spoken language, with the detected language code.
func (c *Client) Transcribe(audio []byte, filename string) (*Transcript, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	filePart, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := filePart.Write(audio); err != nil {
		return nil, err
	}

	_ = writer.WriteField("model", "saaras:v4")
	_ = writer.WriteField("language_code", "unknown")

	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, sttURL, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	var result Transcript
	if err := c.do(req, &result); err != nil {
		return nil, fmt.Errorf("speech-to-text: %w", err)
	}
	return &result, nil
}

// TextToSpeech speaks text in languageCode and returns base64-encoded WAV audio.
func (c *Client) TextToSpeech(text, languageCode string) (string, error) {
	if runes := []rune(text); len(runes) > ttsMaxChars {
		text = string(runes[:ttsMaxChars])
	}

	payload, err := json.Marshal(map[string]interface{}{
		"text":          text,
		"language_code": languageCode,
		"model":         "bulbul:v3",
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, ttsURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	var result struct {
		Audios []string `json:"audios"`
	}
	if err := c.do(req, &result); err != nil {
		return "", fmt.Errorf("text-to-speech: %w", err)
	}
	if len(result.Audios) == 0 {
		return "", fmt.Errorf("text-to-speech: Sarvam returned no audio")
	}
	return result.Audios[0], nil
}

func (c *Client) do(req *http.Request, out interface{}) error {
	req.Header.Set("api-subscription-key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("Sarvam returned %d: %s", resp.StatusCode, string(message))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
