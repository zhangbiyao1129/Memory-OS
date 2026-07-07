package secretlocal

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"memory-os/internal/secret"
)

// Client 是本机 MCP 访问 Memory OS 服务端 secret 端点的 HTTP 客户端。
// 它只上传/下载密文与元信息，绝不发送明文。
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(baseURL, token string, httpClient *http.Client) Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		http:    httpClient,
	}
}

// Metadata 是服务端返回的 secret 元信息（无明文/密文）。
type Metadata struct {
	SecretRef   string `json:"secret_ref"`
	OwnerUserID string `json:"owner_user_id"`
	OrgID       string `json:"org_id"`
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	EnvName     string `json:"env_name"`
	Site        string `json:"site"`
	Purpose     string `json:"purpose"`
	ExpiresAt   string `json:"expires_at"`
	Status      string `json:"status"`
	Version     int    `json:"current_version"`
}

type CreateParams struct {
	OrgID     string
	ProjectID string
	Name      string
	EnvName   string
	Site      string
	Purpose   string
	ExpiresAt string
}

type encryptedPayload struct {
	Algorithm      string `json:"algorithm"`
	DeviceKeyID    string `json:"device_key_id"`
	KeyFingerprint string `json:"key_fingerprint"`
	NonceB64       string `json:"nonce_b64"`
	CiphertextB64  string `json:"ciphertext_b64"`
}

func (c Client) Create(params CreateParams, blob secret.EncryptedBlob) (Metadata, error) {
	payload := map[string]any{
		"org_id":     params.OrgID,
		"project_id": params.ProjectID,
		"name":       params.Name,
		"env_name":   params.EnvName,
		"site":       params.Site,
		"purpose":    params.Purpose,
		"encrypted":  encodeBlob(blob),
	}
	if strings.TrimSpace(params.ExpiresAt) != "" {
		payload["expires_at"] = params.ExpiresAt
	}
	var meta Metadata
	if err := c.post("/memory/secrets/create", payload, &meta); err != nil {
		return Metadata{}, err
	}
	return meta, nil
}

func (c Client) List(orgID, projectID, status string) ([]Metadata, error) {
	payload := map[string]any{"org_id": orgID, "project_id": projectID}
	if strings.TrimSpace(status) != "" {
		payload["status"] = status
	}
	var response struct {
		Secrets []Metadata `json:"secrets"`
	}
	if err := c.post("/memory/secrets/list", payload, &response); err != nil {
		return nil, err
	}
	return response.Secrets, nil
}

func (c Client) GetCiphertext(secretRef string) (Metadata, secret.EncryptedBlob, error) {
	var response struct {
		Metadata
		Encrypted encryptedPayload `json:"encrypted"`
	}
	if err := c.post("/memory/secrets/ciphertext", map[string]any{"secret_ref": secretRef}, &response); err != nil {
		return Metadata{}, secret.EncryptedBlob{}, err
	}
	blob, err := decodeBlob(response.Encrypted)
	if err != nil {
		return Metadata{}, secret.EncryptedBlob{}, err
	}
	return response.Metadata, blob, nil
}

func (c Client) Disable(secretRef, orgID, projectID string) (Metadata, error) {
	payload := map[string]any{"secret_ref": secretRef, "org_id": orgID, "project_id": projectID}
	var meta Metadata
	if err := c.post("/memory/secrets/disable", payload, &meta); err != nil {
		return Metadata{}, err
	}
	return meta, nil
}

func (c Client) post(path string, payload any, out any) error {
	if c.baseURL == "" {
		return errors.New("MEMORY_OS_API_URL is required")
	}
	if c.token == "" {
		return errors.New("MEMORY_OS_TOKEN is required")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode >= 400 {
		return fmt.Errorf("secret api %s failed: status %d: %s", path, response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(responseBody, out)
}

func encodeBlob(blob secret.EncryptedBlob) encryptedPayload {
	return encryptedPayload{
		Algorithm:      blob.Algorithm,
		DeviceKeyID:    blob.DeviceKeyID,
		KeyFingerprint: blob.KeyFingerprint,
		NonceB64:       encodeStd(blob.Nonce),
		CiphertextB64:  encodeStd(blob.Ciphertext),
	}
}

func decodeBlob(payload encryptedPayload) (secret.EncryptedBlob, error) {
	nonce, err := base64.StdEncoding.DecodeString(payload.NonceB64)
	if err != nil {
		return secret.EncryptedBlob{}, fmt.Errorf("decode nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(payload.CiphertextB64)
	if err != nil {
		return secret.EncryptedBlob{}, fmt.Errorf("decode ciphertext: %w", err)
	}
	return secret.EncryptedBlob{
		Algorithm:      payload.Algorithm,
		DeviceKeyID:    payload.DeviceKeyID,
		KeyFingerprint: payload.KeyFingerprint,
		Nonce:          nonce,
		Ciphertext:     ciphertext,
	}, nil
}

func encodeStd(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
