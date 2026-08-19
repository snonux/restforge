package httpclient

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/snonux/restforge/cli/internal/urlresolve"
)

// logResponse records what came back. Every response header is logged
// rather than a chosen few: which header identifies the answering node,
// the cache or the proxy is a property of the deployment, and naming one
// here would be knowledge of a particular server -- mirrors logResponse in
// http.js / http_service.dart. One log call per header rather than one
// with embedded newlines, same reason as the original: a multi-line
// message loses its timestamp and source on every line after the first.
//
// Headers are logged sorted by name rather than in header.Header's
// (unordered) map iteration order, so two runs against the same response
// produce the same log output.
func logResponse(logf func(string), method, target string, status int, header http.Header, elapsed time.Duration) {
	logf(fmt.Sprintf("%s %s -> %d (%dms)", method, urlresolve.Path(target), status, elapsed.Milliseconds()))
	logHeaders(logf, header)
}

// logHeaders logs every response header verbatim, one log call per name.
// Multiple values for one header name are joined with ", ", the same
// representation net/http.Header.Get would return for that name.
func logHeaders(logf func(string), header http.Header) {
	names := make([]string, 0, len(header))
	for name := range header {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		logf(fmt.Sprintf("  %s: %s", name, strings.Join(header[name], ", ")))
	}
}
