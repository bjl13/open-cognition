package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	sigAlgorithm = "AWS4-HMAC-SHA256"
	sigTerminator = "aws4_request"
	s3Service     = "s3"
	timeFormat    = "20060102T150405Z"
	dateFormat    = "20060102"
)

// sign adds AWS Signature Version 4 headers to req.
//
// The body bytes must be supplied even for requests that have already set
// req.Body, so the signing code can hash them without consuming the reader.
// For requests with no body pass nil (the empty-payload hash is used).
func sign(req *http.Request, accessKey, secretKey, region string, body []byte) {
	now := time.Now().UTC()
	date := now.Format(dateFormat)
	datetime := now.Format(timeFormat)

	payloadHash := hashHex(body)

	// Inject the mandatory S3 signing headers before computing the canonical form.
	req.Header.Set("x-amz-date", datetime)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	if req.Header.Get("host") == "" {
		req.Header.Set("host", req.URL.Host)
	}

	signedHeaders, canonicalHeaders := buildCanonicalHeaders(req)
	canonicalURI := encodeURIPath(req.URL.EscapedPath())
	canonicalQS := buildCanonicalQueryString(req.URL.Query())

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQS,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	credentialScope := fmt.Sprintf("%s/%s/%s/%s", date, region, s3Service, sigTerminator)
	stringToSign := strings.Join([]string{
		sigAlgorithm,
		datetime,
		credentialScope,
		hashHex([]byte(canonicalRequest)),
	}, "\n")

	signingKey := derivedKey(secretKey, date, region, s3Service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	req.Header.Set("Authorization", fmt.Sprintf(
		"%s Credential=%s/%s,SignedHeaders=%s,Signature=%s",
		sigAlgorithm, accessKey, credentialScope, signedHeaders, signature,
	))
}

// buildCanonicalHeaders returns (signedHeaders, canonicalHeaderBlock).
// Only headers that are already set on req are included; the caller is
// responsible for injecting x-amz-date, x-amz-content-sha256, and host first.
func buildCanonicalHeaders(req *http.Request) (signed, block string) {
	// Collect header names we want to sign.
	headerNames := []string{"host", "content-type", "x-amz-content-sha256", "x-amz-date"}

	type kv struct{ k, v string }
	var pairs []kv
	seen := make(map[string]bool)

	for _, name := range headerNames {
		lower := strings.ToLower(name)
		if seen[lower] {
			continue
		}
		var val string
		if lower == "host" {
			val = req.Host
			if val == "" {
				val = req.URL.Host
			}
		} else {
			val = req.Header.Get(name)
		}
		if val == "" {
			continue
		}
		seen[lower] = true
		pairs = append(pairs, kv{lower, strings.TrimSpace(val)})
	}

	// Add any x-amz-* headers not already covered.
	for name, values := range req.Header {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-amz-") && !seen[lower] {
			seen[lower] = true
			pairs = append(pairs, kv{lower, strings.TrimSpace(strings.Join(values, ","))})
		}
	}

	sort.Slice(pairs, func(i, j int) bool { return pairs[i].k < pairs[j].k })

	names := make([]string, len(pairs))
	var sb strings.Builder
	for i, p := range pairs {
		names[i] = p.k
		sb.WriteString(p.k)
		sb.WriteByte(':')
		sb.WriteString(p.v)
		sb.WriteByte('\n')
	}

	return strings.Join(names, ";"), sb.String()
}

// buildCanonicalQueryString returns the URL-encoded, sorted query string.
func buildCanonicalQueryString(vals url.Values) string {
	if len(vals) == 0 {
		return ""
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		ek := url.QueryEscape(k)
		for _, v := range vals[k] {
			parts = append(parts, ek+"="+url.QueryEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

// encodeURIPath percent-encodes each path segment per the AWS SigV4 spec:
// every byte is encoded except unreserved characters (A-Z a-z 0-9 - _ . ~)
// and the segment delimiter '/'.
func encodeURIPath(path string) string {
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		segments[i] = encodeURIComponent(seg)
	}
	return strings.Join(segments, "/")
}

// encodeURIComponent percent-encodes a single path segment.
func encodeURIComponent(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUnreserved(c) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func isUnreserved(c byte) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '_' || c == '.' || c == '~'
}

// derivedKey computes the HMAC-SHA256 signing key for the given credentials.
func derivedKey(secretKey, date, region, service string) []byte {
	k := hmacSHA256([]byte("AWS4"+secretKey), []byte(date))
	k = hmacSHA256(k, []byte(region))
	k = hmacSHA256(k, []byte(service))
	return hmacSHA256(k, []byte(sigTerminator))
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func hashHex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
