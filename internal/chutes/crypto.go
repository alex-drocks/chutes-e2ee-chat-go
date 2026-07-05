package chutes

import (
	"bytes"
	"compress/gzip"
	"crypto/mlkem"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

func deriveKey(sharedSecret, mlkemCiphertext, info []byte) ([]byte, error) {
	if len(mlkemCiphertext) < 16 {
		return nil, fmt.Errorf("ML-KEM ciphertext is too short")
	}
	reader := hkdf.New(sha256.New, sharedSecret, mlkemCiphertext[:16], info)
	key := make([]byte, HKDFKeySize)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

func chachaEncrypt(key, nonce, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	return aead.Seal(nil, nonce, plaintext, nil), nil
}

func chachaDecrypt(key, nonce, sealed []byte) ([]byte, error) {
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("ChaCha20-Poly1305 decryption failed: invalid authentication tag")
	}
	return plaintext, nil
}

func BuildE2EERequest(e2ePubKeyB64 string, payload map[string]interface{}) ([]byte, *mlkem.DecapsulationKey768, error) {
	responseKey, err := mlkem.GenerateKey768()
	if err != nil {
		return nil, nil, err
	}
	responsePubKey := responseKey.EncapsulationKey().Bytes()

	e2ePubKey, err := base64.StdEncoding.DecodeString(e2ePubKeyB64)
	if err != nil {
		return nil, nil, err
	}
	if len(e2ePubKey) != MLKEMPublicKeySize {
		return nil, nil, fmt.Errorf("expected e2e_pubkey %d bytes, got %d", MLKEMPublicKeySize, len(e2ePubKey))
	}

	remoteKey, err := mlkem.NewEncapsulationKey768(e2ePubKey)
	if err != nil {
		return nil, nil, err
	}
	sharedSecret, mlkemCiphertext := remoteKey.Encapsulate()
	symKey, err := deriveKey(sharedSecret, mlkemCiphertext, InfoReq)
	if err != nil {
		return nil, nil, err
	}

	payloadWithResponseKey := make(map[string]interface{}, len(payload)+1)
	for key, value := range payload {
		payloadWithResponseKey[key] = value
	}
	payloadWithResponseKey["e2e_response_pk"] = base64.StdEncoding.EncodeToString(responsePubKey)

	payloadBytes, err := json.Marshal(payloadWithResponseKey)
	if err != nil {
		return nil, nil, err
	}

	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	if _, err := gzipWriter.Write(payloadBytes); err != nil {
		return nil, nil, err
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, nil, err
	}

	nonce := make([]byte, ChaChaNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	sealed, err := chachaEncrypt(symKey, nonce, compressed.Bytes())
	if err != nil {
		return nil, nil, err
	}

	blob := make([]byte, 0, len(mlkemCiphertext)+len(nonce)+len(sealed))
	blob = append(blob, mlkemCiphertext...)
	blob = append(blob, nonce...)
	blob = append(blob, sealed...)
	return blob, responseKey, nil
}

func DecryptResponse(responseBlob []byte, responseKey *mlkem.DecapsulationKey768) (interface{}, error) {
	if len(responseBlob) < MLKEMCiphertextSize+ChaChaNonceSize+ChaChaTagSize+1 {
		return nil, fmt.Errorf("response blob too small: %d bytes", len(responseBlob))
	}

	mlkemCiphertext := responseBlob[:MLKEMCiphertextSize]
	nonce := responseBlob[MLKEMCiphertextSize : MLKEMCiphertextSize+ChaChaNonceSize]
	sealed := responseBlob[MLKEMCiphertextSize+ChaChaNonceSize:]

	sharedSecret, err := responseKey.Decapsulate(mlkemCiphertext)
	if err != nil {
		return nil, err
	}
	symKey, err := deriveKey(sharedSecret, mlkemCiphertext, InfoResp)
	if err != nil {
		return nil, err
	}
	plaintext, err := chachaDecrypt(symKey, nonce, sealed)
	if err != nil {
		return nil, err
	}

	gzipReader, err := gzip.NewReader(bytes.NewReader(plaintext))
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()

	decompressed, err := io.ReadAll(gzipReader)
	if err != nil {
		return nil, err
	}

	var decoded interface{}
	if err := json.Unmarshal(decompressed, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func decryptStreamInit(responseKey *mlkem.DecapsulationKey768, mlkemCiphertextB64 string) ([]byte, error) {
	mlkemCiphertext, err := base64.StdEncoding.DecodeString(mlkemCiphertextB64)
	if err != nil {
		return nil, err
	}
	sharedSecret, err := responseKey.Decapsulate(mlkemCiphertext)
	if err != nil {
		return nil, err
	}
	return deriveKey(sharedSecret, mlkemCiphertext, InfoStream)
}

func decryptStreamChunk(encChunkB64 string, streamKey []byte) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encChunkB64)
	if err != nil {
		return "", err
	}
	if len(raw) < ChaChaNonceSize+ChaChaTagSize+1 {
		return "", fmt.Errorf("stream chunk too small: %d bytes", len(raw))
	}
	nonce := raw[:ChaChaNonceSize]
	sealed := raw[ChaChaNonceSize:]
	plaintext, err := chachaDecrypt(streamKey, nonce, sealed)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
