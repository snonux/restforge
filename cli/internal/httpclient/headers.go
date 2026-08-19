package httpclient

import (
	"fmt"
	"regexp"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/failure"
)

// acceptHeader is the Accept value sent on every request: a Siren-speaking
// server first, falling back to plain JSON for anything that does not
// speak Siren specifically.
const acceptHeader = "application/vnd.siren+json, application/json"

// tokenPattern is RFC 7230's token grammar (section 3.2.6), the set of
// characters a header field-name may contain. Matches
// http_service.dart's/http.js's _isValidHeaderName exactly.
var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+.^_` + "`" + `|~-]+$`)

// isValidHeaderName reports whether name is a valid RFC 7230 token.
func isValidHeaderName(name string) bool {
	return tokenPattern.MatchString(name)
}

// buildHeaders applies the auth header and the ones this app always sends.
// Mirrors setHeaders in http.js / _buildHeaders in http_service.dart.
//
// A header name the user typed can be rejected by the platform's HTTP
// stack (net/http.Header.Set panics on an invalid field name), so the name
// is checked against RFC 7230's token grammar up front rather than letting
// whatever panic or error the underlying client would produce be treated
// as a generic, and wrong, failure.Unreachable -- a bad header name is a
// local configuration problem, not a network one.
func buildHeaders(be backend.Backend, hasBody bool) (map[string]string, error) {
	if !isValidHeaderName(be.AuthHeader) {
		return nil, &failure.Failure{
			Kind:    failure.Config,
			Message: fmt.Sprintf("bad auth header name %q", be.AuthHeader),
		}
	}

	headers := map[string]string{
		be.AuthHeader: be.Secret,
		"Accept":      acceptHeader,
	}
	if hasBody {
		headers["Content-Type"] = "application/x-www-form-urlencoded"
	}
	return headers, nil
}
