package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const maxAPIKeyLength = 512

var credentialsAAD = []byte("chutes-e2ee-chat.credentials.v2")

type Credentials struct {
	ChutesAPIKey string `json:"chutesApiKey,omitempty"`
}

type Store struct {
	dir             string
	credentialsFile string
	keyFile         string
	mu              sync.Mutex
}

type envelope struct {
	Version    int    `json:"version"`
	Mode       string `json:"mode"`
	Alg        string `json:"alg"`
	IV         string `json:"iv"`
	Tag        string `json:"tag"`
	Ciphertext string `json:"ciphertext"`
}

func NewStore(appName string) (*Store, error) {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}

	dir := filepath.Join(base, appName)
	return &Store{
		dir:             dir,
		credentialsFile: filepath.Join(dir, "credentials.enc"),
		keyFile:         filepath.Join(dir, "credentials.key"),
	}, nil
}

func NormalizeAPIKey(apiKey string) (string, error) {
	trimmed := strings.TrimSpace(apiKey)
	if trimmed == "" {
		return "", errors.New("API key is required")
	}
	if len(trimmed) > maxAPIKeyLength {
		return "", errors.New("API key is too long")
	}
	return trimmed, nil
}

func (s *Store) Load() (Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := os.ReadFile(s.credentialsFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Credentials{}, nil
		}
		return Credentials{}, err
	}

	var env envelope
	if err := json.Unmarshal(stored, &env); err != nil {
		return Credentials{}, fmt.Errorf("credentials file is not a local-file-key envelope: %w", err)
	}
	if env.Version != 2 || env.Mode != "localFileKey" || env.Alg != "aes-256-gcm" {
		return Credentials{}, errors.New("unsupported credential storage envelope")
	}

	key, err := s.loadLocalKey(false)
	if err != nil {
		return Credentials{}, err
	}
	iv, err := base64.StdEncoding.DecodeString(env.IV)
	if err != nil {
		return Credentials{}, err
	}
	tag, err := base64.StdEncoding.DecodeString(env.Tag)
	if err != nil {
		return Credentials{}, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return Credentials{}, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return Credentials{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Credentials{}, err
	}

	sealed := append(append([]byte{}, ciphertext...), tag...)
	plaintext, err := gcm.Open(nil, iv, sealed, credentialsAAD)
	if err != nil {
		return Credentials{}, err
	}

	var creds Credentials
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return Credentials{}, err
	}
	return creds, nil
}

func (s *Store) Save(creds Credentials) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, err := s.loadLocalKey(true)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	sealed := gcm.Seal(nil, iv, payload, credentialsAAD)
	if len(sealed) < gcm.Overhead() {
		return errors.New("credential encryption failed")
	}
	ciphertext := sealed[:len(sealed)-gcm.Overhead()]
	tag := sealed[len(sealed)-gcm.Overhead():]

	env := envelope{
		Version:    2,
		Mode:       "localFileKey",
		Alg:        "aes-256-gcm",
		IV:         base64.StdEncoding.EncodeToString(iv),
		Tag:        base64.StdEncoding.EncodeToString(tag),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(s.credentialsFile, body, 0o600); err != nil {
		return err
	}
	_ = os.Chmod(s.credentialsFile, 0o600)
	return nil
}

func (s *Store) Delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(s.credentialsFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(s.keyFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) Status() map[string]interface{} {
	creds, _ := s.Load()
	hasKey := creds.ChutesAPIKey != ""
	return map[string]interface{}{
		"hasApiKey":        hasKey,
		"hasStoredKey":     hasKey,
		"source":           map[bool]string{true: "stored", false: "none"}[hasKey],
		"canPersist":       true,
		"storageMode":      "localFileKey",
		"storageBackend":   "localFileKey",
		"isOsBackedStorage": false,
	}
}

func (s *Store) loadLocalKey(create bool) ([]byte, error) {
	encoded, err := os.ReadFile(s.keyFile)
	if err == nil {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
		if err == nil && len(key) == 32 {
			return key, nil
		}
	}
	if !create {
		return nil, errors.New("local credential key is missing or invalid")
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(s.keyFile, []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		return nil, err
	}
	_ = os.Chmod(s.keyFile, 0o600)
	return key, nil
}
