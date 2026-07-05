package chutes

import "time"

const (
	DefaultAPIBase    = "https://api.chutes.ai"
	DefaultModelsBase = "https://llm.chutes.ai"

	MLKEMCiphertextSize = 1088
	MLKEMPublicKeySize  = 1184
	ChaChaNonceSize     = 12
	ChaChaTagSize       = 16
	HKDFKeySize         = 32

	ModelMapTTL        = 5 * time.Minute
	ModelFetchTimeout  = 15 * time.Second
	InstanceTimeout    = 30 * time.Second
	InvokeTimeout      = 120 * time.Second
	DefaultMaxRetries  = 3
	DefaultBaseBackoff = time.Second
)

var (
	InfoReq    = []byte("e2e-req-v1")
	InfoResp   = []byte("e2e-resp-v1")
	InfoStream = []byte("e2e-stream-v1")
)
