package chutes

import (
	"errors"
	"strings"
)

const maxRequestIDLength = 128

type ModelMetadata struct {
	ID                  string   `json:"id"`
	ChuteID             string   `json:"chuteId"`
	InputModalities     []string `json:"inputModalities"`
	OutputModalities    []string `json:"outputModalities"`
	SupportedFeatures   []string `json:"supportedFeatures"`
	ContextLength       *int64   `json:"contextLength"`
	MaxOutputLength     *int64   `json:"maxOutputLength"`
	ConfidentialCompute bool     `json:"confidentialCompute"`
}

type E2EEInstance struct {
	InstanceID string
	E2EPubKey  string
	Nonce      string
}

func NormalizeRequestID(requestID string) (string, error) {
	trimmed := strings.TrimSpace(requestID)
	if trimmed == "" {
		return "", errors.New("invalid request id")
	}
	if len(trimmed) > maxRequestIDLength {
		return "", errors.New("request id is too long")
	}
	return trimmed, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
