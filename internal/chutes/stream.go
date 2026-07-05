package chutes

import (
	"bufio"
	"context"
	"crypto/mlkem"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type StreamProcessor struct {
	responseKey   *mlkem.DecapsulationKey768
	streamKey     []byte
	parseFailures int
}

func NewStreamProcessor(responseKey *mlkem.DecapsulationKey768) *StreamProcessor {
	return &StreamProcessor{responseKey: responseKey}
}

func ReadSSE(ctx context.Context, reader io.Reader, onLine func(string) error) error {
	buffered := bufio.NewReader(reader)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line, err := buffered.ReadString('\n')
		if len(line) > 0 {
			if cbErr := onLine(line); cbErr != nil {
				return cbErr
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (p *StreamProcessor) Process(line string) ([]string, bool, error) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "data: ") {
		return nil, false, nil
	}

	raw := strings.TrimSpace(strings.TrimPrefix(trimmed, "data: "))
	if raw == "" {
		return nil, false, nil
	}
	if raw == "[DONE]" {
		return nil, true, nil
	}

	var event map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		p.parseFailures++
		if p.parseFailures >= 10 {
			return nil, false, fmt.Errorf("too many consecutive SSE parse failures - stream may be corrupted")
		}
		return nil, false, nil
	}
	p.parseFailures = 0

	if initRaw, ok := event["e2e_init"]; ok {
		var initB64 string
		if err := json.Unmarshal(initRaw, &initB64); err != nil {
			return nil, false, err
		}
		streamKey, err := decryptStreamInit(p.responseKey, initB64)
		if err != nil {
			return nil, false, err
		}
		p.streamKey = streamKey
		return nil, false, nil
	}

	if chunkRaw, ok := event["e2e"]; ok {
		if len(p.streamKey) == 0 {
			return nil, false, fmt.Errorf("received e2e chunk before e2e_init")
		}
		var chunkB64 string
		if err := json.Unmarshal(chunkRaw, &chunkB64); err != nil {
			return nil, false, err
		}
		decrypted, err := decryptStreamChunk(chunkB64, p.streamKey)
		if err != nil {
			return nil, false, err
		}
		return extractSSEData(decrypted), false, nil
	}

	if _, ok := event["usage"]; ok {
		return []string{raw}, false, nil
	}

	if errRaw, ok := event["e2e_error"]; ok {
		var message string
		if err := json.Unmarshal(errRaw, &message); err == nil && message != "" {
			return nil, false, fmt.Errorf("%s", message)
		}
		return nil, false, fmt.Errorf("Chutes E2EE stream failed: %s", string(errRaw))
	}

	return nil, false, nil
}

func extractSSEData(text string) []string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "data: ") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "data: "))
			if value != "" {
				out = append(out, value)
			}
		}
	}
	if len(out) == 0 && strings.TrimSpace(text) != "" {
		out = append(out, strings.TrimSpace(text))
	}
	return out
}
