package runner

import (
	"strings"

	"github.com/dataGriff/api-caller/internal/assert"
)

// This file holds the response half of redaction. The request half
// (DisplayHeaders, DisplayBody, DisplayURL) lives beside Resolved in
// runner.go; both are driven by Result.Redact, set from Options.Redact.
//
// What --redact hides is a documented contract: see docs/cli.md. The rule is
// that structure survives and values do not, so a redacted run still tells CI
// which request failed and why without writing a credential into a stored log.

// sensitiveResponseHeaders carry credentials the server sends back. They are
// masked whenever the response is redacted. Captures read the raw header, so
// "# @capture sid = header.set-cookie" keeps working; only display changes.
var sensitiveResponseHeaders = map[string]bool{
	"set-cookie": true, "www-authenticate": true, "proxy-authenticate": true,
	"authorization": true, "x-amz-security-token": true,
}

// DisplayHeaders returns the response headers with every value masked when
// redacting, and sensitive values masked either way.
func (r *Response) DisplayHeaders(redact bool) map[string]string {
	if r == nil {
		return nil
	}
	out := make(map[string]string, len(r.Headers))
	for k, v := range r.Headers {
		if redact || sensitiveResponseHeaders[strings.ToLower(k)] {
			out[k] = Masked
			continue
		}
		out[k] = v
	}
	return out
}

// DisplayResponse returns the response as it should be shown: status, timing
// and size survive because they are not secrets and CI needs them, while the
// body and header values are masked when redacting.
func (r Result) DisplayResponse() *Response {
	if r.Response == nil {
		return nil
	}
	out := *r.Response
	out.Headers = r.Response.DisplayHeaders(r.Redact)
	if r.Redact && out.Body != nil {
		out.Body = Masked
		out.BodyEncoding = "" // the mask is text, whatever the body was
	}
	return &out
}

// DisplayRawBody returns the raw response body for renderers, masked when
// redacting.
func (r *Result) DisplayRawBody() []byte {
	if r.raw == nil {
		return nil
	}
	if r.Redact && len(r.raw.Body) > 0 {
		return []byte(Masked)
	}
	return r.raw.Body
}

// DisplayAsserts returns the assertion results with the values they carry
// masked when redacting. An assertion like "# @assert body.$.token == {{token}}"
// bakes the rendered secret into Expr, Expected and Actual, so all three are
// hidden; Pass and Error are structural and survive.
func (r Result) DisplayAsserts() []assert.Result {
	if !r.Redact || len(r.Asserts) == 0 {
		return r.Asserts
	}
	out := make([]assert.Result, len(r.Asserts))
	for i, a := range r.Asserts {
		out[i] = a
		out[i].Expr = RedactExpr(a.Expr)
		if a.Actual != "" {
			out[i].Actual = Masked
		}
		if a.Expected != "" {
			out[i].Expected = Masked
		}
	}
	return out
}

// RedactExpr keeps the selector and operator of an assertion expression and
// hides the expected value.
func RedactExpr(expr string) string {
	fields := strings.Fields(expr)
	if len(fields) <= 2 {
		return expr
	}
	return fields[0] + " " + fields[1] + " " + Masked
}
