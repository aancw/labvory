// Package notify delivers push notifications to a user-configured webhook.
// The payload is ntfy-compatible: POST the URL with the message as the
// plain-text body and the title in the X-Title header — ntfy turns that
// pairing into a proper notification, and any other receiver gets a simple
// text POST it can parse or route.
package notify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var client = &http.Client{Timeout: 10 * time.Second}

// Post sends one notification. The webhook URL is pre-validated by the
// settings layer; failures here are the caller's logging concern.
func Post(ctx context.Context, url, title, message string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(message))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req.Header.Set("X-Title", title)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %s", resp.Status)
	}
	return nil
}
