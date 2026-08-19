// Package httpclient performs HTTP exchanges against one configured
// backend at a time.
//
// This is the Go port of flutter/lib/services/http_service.dart, which is
// itself the Dart port of pebble/src/pkjs/http.js -- see that file's header
// comment for the reasoning behind everything that carries over unchanged:
//
//   - Two timeout budgets, not one. A read is quick or it is broken; an
//     action that changes physical state can legitimately take a long time,
//     so it gets its own, longer budget rather than making every request
//     wait a minute before giving up. GetTimeout / ActionTimeout are the Go
//     names for GET_TIMEOUT_MS / ACTION_TIMEOUT_MS. Each request gets its
//     own context.WithTimeout rather than one shared http.Client timeout,
//     so the two budgets can differ per call.
//   - failure.Unreachable is distinguished from every other kind. A
//     request that never arrived says nothing about the state of the thing
//     it asked about, and must never be rendered as if the server had
//     answered -- see docs/DESIGN.md, "A failed request is not an answer".
//   - The secret goes in a request header, never a query string. A key in
//     a URI is written to the server's access log and, behind a reverse
//     proxy, the proxy's log too.
//   - Nothing in this package logs a header value it sent. Response
//     headers are logged in full -- which one identifies the answering
//     node, the cache or the proxy is a property of the deployment, not
//     something this generic client gets to assume -- but a header this
//     package *set* (the auth header above all) never reaches the log.
//
// What did not carry over: the two XHR-specific workarounds http.js exists
// to route around (a status-0 branch for connection failures that fire no
// event, and a distinction between sending an empty string and sending
// nothing at all, for Content-Length). Go's
// net/http returns an error before any *http.Response exists on a
// connection failure, and its request body is sent exactly as given, so
// neither workaround has anything to route around here. The *behaviour*
// those workarounds guaranteed is still a requirement and is still pinned
// by tests ported from flutter/test/services/http_service_test.dart.
//
// Entity in HTTPResponse is the empty interface (any), not a siren.Entity,
// deliberately: this layer knows only that a server speaks JSON, not what
// shape a particular response is -- that is internal/siren's job, one layer
// up. Keeping this type ignorant of Siren is what keeps this package free
// of any server-specific vocabulary, same as http_service.dart never
// importing siren.dart -- this package must never import internal/siren.
package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/urlresolve"
)

// GetTimeout is the budget a GET/HEAD gets before it is reported as
// failure.Timeout. Carried over unchanged from GET_TIMEOUT_MS in http.js.
const GetTimeout = 20 * time.Second

// ActionTimeout is the budget anything else (an action) gets. Longer than
// GetTimeout because an action can legitimately take a long time to
// complete on the server, and a client giving up early on a request that
// is still working cannot tell that apart from one that failed. Carried
// over unchanged from ACTION_TIMEOUT_MS in http.js.
const ActionTimeout = 60 * time.Second

// HTTPResponse is one successful exchange: the status, the URL it was
// actually fetched from (after resolution against the backend's base), and
// the decoded body. See the package comment for why Entity is any, not a
// siren.Entity.
type HTTPResponse struct {
	Status int
	URL    string
	Entity any
}

// Client performs HTTP exchanges against one configured backend.Backend at
// a time.
//
// The *http.Client and the log function are both overridable via Option,
// mirroring http_service.dart's injected http.Client and log parameters:
// production code gets the zero-value http.Client and a no-op log by
// default (a caller wanting output supplies WithLog), while tests point
// getTimeout/actionTimeout at millisecond-scale values via
// WithGetTimeout/WithActionTimeout so a test proving the two budgets are
// actually different has no reason to wait 20 real seconds to prove it.
type Client struct {
	httpClient    *http.Client
	log           func(message string)
	getTimeout    time.Duration
	actionTimeout time.Duration
}

// Option configures a Client built by New.
type Option func(*Client)

// WithHTTPClient overrides the *http.Client used to send requests. Tests
// point this at an httptest.Server's URL through backend.BaseURL rather
// than swapping the client itself; this option exists mainly for
// completeness and for a caller that wants to plug in a custom transport.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithLog overrides where request/response log lines go. Defaults to a
// no-op, mirroring debugPrint in http_service.dart being the production
// default and a capturing function being the test double.
func WithLog(logf func(message string)) Option {
	return func(c *Client) { c.log = logf }
}

// WithGetTimeout overrides the GET/HEAD timeout budget. For tests only --
// production code relies on the GetTimeout default -- so a test asserting
// the timeout kind does not have to wait out the real 20-second budget.
func WithGetTimeout(d time.Duration) Option {
	return func(c *Client) { c.getTimeout = d }
}

// WithActionTimeout overrides the action timeout budget. For tests only --
// see WithGetTimeout.
func WithActionTimeout(d time.Duration) Option {
	return func(c *Client) { c.actionTimeout = d }
}

// New builds a Client with the production defaults, overridden by opts.
func New(opts ...Option) *Client {
	c := &Client{
		httpClient:    &http.Client{},
		log:           func(string) {},
		getTimeout:    GetTimeout,
		actionTimeout: ActionTimeout,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Get is the common case, spelled out so callers do not pass a nil fields
// map -- mirrors get in http.js / http_service.dart.
func (c *Client) Get(be backend.Backend, href string) (HTTPResponse, error) {
	return c.Request(be, href, http.MethodGet, nil)
}

// Request performs one HTTP exchange against be.
//
// href is whatever the server put in the document -- absolute,
// root-relative or relative -- and is resolved against be's base URL. Only
// the scheme and authority come from us; the path always came from the
// server (see internal/urlresolve and docs/DESIGN.md, "The rule everything
// else follows from").
func (c *Client) Request(be backend.Backend, href, method string, fields map[string]string) (HTTPResponse, error) {
	verb := strings.ToUpper(method)
	target := urlresolve.Resolve(href, be.BaseURL)
	isRead := verb == http.MethodGet || verb == http.MethodHead

	headers, err := buildHeaders(be, !isRead)
	if err != nil {
		return HTTPResponse{}, err
	}

	timeout := c.actionTimeout
	if isRead {
		timeout = c.getTimeout
	}

	c.log(fmt.Sprintf("%s %s%s", verb, urlresolve.Origin(target), urlresolve.Path(target)))

	resp, body, elapsed, err := c.exchange(verb, target, headers, requestBody(isRead, fields), timeout)
	if err != nil {
		return HTTPResponse{}, c.classifyTransportError(err, verb, target, timeout)
	}

	return c.finish(verb, target, resp, body, elapsed)
}

// requestBody builds the body for the outgoing request. A read never
// carries one (nil, so net/http sends no Content-Length at all). Anything
// else always carries one, even an empty string when fields is nil --
// mirrors the shape of http_service.dart sending an explicit empty string
// rather than no body at all: some servers reject a POST with neither a
// body nor Content-Length.
func requestBody(isRead bool, fields map[string]string) io.Reader {
	if isRead {
		return nil
	}
	if fields == nil {
		return strings.NewReader("")
	}
	return strings.NewReader(urlresolve.EncodeForm(fields))
}

// exchange sends one request under a per-call context.WithTimeout and
// reads the full response body before the deadline is torn down.
//
// The context's cancel func is deferred here, not in the caller, and
// deliberately spans the io.ReadAll call: net/http ties a response body
// read to the request's context, so cancelling before the body is fully
// read would abort an in-progress read with a spurious error, not just
// bound the time to first byte.
func (c *Client) exchange(verb, target string, headers map[string]string, body io.Reader, timeout time.Duration) (*http.Response, []byte, time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, verb, target, body)
	if err != nil {
		return nil, nil, 0, err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	startedAt := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, time.Since(startedAt), err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	elapsed := time.Since(startedAt)
	if err != nil {
		return resp, nil, elapsed, err
	}
	return resp, respBody, elapsed, nil
}

// classifyTransportError turns whatever net/http returned before a
// response existed into a Failure.
//
// A timeout is distinguished from every other transport error by checking
// errors.Is(err, context.DeadlineExceeded): our own per-request context is
// the only deadline in play, so that error -- and only that error -- means
// the request outran its budget. Anything else (DNS failure, TLS error,
// refused connection, a body read that broke mid-stream) means the request
// never arrived and the answer never will; that is exactly
// failure.Unreachable, per docs/DESIGN.md's "A failed request is not an
// answer" -- it says nothing about the state of the thing we asked about,
// only that we could not ask.
func (c *Client) classifyTransportError(err error, verb, target string, timeout time.Duration) error {
	if errors.Is(err, context.DeadlineExceeded) {
		c.log(fmt.Sprintf("%s %s -> timed out after %dms", verb, urlresolve.Path(target), timeout.Milliseconds()))
		return &failure.Failure{Kind: failure.Timeout, Message: "timed out"}
	}

	// The message names the host, because the one thing worth knowing
	// here is which host could not be reached -- the banner elsewhere
	// already says that it could not.
	host := urlresolve.Origin(target)
	if host == "" {
		host = target
	}
	return &failure.Failure{Kind: failure.Unreachable, Message: fmt.Sprintf("no answer from %s", host)}
}

// finish turns a completed exchange into either an error or a result --
// mirrors _finish in http_service.dart.
func (c *Client) finish(verb, target string, resp *http.Response, body []byte, elapsed time.Duration) (HTTPResponse, error) {
	logResponse(c.log, verb, target, resp.StatusCode, resp.Header, elapsed)

	entity := parseBody(body)
	if kind, isFailure := classifyStatus(resp.StatusCode); isFailure {
		return HTTPResponse{}, &failure.Failure{
			Kind:    kind,
			Status:  resp.StatusCode,
			Message: describeFailure(kind, resp.StatusCode, entity),
		}
	}
	if entity == nil {
		return HTTPResponse{}, &failure.Failure{Kind: failure.Parse, Status: resp.StatusCode, Message: "response was not JSON"}
	}
	return HTTPResponse{Status: resp.StatusCode, URL: target, Entity: entity}, nil
}
