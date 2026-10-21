// Package backend calls the rental app's read-only report endpoints (/irrl/chat-reports).
// The chatbot has no database access: every figure comes from the rental app, so its
// business rules apply.
package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const reportsPath = "/irrl/chat-reports/"

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Report fetches one report and returns its "data" payload as raw JSON.
func (c *Client) Report(ctx context.Context, name string, params url.Values) (json.RawMessage, error) {
	endpoint := c.baseURL + reportsPath + name
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Chatbot-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rental app unreachable: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	var envelope struct {
		Msg  interface{}     `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("rental app returned %d with an unreadable body", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rental app returned %d: %v", resp.StatusCode, envelope.Msg)
	}
	return envelope.Data, nil
}

// Ping checks that the rental app answers with this client's key.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Report(ctx, "order-status-summary", nil)
	return err
}
