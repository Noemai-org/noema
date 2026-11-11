package understanding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// compilePath is the model service's compile endpoint (PERD 39.3, mTLS, contracts only).
const compilePath = "/internal/v1/understanding:compile"

var packRefRe = regexp.MustCompile(`^[a-z][a-z0-9]*@[0-9]+\.[0-9]+\.[0-9]+$`)

// Failure is how a compile call failed, in the words the callers act on.
type Failure string

// Every failure of a compile call.
const (
	FailureProviderUnavailable Failure = "PROVIDER_UNAVAILABLE" // every provider errored, timed out or returned a refused reply (REQ-INT-002)
	FailureInputInvalid        Failure = "INPUT_INVALID"        // the model service refused the canonical input
	FailureUnavailable         Failure = "UNAVAILABLE"          // the model service could not be reached or answered with a server error
	FailureMalformed           Failure = "MALFORMED"            // the answer was not the contract's: an unknown member, a wrong shape, a body too large
)

// CompileError is the error of Compile. It never carries the request or the response body (AGENT-13).
type CompileError struct{ Failure Failure }

func (e *CompileError) Error() string {
	return prefix + "the compile call failed: " + string(e.Failure)
}

// CompileRequest is the body of the compile call: contracts objects only. CanonicalInput and OpenIntent are JSON documents the caller produced
// (canonical-input.v2, and the admitted intent of an open investigation); PackRef is `<vertical>@<version>`.
type CompileRequest struct {
	CanonicalInput json.RawMessage
	OpenIntent     json.RawMessage
	PackRef        string
}

// Compiled is the answer: the Intent Object exactly as the model service returned it (untrusted until Gate.Admit), and the canonical input it used.
type Compiled struct {
	IntentObject   json.RawMessage
	CanonicalInput json.RawMessage
}

// ClientConfig is what NewClient needs. The caller builds HTTP with the mTLS identity of the core (PERD 18.4, boundary 2); the client holds no credential.
type ClientConfig struct {
	BaseURL          string
	HTTP             *http.Client
	MaxResponseBytes int64 // from configuration: a response longer than this is MALFORMED (AGENT-16)
}

// Client calls the model service's compile endpoint.
type Client struct {
	base string
	http *http.Client
	max  int64
}

// NewClient refuses a base URL that is not http(s)://host[:port] with no path, a missing HTTP client, and a non-positive bound.
func NewClient(c ClientConfig) (*Client, error) {
	u, err := url.Parse(c.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return nil, errors.New(prefix + "the model service URL must be http(s)://host[:port] with no path or query")
	}
	if c.HTTP == nil || c.MaxResponseBytes <= 0 {
		return nil, errors.New(prefix + "the client needs an HTTP client and a positive response bound")
	}
	hc := *c.HTTP
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse } // a redirect would carry the body to another host
	return &Client{base: strings.TrimRight(c.BaseURL, "/"), http: &hc, max: c.MaxResponseBytes}, nil
}

// Compile asks the model service to compile one canonical input. The context needs a deadline; without one nothing is sent. The returned objects are untrusted:
// pass IntentObject to Gate.Admit.
func (c *Client) Compile(ctx context.Context, req CompileRequest) (Compiled, error) {
	if _, ok := ctx.Deadline(); !ok {
		return Compiled{}, errors.New(prefix + "the context needs a deadline")
	}
	if !packRefRe.MatchString(req.PackRef) || !json.Valid(req.CanonicalInput) || (len(req.OpenIntent) > 0 && !json.Valid(req.OpenIntent)) {
		return Compiled{}, errors.New(prefix + "the compile request is not well formed")
	}
	body := map[string]json.RawMessage{"canonical_input": req.CanonicalInput, "pack_ref": mustJSON(req.PackRef)}
	if len(req.OpenIntent) > 0 {
		body["open_intent"] = req.OpenIntent
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Compiled{}, fmt.Errorf("%sencode the request: %w", prefix, err)
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+compilePath, bytes.NewReader(raw))
	if err != nil {
		return Compiled{}, errors.New(prefix + "the compile request could not be built")
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(hreq)
	if err != nil {
		return Compiled{}, &CompileError{Failure: FailureUnavailable}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.max+1))
	if err != nil {
		return Compiled{}, &CompileError{Failure: FailureUnavailable}
	}
	if int64(len(data)) > c.max {
		return Compiled{}, &CompileError{Failure: FailureMalformed}
	}
	if resp.StatusCode == http.StatusOK {
		return decodeCompiled(data)
	}
	return Compiled{}, failureOf(resp.StatusCode, data)
}

func mustJSON(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

// decodeCompiled reads `{intent_object, canonical_input}` strictly: both members, nothing else, no repeated member, nothing after the document.
func decodeCompiled(data []byte) (Compiled, error) {
	bad := &CompileError{Failure: FailureMalformed}
	if err := wholeDocument(data); err != nil {
		return Compiled{}, bad
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil || len(members) != 2 {
		return Compiled{}, bad
	}
	if repeatedMember(data) {
		return Compiled{}, bad
	}
	intent, ok1 := members["intent_object"]
	input, ok2 := members["canonical_input"]
	if !ok1 || !ok2 || isNull(intent) || isNull(input) {
		return Compiled{}, bad
	}
	return Compiled{IntentObject: append(json.RawMessage(nil), intent...), CanonicalInput: append(json.RawMessage(nil), input...)}, nil
}

func isNull(b json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(b), []byte("null")) }

// repeatedMember is true when the top-level object names a member twice (encoding/json keeps the last and says nothing).
func repeatedMember(data []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(data))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return true
	}
	seen := map[string]bool{}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return true
		}
		key, _ := k.(string)
		if seen[key] {
			return true
		}
		seen[key] = true
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return true
		}
	}
	return false
}

// failureOf maps a non-200 answer. The model service's error envelope (errors.v2) is read strictly; anything else with a client status is MALFORMED, and a
// server status is UNAVAILABLE.
func failureOf(status int, data []byte) error {
	var env contracts.ErrorsV2ErrorEnvelope
	if err := json.Unmarshal(data, &env); err == nil {
		switch env.Error.Code {
		case contracts.ErrorsV2ErrorCodePROVIDERUNAVAILABLE:
			return &CompileError{Failure: FailureProviderUnavailable}
		case contracts.ErrorsV2ErrorCodeINPUTINVALID:
			return &CompileError{Failure: FailureInputInvalid}
		}
	}
	if status >= 500 {
		return &CompileError{Failure: FailureUnavailable}
	}
	return &CompileError{Failure: FailureMalformed}
}
