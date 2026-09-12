package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AWSCredentials are static AWS credentials.
type AWSCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// SignSigV4 signs req in place with AWS Signature Version 4 (header auth).
// body is the request body as sent. Behaviour matches the AWS SDK signer:
// the path is URI-encoded twice except for S3, query parameters are sorted,
// and Content-Length is signed when present.
func SignSigV4(req *http.Request, body []byte, creds AWSCredentials, service, region string, at time.Time) {
	req.Header.Set("X-Amz-Content-Sha256", sha256hex(body))
	sign(req, sha256hex(body), creds, service, region, at)
}

// signWithout signs without adding X-Amz-Content-Sha256 (used by tests
// against the official vectors, which omit that header).
func signWithout(req *http.Request, creds AWSCredentials, service, region string, at time.Time) {
	sign(req, sha256hex(nil), creds, service, region, at)
}

func sign(req *http.Request, payloadHash string, creds AWSCredentials, service, region string, at time.Time) {
	at = at.UTC()
	amzDate := at.Format("20060102T150405Z")
	dateStamp := at.Format("20060102")

	req.Header.Set("X-Amz-Date", amzDate)
	if creds.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", creds.SessionToken)
	}

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	headers := map[string]string{"host": host}
	for name, values := range req.Header {
		lower := strings.ToLower(name)
		if ignoredHeaders[lower] {
			continue
		}
		headers[lower] = canonicalValue(values)
	}
	if req.ContentLength > 0 {
		headers["content-length"] = strconv.FormatInt(req.ContentLength, 10)
	}
	names := make([]string, 0, len(headers))
	for n := range headers {
		names = append(names, n)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, n := range names {
		canonHeaders.WriteString(n + ":" + headers[n] + "\n")
	}
	signedHeaders := strings.Join(names, ";")

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalPath(req.URL, service),
		canonicalQuery(req.URL.Query()),
		canonHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := strings.Join([]string{dateStamp, region, service, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{"AWS4-HMAC-SHA256", amzDate, scope, sha256hex([]byte(canonicalRequest))}, "\n")

	kDate := hmacSHA256([]byte("AWS4"+creds.SecretAccessKey), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+creds.AccessKeyID+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
}

var ignoredHeaders = map[string]bool{
	"authorization":     true,
	"user-agent":        true,
	"x-amzn-trace-id":   true,
	"expect":            true,
	"transfer-encoding": true,
}

func canonicalValue(values []string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strings.Join(strings.Fields(v), " ")
	}
	return strings.Join(parts, ",")
}

func canonicalPath(u *url.URL, service string) string {
	path := u.EscapedPath()
	if u.Opaque != "" {
		path = "/" + strings.Join(strings.Split(u.Opaque, "/")[3:], "/")
	}
	if path == "" {
		path = "/"
	}
	if service == "s3" {
		return path
	}
	return awsEscape(path, false)
}

func canonicalQuery(q url.Values) string {
	delete(q, "X-Amz-Signature")
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vals := append([]string(nil), q[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, awsEscape(k, true)+"="+awsEscape(v, true))
		}
	}
	return strings.Join(parts, "&")
}

// awsEscape percent-encodes every byte except the unreserved set and,
// unless encodeSlash is set, the path separator.
func awsEscape(s string, encodeSlash bool) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && !encodeSlash:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&15])
		}
	}
	return b.String()
}

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}
