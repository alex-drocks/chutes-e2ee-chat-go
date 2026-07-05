package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/alex-drocks/chutes-e2ee-chat-go/internal/chutes"
	"github.com/alex-drocks/chutes-e2ee-chat-go/internal/credentials"
)

type App struct {
	ctx context.Context

	store     *credentials.Store
	transport *chutes.Transport

	mu                sync.Mutex
	activeControllers map[string]context.CancelFunc
}

func NewApp() *App {
	store, err := credentials.NewStore("Chutes E2EE Chat")
	if err != nil {
		panic(err)
	}

	return &App{
		store:             store,
		transport:         chutes.NewTransport(""),
		activeControllers: make(map[string]context.CancelFunc),
	}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	if creds, err := a.store.Load(); err == nil {
		a.transport.SetAPIKey(creds.ChutesAPIKey)
	}
}

type chatPayload struct {
	RequestID string                 `json:"requestId"`
	Params    map[string]interface{} `json:"params"`
}

func (a *App) Chat(payload chatPayload) map[string]interface{} {
	requestID, err := chutes.NormalizeRequestID(payload.RequestID)
	if err != nil {
		return fail(err)
	}
	if payload.Params == nil {
		return fail(errors.New("invalid chat params"))
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.setActiveController(requestID, cancel)

	result, err := a.transport.Chat(ctx, payload.Params)
	if err != nil {
		a.clearActiveController(requestID)
		cancel()
		return fail(err)
	}

	if result.Stream {
		go a.pumpStream(ctx, requestID, result)
		return map[string]interface{}{
			"ok":        true,
			"stream":    true,
			"modelUsed": result.ModelUsed,
		}
	}

	a.clearActiveController(requestID)
	cancel()
	return map[string]interface{}{
		"ok":        true,
		"stream":    false,
		"body":      result.Body,
		"modelUsed": result.ModelUsed,
	}
}

func (a *App) Abort(payload map[string]interface{}) map[string]interface{} {
	requestID, err := chutes.NormalizeRequestID(stringValue(payload, "requestId"))
	if err != nil {
		return fail(err)
	}

	a.mu.Lock()
	cancel := a.activeControllers[requestID]
	delete(a.activeControllers, requestID)
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	return map[string]interface{}{"ok": true}
}

func (a *App) Models() map[string]interface{} {
	metadata, err := a.transport.GetModelMetadata(context.Background())
	if err != nil {
		return fail(err)
	}
	models := make([]string, 0, len(metadata))
	for _, entry := range metadata {
		models = append(models, entry.ID)
	}
	return map[string]interface{}{
		"ok":       true,
		"models":   models,
		"metadata": metadata,
	}
}

func (a *App) ModelStats() map[string]interface{} {
	stats, err := chutes.FetchModelStats(context.Background())
	if err != nil {
		return fail(err)
	}
	return map[string]interface{}{"ok": true, "stats": stats}
}

func (a *App) WebSearch(payload map[string]interface{}) map[string]interface{} {
	query := stringValue(payload, "query")
	deepSearch, _ := payload["deepSearch"].(bool)
	result, err := chutes.WebSearch(context.Background(), query, deepSearch)
	if err != nil {
		return map[string]interface{}{
			"ok":                         false,
			"error":                      err.Error(),
			"results":                    []interface{}{},
			"provider":                   "DuckDuckGo",
			"deepSearch":                 deepSearch,
			"extractedCount":             0,
			"extractionAttemptedCount":   0,
			"totalResults":               0,
			"errors":                     0,
		}
	}
	return result
}

func (a *App) ClipboardImage() map[string]interface{} {
	return map[string]interface{}{
		"ok":       true,
		"hasImage": false,
	}
}

func (a *App) SaveApiKey(payload map[string]interface{}) map[string]interface{} {
	if provider := stringValue(payload, "provider"); provider != "chutes" {
		return fail(errors.New("unsupported provider. Use \"chutes\""))
	}
	apiKey, err := credentials.NormalizeAPIKey(stringValue(payload, "apiKey"))
	if err != nil {
		return fail(err)
	}
	if err := a.store.Save(credentials.Credentials{ChutesAPIKey: apiKey}); err != nil {
		return fail(err)
	}
	a.transport.SetAPIKey(apiKey)
	return a.apiKeyStatus(true)
}

func (a *App) GetApiKeyStatus(payload map[string]interface{}) map[string]interface{} {
	if provider := stringValue(payload, "provider"); provider != "chutes" {
		return fail(errors.New("unsupported provider. Use \"chutes\""))
	}
	return a.apiKeyStatus(true)
}

func (a *App) DeleteApiKey(payload map[string]interface{}) map[string]interface{} {
	if provider := stringValue(payload, "provider"); provider != "chutes" {
		return fail(errors.New("unsupported provider. Use \"chutes\""))
	}
	if err := a.store.Delete(); err != nil {
		return fail(err)
	}
	a.transport.SetAPIKey("")
	return a.apiKeyStatus(true)
}

func (a *App) apiKeyStatus(ok bool) map[string]interface{} {
	status := a.store.Status()
	status["ok"] = ok
	return status
}

func (a *App) pumpStream(ctx context.Context, requestID string, result *chutes.ChatResult) {
	defer func() {
		result.Close()
		a.clearActiveController(requestID)
	}()

	processor := chutes.NewStreamProcessor(result.ResponseKey)
	chunks := 0

	err := chutes.ReadSSE(ctx, result.StreamBody, func(line string) error {
		events, done, err := processor.Process(line)
		if err != nil {
			return err
		}
		for _, data := range events {
			if data == "" {
				continue
			}
			chunks++
			runtime.EventsEmit(a.ctx, "chutes:chunk", map[string]interface{}{
				"requestId": requestID,
				"data":      data,
				"done":      false,
			})
		}
		if done {
			runtime.EventsEmit(a.ctx, "chutes:chunk", map[string]interface{}{
				"requestId": requestID,
				"done":      true,
			})
		}
		return nil
	})

	if err != nil && !errors.Is(err, context.Canceled) {
		runtime.EventsEmit(a.ctx, "chutes:error", map[string]interface{}{
			"requestId": requestID,
			"error":     err.Error(),
			"done":      true,
		})
		return
	}

	if chunks == 0 {
		runtime.EventsEmit(a.ctx, "chutes:error", map[string]interface{}{
			"requestId": requestID,
			"error":     "The model returned an empty response. It may be warming up or at capacity.",
			"done":      true,
		})
		return
	}

	runtime.EventsEmit(a.ctx, "chutes:chunk", map[string]interface{}{
		"requestId": requestID,
		"done":      true,
	})
}

func (a *App) setActiveController(requestID string, cancel context.CancelFunc) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.activeControllers[requestID] = cancel
}

func (a *App) clearActiveController(requestID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.activeControllers, requestID)
}

func fail(err error) map[string]interface{} {
	return map[string]interface{}{
		"ok":    false,
		"error": err.Error(),
	}
}

func stringValue(payload map[string]interface{}, key string) string {
	if payload == nil {
		return ""
	}
	switch value := payload[key].(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	case json.Number:
		return value.String()
	default:
		return ""
	}
}
