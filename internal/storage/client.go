// Package storage provides an S3-compatible object storage client.
//
// Tested against MinIO (local dev) and Cloudflare R2 (production).
// Both use path-style URLs and AWS Signature Version 4.
//
// Configuration is via Config; the endpoint, bucket, credentials, and region
// are all runtime-configurable so no code changes are needed to switch targets.
package storage

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config holds the storage backend configuration.
// All fields are required; use EnvConfig() to populate from environment variables.
type Config struct {
	// Endpoint is the base URL of the S3-compatible server, without trailing slash.
	// MinIO example:  "http://minio:9000"
	// R2 example:     "https://<account-id>.r2.cloudflarestorage.com"
	Endpoint string

	// Bucket is the name of the bucket to use for all operations.
	Bucket string

	// AccessKeyID and SecretAccessKey are the AWS/S3-compatible credentials.
	AccessKeyID     string
	SecretAccessKey string

	// Region is the AWS region or equivalent.
	// For MinIO: "us-east-1" (conventional; MinIO ignores it)
	// For R2:    "auto" or the actual region
	Region string
}

// Client is a production-grade S3-compatible object storage client.
// It is safe for concurrent use.
type Client struct {
	cfg        Config
	httpClient *http.Client
	baseURL    string // cfg.Endpoint + "/" + cfg.Bucket
}

// New constructs a Client and validates that the endpoint URL is parseable.
// It does not open a network connection; call EnsureBucket to verify connectivity
// and create the bucket if required.
func New(cfg Config) (*Client, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("storage: endpoint is required")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("storage: bucket is required")
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, fmt.Errorf("storage: access key ID and secret access key are required")
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("storage: region is required")
	}
	if _, err := url.Parse(cfg.Endpoint); err != nil {
		return nil, fmt.Errorf("storage: invalid endpoint %q: %w", cfg.Endpoint, err)
	}
	return &Client{
		cfg:     cfg,
		baseURL: strings.TrimRight(cfg.Endpoint, "/") + "/" + cfg.Bucket,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				MaxIdleConns:          50,
				MaxIdleConnsPerHost:   10,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 20 * time.Second,
			},
		},
	}, nil
}

// EnsureBucket creates the bucket if it does not already exist.
// It is idempotent and safe to call on every startup.
func (c *Client) EnsureBucket(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodPut, "", nil, "")
	if err != nil {
		return fmt.Errorf("storage: ensure bucket: %w", err)
	}
	resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		return nil
	case http.StatusConflict: // BucketAlreadyOwnedByYou — MinIO / S3
		return nil
	default:
		return fmt.Errorf("storage: ensure bucket: unexpected status %d", resp.StatusCode)
	}
}

// ObjectExists reports whether an object at the given path exists in the bucket.
// path must be the full object key, e.g. "canonical/observation/2026/01/01/sha256:abc....json".
func (c *Client) ObjectExists(ctx context.Context, path string) (bool, error) {
	resp, err := c.do(ctx, http.MethodHead, path, nil, "")
	if err != nil {
		return false, fmt.Errorf("storage: head %s: %w", path, err)
	}
	resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, &Error{StatusCode: resp.StatusCode, Code: "HeadFailed",
			Message: fmt.Sprintf("unexpected status %d for HEAD %s", resp.StatusCode, path)}
	}
}

// PutObject uploads data to the given path in the bucket.
// The operation is idempotent for content-addressed objects because the same
// key will always contain the same bytes.
//
// PutObject does NOT enforce no-overwrite at the S3 level; the caller is
// responsible for calling ObjectExists first if duplicate detection is required.
func (c *Client) PutObject(ctx context.Context, path string, data []byte, contentType string) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			delay := backoff(attempt)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		resp, err := c.do(ctx, http.MethodPut, path, data, contentType)
		if err != nil {
			if isRetryableNetErr(err) {
				lastErr = err
				continue
			}
			return fmt.Errorf("storage: put %s: %w", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusOK, http.StatusNoContent:
			return nil
		case http.StatusInternalServerError, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			lastErr = parseS3Error(resp.StatusCode, body)
			continue // retry
		default:
			return fmt.Errorf("storage: put %s: %w", path, parseS3Error(resp.StatusCode, body))
		}
	}
	return fmt.Errorf("storage: put %s: %w (after retries)", path, lastErr)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// do builds, signs, and executes a single request against the storage backend.
// path is the object key (empty string targets the bucket itself).
// body may be nil for HEAD/GET requests or empty puts.
func (c *Client) do(ctx context.Context, method, path string, body []byte, contentType string) (*http.Response, error) {
	rawURL := c.baseURL
	if path != "" {
		rawURL += "/" + strings.TrimPrefix(path, "/")
	}

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	} else {
		body = nil // normalise nil vs empty for signing
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("content-type", contentType)
	}

	sign(req, c.cfg.AccessKeyID, c.cfg.SecretAccessKey, c.cfg.Region, body)

	return c.httpClient.Do(req)
}

// ---------------------------------------------------------------------------
// S3 error handling
// ---------------------------------------------------------------------------

// Error represents an error response from the S3-compatible API.
type Error struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *Error) Error() string {
	return fmt.Sprintf("storage %s (%d): %s", e.Code, e.StatusCode, e.Message)
}

type s3ErrorXML struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}

func parseS3Error(status int, body []byte) error {
	var parsed s3ErrorXML
	if err := xml.Unmarshal(body, &parsed); err == nil && parsed.Code != "" {
		return &Error{StatusCode: status, Code: parsed.Code, Message: parsed.Message}
	}
	return &Error{StatusCode: status, Code: "Unknown", Message: string(body)}
}

// ---------------------------------------------------------------------------
// Retry helpers
// ---------------------------------------------------------------------------

func isRetryableNetErr(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if ok := isNetError(err, &netErr); ok {
		return netErr.Timeout()
	}
	msg := err.Error()
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "broken pipe")
}

// isNetError unwraps err to find a net.Error, storing it in target.
func isNetError(err error, target *net.Error) bool {
	for err != nil {
		if ne, ok := err.(net.Error); ok {
			*target = ne
			return true
		}
		// unwrap one level
		type unwrapper interface{ Unwrap() error }
		if u, ok := err.(unwrapper); ok {
			err = u.Unwrap()
		} else {
			break
		}
	}
	return false
}

// backoff returns a jittered exponential delay for the given attempt number
// (1-indexed; attempt 1 → ~200ms, attempt 2 → ~400ms, …).
func backoff(attempt int) time.Duration {
	base := 100 * time.Millisecond * (1 << attempt) // 200ms, 400ms, 800ms
	// Add ±25% jitter.
	jitter := time.Duration(rand.Int63n(int64(base) / 2))
	return base + jitter - base/4
}
