package chutes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

type HTTPError struct {
	Status     int
	RetryAfter int
	Message    string
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

func fetchWithRetry(ctx context.Context, client *http.Client, makeRequest func(context.Context) (*http.Request, error), maxRetries int) (*http.Response, error) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := makeRequest(ctx)
		if err != nil {
			return nil, err
		}

		res, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt >= maxRetries {
				return nil, fmt.Errorf("network error fetching %s: %w", req.URL.String(), err)
			}
			wait(ctx, retryDelay(attempt))
			continue
		}

		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return res, nil
		}

		retriable := res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500
		if retriable && attempt < maxRetries {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			wait(ctx, retryDelay(attempt))
			continue
		}

		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		res.Body.Close()
		retryAfter, _ := strconv.Atoi(res.Header.Get("retry-after"))
		return nil, &HTTPError{
			Status:     res.StatusCode,
			RetryAfter: retryAfter,
			Message:    fmt.Sprintf("HTTP %d %s: %s", res.StatusCode, res.Status, string(body)),
		}
	}

	return nil, fmt.Errorf("max retries reached")
}

func decodeJSONResponse(res *http.Response, target interface{}) error {
	defer res.Body.Close()
	dec := json.NewDecoder(res.Body)
	dec.UseNumber()
	return dec.Decode(target)
}

func retryDelay(attempt int) time.Duration {
	base := DefaultBaseBackoff * time.Duration(1<<attempt)
	jitter := time.Duration(rand.Int63n(int64(time.Second)))
	return base + jitter
}

func wait(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
