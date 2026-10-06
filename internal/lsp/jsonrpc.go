// Package lsp is apic's language server: `apic lsp` speaks the Language
// Server Protocol over stdio, so any editor with an LSP client gets the
// diagnostics, completion, hover, code lenses and formatting the VS Code
// extension has, from the same code the CLI runs.
//
// The transport is JSON-RPC 2.0 with the protocol's Content-Length
// framing, written here in a couple of hundred lines rather than pulled
// in as a library, in the spirit of the SigV4 signer and the OpenAPI
// reader.
package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
)

// message is any JSON-RPC 2.0 message: a request (ID and Method), a
// notification (Method only) or a response (ID and Result or Error).
type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  any              `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

// rpcError is a JSON-RPC error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("%s (%d)", e.Message, e.Code) }

// JSON-RPC and LSP error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
	codeNotInitialized = -32002
	codeRequestFailed  = -32803
)

// conn reads and writes framed messages. Writes are serialised, since
// replies and notifications come from more than one goroutine.
type conn struct {
	r  *bufio.Reader
	mu sync.Mutex
	w  io.Writer
}

func newConn(r io.Reader, w io.Writer) *conn {
	return &conn{r: bufio.NewReader(r), w: w}
}

// maxMessage bounds one message, so a client that sends a nonsense
// Content-Length cannot make the server allocate without limit.
const maxMessage = 64 << 20

// read returns the next message. A clean end of the stream is io.EOF.
func (c *conn) read() (*message, error) {
	headers, err := textproto.NewReader(c.r).ReadMIMEHeader()
	if err != nil {
		if errors.Is(err, io.EOF) && len(headers) == 0 {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("reading message header: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(headers.Get("Content-Length")))
	if err != nil || n < 0 || n > maxMessage {
		return nil, fmt.Errorf("bad Content-Length %q", headers.Get("Content-Length"))
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(c.r, body); err != nil {
		return nil, fmt.Errorf("reading message body: %w", err)
	}
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, &rpcError{Code: codeParseError, Message: err.Error()}
	}
	return &m, nil
}

// write sends one message.
func (c *conn) write(m *message) error {
	m.JSONRPC = "2.0"
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = c.w.Write(body)
	return err
}

// reply answers a request. A nil result is sent as JSON null, which the
// protocol requires for "no result" (no hover, no edits).
func (c *conn) reply(id *json.RawMessage, result any, err error) error {
	if id == nil {
		// A message too broken to have an id is answered with "id": null.
		null := json.RawMessage("null")
		id = &null
	}
	m := &message{ID: id}
	switch {
	case err != nil:
		var re *rpcError
		if !errors.As(err, &re) {
			re = &rpcError{Code: codeRequestFailed, Message: err.Error()}
		}
		m.Error = re
	case result == nil:
		m.Result = json.RawMessage("null")
	default:
		m.Result = result
	}
	return c.write(m)
}

// notify sends a notification to the client.
func (c *conn) notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.write(&message{Method: method, Params: raw})
}
