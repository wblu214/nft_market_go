package ipfs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"time"
)

// PinataClient is a minimal client for Pinata's IPFS API.
// It supports uploading files via pinFileToIPFS and returns CID + gateway URL.
type PinataClient struct {
	apiURL       string
	gatewayURL   string
	apiKey       string
	secretAPIKey string
	httpClient   *http.Client
}

// NewPinataClient creates a new client.
func NewPinataClient(apiURL, gatewayURL, apiKey, secret string) *PinataClient {
	if apiURL == "" {
		apiURL = "https://api.pinata.cloud"
	}
	if gatewayURL == "" {
		gatewayURL = "https://gateway.pinata.cloud/ipfs"
	}
	return &PinataClient{
		apiURL:       apiURL,
		gatewayURL:   gatewayURL,
		apiKey:       apiKey,
		secretAPIKey: secret,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// UploadResult contains the CID and public URL for an uploaded file.
type UploadResult struct {
	CID string
	URL string
}

type pinataFileResponse struct {
	IpfsHash  string `json:"IpfsHash"`
	PinSize   int64  `json:"PinSize"`
	Timestamp string `json:"Timestamp"`
}

// UploadFile uploads a single file stream to Pinata.
// name is used as the file name in the multipart form.
func (c *PinataClient) UploadFile(ctx context.Context, name string, r io.Reader) (*UploadResult, error) {
	if c.apiKey == "" || c.secretAPIKey == "" {
		return nil, fmt.Errorf("pinata api key or secret is not configured")
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	filename := filepath.Base(name)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}

	if _, err := io.Copy(part, r); err != nil {
		return nil, fmt.Errorf("copy file data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/pinning/pinFileToIPFS", &buf)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("pinata_api_key", c.apiKey)
	req.Header.Set("pinata_secret_api_key", c.secretAPIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pinata request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pinata error: status=%d body=%s", resp.StatusCode, string(body))
	}

	var parsed pinataFileResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if parsed.IpfsHash == "" {
		return nil, fmt.Errorf("pinata response missing IpfsHash")
	}

	url := fmt.Sprintf("%s/%s", c.gatewayURL, parsed.IpfsHash)
	return &UploadResult{
		CID: parsed.IpfsHash,
		URL: url,
	}, nil
}
