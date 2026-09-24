package runner

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
)

// Exit codes returned by the CLI.
const (
	ExitOK        = 0
	ExitAssert    = 1 // an assertion or capture failed
	ExitUsage     = 2 // parse error, unknown request, missing variable
	ExitTransport = 3 // could not reach the server, timeout, TLS failure
)

// Code identifies a kind of error apic reports. Codes are stable: a script
// or an agent can branch on one, and docs/errors.md (generated from
// Catalogue) explains each.
type Code string

// The codes. E1xx is the request as written, E2xx the command and the
// project around it, E3xx the network.
const (
	CodeOther           Code = "E200"
	CodeMissingVariable Code = "E101"
	CodeBuild           Code = "E102"
	CodeInvalidFile     Code = "E103"
	CodeDirective       Code = "E104"
	CodeAuth            Code = "E105"
	CodeBodyFile        Code = "E106"
	CodeRef             Code = "E107"
	CodeUnknownRequest  Code = "E201"
	CodeAmbiguous       Code = "E202"
	CodeFlag            Code = "E203"
	CodeEnvironment     Code = "E204"
	CodeProject         Code = "E205"
	CodeTLSConfig       Code = "E206"
	CodeProxy           Code = "E207"
	CodeSession         Code = "E208"
	CodeData            Code = "E209"
	CodeFile            Code = "E210"
	CodeImport          Code = "E211"
	CodeFeatures        Code = "E212"
	CodeTerminal        Code = "E213"
	CodeServer          Code = "E214"
	CodeTransport       Code = "E300"
	CodeConnect         Code = "E301"
	CodeTimeout         Code = "E302"
	CodeTLS             Code = "E303"
	CodeProtocol        Code = "E304"
	CodeCancelled       Code = "E305"
)

// Entry is one row of the error catalogue.
type Entry struct {
	Code  Code
	Exit  int
	Title string // a few words, e.g. "missing variable"
	Hint  string // one line: what to do about it
	About string // what it means and how it comes about, for docs/errors.md
}

// DocsURL is where docs/errors.md is published; an entry's anchor is its
// code in lower case.
const DocsURL = "https://datagriff.github.io/api-caller/errors/"

// URL is the entry's address on the documentation site.
func (c Code) URL() string { return DocsURL + "#" + strings.ToLower(string(c)) }

// Catalogue is every code, in order. docs/errors.md is generated from it
// (task docs:errors), so a message and its documentation cannot drift.
var Catalogue = []Entry{
	{CodeMissingVariable, ExitUsage, "missing variable",
		"Pass it with --var name=value, add it to an env file, or run the request that captures it first.",
		"A `{{placeholder}}` in the request has no value from any source: `--var`, the environment's entry in the env files, `.env`, the session, or a request earlier in the same run. The message names each missing variable and, when another request's `# @capture` provides it, that request. A `# @ref` to that request makes it run first by itself; `apic describe <request>` shows where every variable comes from."},
	{CodeBuild, ExitUsage, "request cannot be built",
		"Check the placeholder, URL, header or body the message names.",
		"The request's text could not be turned into a request: a built-in with bad arguments (`{{$randomInt 5 1}}`), an unknown built-in, a URL that does not parse once its placeholders are filled in, or a GraphQL body whose variables are not a JSON object."},
	{CodeInvalidFile, ExitUsage, "request file has errors",
		"Run apic validate to see every problem with its line and column.",
		"The `.http` file failed to parse where the request is: a malformed directive, a multipart body without a boundary, a GraphQL request without a query. apic refuses to send from a file with errors rather than guess; `apic validate` lists them all."},
	{CodeDirective, ExitUsage, "bad directive value",
		"Fix the directive's value; docs/format.md lists what each takes.",
		"A directive is well formed but its value cannot be used: `# @timeout soon`, `# @retry 0`, `# @sleep -1s`, an `# @assert` that does not parse, an HTTP version other than HTTP/1.1 or HTTP/2 on the request line, or `HTTP/2` over plain `http://`."},
	{CodeAuth, ExitUsage, "authentication failed",
		"Check the # @auth spec (or auth.default), its credentials, and the token endpoint.",
		"The request's auth could not be applied: an `# @auth` spec that does not parse, `# @auth exec` without `auth.allowExec`, an OAuth2 token endpoint that refused the grant, a browser sign-in with no terminal, or a JetBrains `Security.Auth` configuration that does not exist or cannot be used."},
	{CodeBodyFile, ExitUsage, "body file problem",
		"Check that the file exists, relative to the .http file, and lies inside the project.",
		"A file the request reads could not be used: a `< file` body, a multipart part's file, or a schema for `matchesSchema`. Paths are relative to the `.http` file and must resolve (symlinks included) inside the project root, so a request file cannot read something outside it."},
	{CodeRef, ExitUsage, "@ref problem",
		"Point # @ref at exactly one request, and break any cycle.",
		"A `# @ref` or `# @forceRef` names no request, several requests, or leads back to the request it started from. `apic validate` reports these as `bad-ref` and `ref-cycle` before anything runs."},
	{CodeOther, ExitUsage, "error",
		"The message says what went wrong; if it is unclear, please open an issue.",
		"An error without a more specific code. It should be rare: if you meet one, the message is worth an issue so it can get a code of its own."},
	{CodeUnknownRequest, ExitUsage, "unknown request",
		"Run apic list to see the request ids, and use name, file.http, file.http#name or file.http#N.",
		"The target names no request: no `# @name` of that name, no such `.http` file, or no request at that position in the file. The message suggests the closest names."},
	{CodeAmbiguous, ExitUsage, "several requests match",
		"Pick one with file.http#name, or give the command a single request.",
		"The command works on one request (`describe`, `curl`, `snippet`) and the target names a whole file or a name used in several files."},
	{CodeFlag, ExitUsage, "bad flag or argument",
		"Check the command's flags with apic <command> --help.",
		"A flag has a value the command cannot use, two flags cannot be combined (`--output` with a flow or `--data`, `--use-session` with `--no-session`), an argument is missing or extra, or the command does not exist."},
	{CodeEnvironment, ExitUsage, "unknown environment",
		"Use one of the environments the message lists, or add it to http-client.env.json.",
		"`--env` (or `env:` in `apic.yaml`) names an environment that neither env file declares, or there are no env files at all. `apic env` lists the environments found."},
	{CodeProject, ExitUsage, "project configuration problem",
		"Fix apic.yaml or the env file the message names; apic validate helps.",
		"The project could not be loaded: `apic.yaml` or an env file is not valid YAML/JSON, or a setting in it (a timeout, a retry policy) cannot be read."},
	{CodeTLSConfig, ExitUsage, "TLS configuration problem",
		"Check the CA file, client certificate and key the message names.",
		"A TLS setting cannot be used: a CA file with no PEM certificates, a certificate or key that does not load, a key that is world-readable or needs a passphrase apic cannot supply, or a path outside the project."},
	{CodeProxy, ExitUsage, "proxy configuration problem",
		"Give the proxy as an http, https, socks5 or socks5h URL.",
		"`--proxy`, `proxy:` in `apic.yaml` or `HTTP(S)_PROXY` holds a URL apic cannot use. apic refuses the request rather than send it directly."},
	{CodeSession, ExitUsage, "session or cookie store problem",
		"Check .apic/ is writable, or run with --no-session.",
		"`.apic/session.json` or the cookie jar could not be read or written, or a session command ran with `--no-session`."},
	{CodeData, ExitUsage, "bad data file",
		"Give --data a CSV file with a header row, or a JSON array of objects.",
		"The rows for `apic run --data` could not be read: an empty file, a header column without a name, a row with the wrong number of fields, or JSON that is not an array of objects."},
	{CodeFile, ExitUsage, "cannot read or write a file",
		"Check the path and its permissions.",
		"A file named on the command line (a report, a data file, a file to format, stdin) could not be read or written."},
	{CodeImport, ExitUsage, "import failed",
		"Check the input is an OpenAPI 3 document, a Postman v2 collection or a curl command.",
		"`apic import` could not read what it was given, or refused to overwrite files without `--force`."},
	{CodeFeatures, ExitUsage, "feature file problem",
		"Check the feature files, the step text and the tag expression.",
		"`apic test` could not run the features: no `.feature` files, a feature that does not parse, an invalid tag expression, a step table or variable name it cannot use, or a step naming a target that is not a request."},
	{CodeTerminal, ExitUsage, "needs a terminal",
		"Run apic ui in a terminal; use apic run or apic list --json in scripts.",
		"`apic ui` is interactive: it refuses `--json` and a stdout that is not a terminal."},
	{CodeServer, ExitUsage, "server could not start",
		"Check the address or port is free and allowed.",
		"`apic mcp --http` or `apic demo` could not listen where it was asked to."},
	{CodeTransport, ExitTransport, "request failed",
		"The message has the underlying error; check the URL and the network.",
		"The request could not complete for a reason none of the E3xx codes below describes."},
	{CodeConnect, ExitTransport, "could not connect",
		"Check the host name, the port and that the server is running.",
		"The host name did not resolve, the connection was refused or reset, or the network is unreachable. Nothing reached the server."},
	{CodeTimeout, ExitTransport, "timed out",
		"Raise --timeout, # @timeout or timeout: in apic.yaml, or check the server.",
		"The server did not answer within the timeout (30 seconds unless `--timeout`, `timeout:` in `apic.yaml` or `# @timeout` says otherwise)."},
	{CodeTLS, ExitTransport, "TLS handshake failed",
		"Trust the server's CA with --cacert or tls.caFile, or check the client certificate.",
		"The TLS handshake failed: the server's certificate is not trusted, has the wrong name or has expired, or the server refused the client certificate. `--insecure` skips verification for one command, as a last resort."},
	{CodeProtocol, ExitTransport, "protocol problem",
		"Check the server speaks what the request asks for.",
		"The server's answer could not be used: it did not negotiate HTTP/2 for a request line that asks for it, the response was malformed, or the body was larger than apic reads."},
	{CodeCancelled, ExitTransport, "cancelled",
		"Run the command again.",
		"The run was interrupted (Ctrl-C, or `esc` in the UI) while a request, a `# @sleep` or a retry wait was in progress."},
}

// Lookup returns a code's entry.
func Lookup(c Code) (Entry, bool) {
	for _, e := range Catalogue {
		if e.Code == c {
			return e, true
		}
	}
	return Entry{}, false
}

// UsageError is a problem with the request definition or invocation.
type UsageError struct {
	Code Code // the catalogue entry; empty reads as CodeOther
	Msg  string
}

func (e *UsageError) Error() string { return e.Msg }

// Usage builds a UsageError with its code.
func Usage(code Code, msg string) *UsageError { return &UsageError{Code: code, Msg: msg} }

// Usagef is Usage with a format. When one of args is itself an error that
// carries a code (a missing variable inside a header, say), that code is
// kept: it says more than the outer one.
func Usagef(code Code, format string, args ...any) *UsageError {
	for _, a := range args {
		if err, ok := a.(error); ok {
			var ue *UsageError
			if errors.As(err, &ue) && ue.Code != "" {
				code = ue.Code
				break
			}
		}
	}
	return &UsageError{Code: code, Msg: fmt.Sprintf(format, args...)}
}

func usagef(code Code, format string, args ...any) error {
	return Usagef(code, format, args...)
}

// TransportError is a network-level failure.
type TransportError struct {
	Code Code // set where the cause is known; otherwise classified from Err
	Err  error
}

func (e *TransportError) Error() string { return "request failed: " + e.Err.Error() }
func (e *TransportError) Unwrap() error { return e.Err }

// ErrorCode is the catalogue code for the failure.
func (e *TransportError) ErrorCode() Code {
	if e.Code != "" {
		return e.Code
	}
	return classify(e.Err)
}

// classify reads the kind of a network failure from the error chain.
func classify(err error) Code {
	var dns *net.DNSError
	var op *net.OpError
	var cert *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var alert tls.AlertError
	var record tls.RecordHeaderError
	var netErr net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return CodeCancelled
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return CodeTimeout
	case errors.As(err, &cert), errors.As(err, &unknownCA), errors.As(err, &hostname), errors.As(err, &invalid),
		errors.As(err, &alert), errors.As(err, &record), strings.Contains(err.Error(), "tls: "):
		return CodeTLS
	case errors.As(err, &dns), errors.As(err, &op):
		return CodeConnect
	}
	return CodeTransport
}

// ErrorInfo is an error as --json, MCP and a result report it: the
// catalogue entry plus the message. It is part of the --json contract.
type ErrorInfo struct {
	Code    Code   `json:"code"`
	Title   string `json:"title"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
	Exit    int    `json:"exit"`
	URL     string `json:"url"`
}

// Info describes err with its catalogue entry. The exit status is the
// entry's, which is what ExitCode gives for the same error.
func Info(err error) *ErrorInfo {
	c := CodeOf(err)
	e, _ := Lookup(c)
	return &ErrorInfo{Code: c, Title: e.Title, Message: err.Error(), Hint: e.Hint, Exit: e.Exit, URL: c.URL()}
}

// CodeOf is the catalogue code for an error: its own when it carries one,
// CodeOther for any other.
func CodeOf(err error) Code {
	var te *TransportError
	if errors.As(err, &te) {
		return te.ErrorCode()
	}
	var ue *UsageError
	if errors.As(err, &ue) && ue.Code != "" {
		return ue.Code
	}
	return CodeOther
}

// ExitCode maps an error to the CLI exit code.
//
// The exit codes are a public contract, so this unwraps rather than switching
// on the concrete type: wrapping a TransportError with %w anywhere on the way
// up would otherwise turn a documented 3 into a 2.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var te *TransportError
	if errors.As(err, &te) {
		return ExitTransport
	}
	return ExitUsage
}
