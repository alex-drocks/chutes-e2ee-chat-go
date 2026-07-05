package chutes

import (
	"bytes"
	"context"
	"crypto/mlkem"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Transport struct {
	apiBase string
	apiKey  string
	client  *http.Client

	discovery *DiscoveryManager
	mu        sync.RWMutex
}

type ChatResult struct {
	Stream     bool
	Body       interface{}
	ModelUsed  string
	StreamBody io.ReadCloser
	ResponseKey *mlkem.DecapsulationKey768
	cancel     context.CancelFunc
}

func NewTransport(apiKey string) *Transport {
	client := &http.Client{}
	t := &Transport{
		apiBase: strings.TrimRight(DefaultAPIBase, "/"),
		apiKey:  apiKey,
		client:  client,
	}
	t.discovery = NewDiscoveryManager(apiKey, DefaultAPIBase, DefaultModelsBase, client)
	return t
}

func (t *Transport) SetAPIKey(apiKey string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.apiKey = apiKey
	t.discovery.SetAPIKey(apiKey)
}

func (t *Transport) GetModelMetadata(ctx context.Context) ([]ModelMetadata, error) {
	return t.discovery.GetModelMetadata(ctx)
}

func (t *Transport) Chat(ctx context.Context, params map[string]interface{}) (*ChatResult, error) {
	model, _ := params["model"].(string)
	if model == "" {
		return nil, fmt.Errorf("missing \"model\" in chat params")
	}

	t.mu.RLock()
	apiKey := t.apiKey
	apiBase := t.apiBase
	t.mu.RUnlock()

	chuteID, err := t.discovery.ResolveE2EEChuteID(ctx, model)
	if err != nil {
		return nil, err
	}
	instance, err := t.discovery.GetNonce(ctx, chuteID)
	if err != nil {
		return nil, err
	}
	if err := validateE2EEInstanceMaterial(instance); err != nil {
		return nil, err
	}

	blob, responseKey, err := BuildE2EERequest(instance.E2EPubKey, params)
	if err != nil {
		return nil, err
	}

	stream, _ := params["stream"].(bool)
	invokeCtx := ctx
	cancel := func() {}
	if !stream {
		invokeCtx, cancel = context.WithTimeout(ctx, InvokeTimeout)
	}

	req, err := http.NewRequestWithContext(invokeCtx, http.MethodPost, apiBase+"/e2e/invoke", bytes.NewReader(blob))
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("authorization", "Bearer "+apiKey)
	req.Header.Set("x-chute-id", chuteID)
	req.Header.Set("x-instance-id", instance.InstanceID)
	req.Header.Set("x-e2e-nonce", instance.Nonce)
	req.Header.Set("x-e2e-stream", strings.ToLower(fmt.Sprint(stream)))
	req.Header.Set("x-e2e-path", "/v1/chat/completions")
	req.Header.Set("content-type", "application/octet-stream")

	res, err := t.client.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		cancel()
		defer res.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, &HTTPError{
			Status:  res.StatusCode,
			Message: fmt.Sprintf("Chutes E2EE invoke failed: %d %s - %s", res.StatusCode, res.Status, string(body)),
		}
	}

	if stream {
		return &ChatResult{
			Stream:      true,
			ModelUsed:   model,
			StreamBody:  res.Body,
			ResponseKey: responseKey,
			cancel:      cancel,
		}, nil
	}

	defer cancel()
	defer res.Body.Close()
	responseBlob, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	body, err := DecryptResponse(responseBlob, responseKey)
	if err != nil {
		return nil, err
	}
	return &ChatResult{
		Stream:    false,
		Body:      body,
		ModelUsed: model,
	}, nil
}

func (r *ChatResult) Close() {
	if r == nil {
		return
	}
	if r.StreamBody != nil {
		_ = r.StreamBody.Close()
	}
	if r.cancel != nil {
		r.cancel()
	}
}

func validateE2EEInstanceMaterial(instance E2EEInstance) error {
	if normalizeHeaderValue(instance.InstanceID, 256) == "" {
		return fmt.Errorf("E2EE instance discovery returned an invalid instance id")
	}
	if normalizeHeaderValue(instance.Nonce, 4096) == "" {
		return fmt.Errorf("E2EE instance discovery returned an invalid nonce")
	}
	if normalizeE2EPubKey(instance.E2EPubKey) == "" {
		return fmt.Errorf("E2EE instance public key must decode to %d bytes", MLKEMPublicKeySize)
	}
	return nil
}

func MarshalChatParams(params map[string]interface{}) ([]byte, error) {
	return json.Marshal(params)
}

func init() {
	_ = time.Second
}
