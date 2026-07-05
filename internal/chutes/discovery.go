package chutes

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

type DiscoveryManager struct {
	apiBase    string
	modelsBase string
	apiKey     string
	client     *http.Client

	mu               sync.Mutex
	modelMap         map[string]string
	modelMeta        map[string]ModelMetadata
	modelMapLoadedAt time.Time
	nonceCache       map[string]*nonceCacheEntry
}

type nonceCacheEntry struct {
	instances []nonceInstance
	expiresAt time.Time
}

type nonceInstance struct {
	instanceID string
	e2ePubKey  string
	nonces     []string
}

func NewDiscoveryManager(apiKey, apiBase, modelsBase string, client *http.Client) *DiscoveryManager {
	return &DiscoveryManager{
		apiKey:     apiKey,
		apiBase:    strings.TrimRight(apiBase, "/"),
		modelsBase: strings.TrimRight(modelsBase, "/"),
		client:     client,
		modelMap:   make(map[string]string),
		modelMeta:  make(map[string]ModelMetadata),
		nonceCache: make(map[string]*nonceCacheEntry),
	}
}

func (d *DiscoveryManager) SetAPIKey(apiKey string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.apiKey = apiKey
	d.modelMapLoadedAt = time.Time{}
	d.modelMap = make(map[string]string)
	d.modelMeta = make(map[string]ModelMetadata)
	d.nonceCache = make(map[string]*nonceCacheEntry)
}

func (d *DiscoveryManager) GetModelMetadata(ctx context.Context) ([]ModelMetadata, error) {
	if err := d.maybeRefreshModelMap(ctx); err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	metadata := make([]ModelMetadata, 0, len(d.modelMeta))
	for _, entry := range d.modelMeta {
		metadata = append(metadata, entry)
	}
	return metadata, nil
}

func (d *DiscoveryManager) ResolveChuteID(ctx context.Context, model string) (string, error) {
	if looksLikeUUID(model) {
		return model, nil
	}
	if err := d.maybeRefreshModelMap(ctx); err != nil {
		return "", err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if chuteID := d.modelMap[model]; chuteID != "" {
		return chuteID, nil
	}
	return "", fmt.Errorf("model %q not found. Check /v1/models for available chutes", model)
}

func (d *DiscoveryManager) ResolveE2EEChuteID(ctx context.Context, model string) (string, error) {
	if looksLikeUUID(model) {
		return "", errors.New("raw chute IDs are not accepted for E2EE chat. Choose an advertised confidential-compute model name so metadata can be verified")
	}
	if err := d.maybeRefreshModelMap(ctx); err != nil {
		return "", err
	}

	d.mu.Lock()
	meta, ok := d.modelMeta[model]
	d.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("model %q not found. Check /v1/models for available chutes", model)
	}
	if !isE2EETextModelMeta(meta) {
		return "", fmt.Errorf("model %q is not advertised as a confidential-compute text model. Refusing to send encrypted chat payload", model)
	}
	return meta.ChuteID, nil
}

func (d *DiscoveryManager) GetNonce(ctx context.Context, chuteID string) (E2EEInstance, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.evictExpiredNonceCacheLocked()
	if result, ok := d.takeNonceLocked(chuteID); ok {
		return result, nil
	}

	discovery, err := d.fetchInstancesLocked(ctx, chuteID)
	if err != nil {
		return E2EEInstance{}, err
	}
	d.nonceCache[chuteID] = discovery
	if result, ok := d.takeNonceLocked(chuteID); ok {
		return result, nil
	}

	return E2EEInstance{}, fmt.Errorf("no nonces available for chute %s. The chute may have no active E2EE-capable instances", chuteID)
}

func (d *DiscoveryManager) ClearNonceCache(chuteID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if chuteID == "" {
		d.nonceCache = make(map[string]*nonceCacheEntry)
		return
	}
	delete(d.nonceCache, chuteID)
}

func (d *DiscoveryManager) maybeRefreshModelMap(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if time.Since(d.modelMapLoadedAt) < ModelMapTTL {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, ModelFetchTimeout)
	defer cancel()

	modelsURL := d.modelsBase + "/v1/models"
	res, err := fetchWithRetry(ctx, d.client, func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("accept", "application/json")
		if d.apiKey != "" {
			req.Header.Set("authorization", "Bearer "+d.apiKey)
		}
		return req, nil
	}, DefaultMaxRetries)
	if err != nil {
		return err
	}

	var body struct {
		Data []struct {
			ID                  string   `json:"id"`
			ChuteID             string   `json:"chute_id"`
			InputModalities     []string `json:"input_modalities"`
			OutputModalities    []string `json:"output_modalities"`
			SupportedFeatures   []string `json:"supported_features"`
			ContextLength       *int64   `json:"context_length"`
			MaxModelLen         *int64   `json:"max_model_len"`
			MaxOutputLength     *int64   `json:"max_output_length"`
			ConfidentialCompute bool     `json:"confidential_compute"`
		} `json:"data"`
	}
	if err := decodeJSONResponse(res, &body); err != nil {
		return err
	}

	nextMap := make(map[string]string)
	nextMeta := make(map[string]ModelMetadata)
	for _, entry := range body.Data {
		if entry.ID == "" || entry.ChuteID == "" {
			continue
		}
		input := entry.InputModalities
		if len(input) == 0 {
			input = []string{"text"}
		}
		output := entry.OutputModalities
		if len(output) == 0 {
			output = []string{"text"}
		}
		contextLength := entry.ContextLength
		if contextLength == nil {
			contextLength = entry.MaxModelLen
		}
		nextMap[entry.ID] = entry.ChuteID
		nextMeta[entry.ID] = ModelMetadata{
			ID:                  entry.ID,
			ChuteID:             entry.ChuteID,
			InputModalities:     input,
			OutputModalities:    output,
			SupportedFeatures:   entry.SupportedFeatures,
			ContextLength:       contextLength,
			MaxOutputLength:     entry.MaxOutputLength,
			ConfidentialCompute: entry.ConfidentialCompute,
		}
	}

	d.modelMap = nextMap
	d.modelMeta = nextMeta
	d.modelMapLoadedAt = time.Now()
	return nil
}

func (d *DiscoveryManager) fetchInstancesLocked(ctx context.Context, chuteID string) (*nonceCacheEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, InstanceTimeout)
	defer cancel()

	instancesURL := d.apiBase + "/e2e/instances/" + url.PathEscape(chuteID)
	res, err := fetchWithRetry(ctx, d.client, func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, instancesURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("accept", "application/json")
		if d.apiKey != "" {
			req.Header.Set("authorization", "Bearer "+d.apiKey)
		}
		return req, nil
	}, 0)
	if err != nil {
		return nil, err
	}

	var body struct {
		NonceExpiresIn int `json:"nonce_expires_in"`
		Instances      []struct {
			InstanceID string   `json:"instance_id"`
			E2EPubKey  string   `json:"e2e_pubkey"`
			Nonces     []string `json:"nonces"`
		} `json:"instances"`
	}
	if err := decodeJSONResponse(res, &body); err != nil {
		return nil, err
	}

	instances := make([]nonceInstance, 0, len(body.Instances))
	rejected := 0
	for _, raw := range body.Instances {
		instanceID := normalizeHeaderValue(raw.InstanceID, 256)
		pubKey := normalizeE2EPubKey(raw.E2EPubKey)
		nonces := make([]string, 0, len(raw.Nonces))
		for _, nonce := range raw.Nonces {
			if normalized := normalizeHeaderValue(nonce, 4096); normalized != "" {
				nonces = append(nonces, normalized)
			}
		}
		if instanceID == "" || pubKey == "" || len(nonces) == 0 {
			rejected++
			continue
		}
		instances = append(instances, nonceInstance{
			instanceID: instanceID,
			e2ePubKey:  pubKey,
			nonces:     nonces,
		})
	}

	if len(body.Instances) > 0 && len(instances) == 0 {
		return nil, fmt.Errorf("Chutes returned %d E2EE instance(s), but none had valid public key and nonce material", len(body.Instances))
	}
	if rejected > 0 {
		fmt.Printf("[e2ee] ignored %d malformed E2EE instance record(s) for chute %s\n", rejected, chuteID)
	}

	ttl := body.NonceExpiresIn
	if ttl <= 0 {
		ttl = 55
	}
	return &nonceCacheEntry{
		instances: instances,
		expiresAt: time.Now().Add(time.Duration(ttl) * time.Second),
	}, nil
}

func (d *DiscoveryManager) takeNonceLocked(chuteID string) (E2EEInstance, bool) {
	cached := d.nonceCache[chuteID]
	if cached == nil || time.Now().After(cached.expiresAt) {
		delete(d.nonceCache, chuteID)
		return E2EEInstance{}, false
	}

	for i := range cached.instances {
		if len(cached.instances[i].nonces) == 0 {
			continue
		}
		nonce := cached.instances[i].nonces[0]
		cached.instances[i].nonces = cached.instances[i].nonces[1:]
		return E2EEInstance{
			InstanceID: cached.instances[i].instanceID,
			E2EPubKey:  cached.instances[i].e2ePubKey,
			Nonce:      nonce,
		}, true
	}
	return E2EEInstance{}, false
}

func (d *DiscoveryManager) evictExpiredNonceCacheLocked() {
	now := time.Now()
	for chuteID, entry := range d.nonceCache {
		if now.After(entry.expiresAt) {
			delete(d.nonceCache, chuteID)
		}
	}
}

func looksLikeUUID(value string) bool {
	return regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`).MatchString(value)
}

func isE2EETextModelMeta(meta ModelMetadata) bool {
	return meta.ID != "" &&
		meta.ChuteID != "" &&
		meta.ConfidentialCompute &&
		containsString(meta.InputModalities, "text") &&
		containsString(meta.OutputModalities, "text")
}

func normalizeHeaderValue(value string, maxLength int) string {
	if value == "" || len(value) > maxLength {
		return ""
	}
	if strings.TrimSpace(value) != value {
		return ""
	}
	if strings.ContainsAny(value, "\r\n") {
		return ""
	}
	return value
}

func normalizeE2EPubKey(value string) string {
	normalized := normalizeHeaderValue(value, 4096)
	if normalized == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(normalized)
	if err != nil || len(decoded) != MLKEMPublicKeySize {
		return ""
	}
	return normalized
}
