package httpclient

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/snonux/restforge/cli/internal/failure"
)

// classifyStatus maps an HTTP status onto a failure.Kind, or isFailure=false
// for a 2xx success. 202 is a success: it means the work was accepted and
// is still running, which is a normal answer, not a failure -- mirrors
// classify in http.js / http_service.dart.
func classifyStatus(status int) (kind failure.Kind, isFailure bool) {
	switch {
	case status >= 200 && status < 300:
		return 0, false
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return failure.Auth, true
	case status == http.StatusConflict:
		return failure.Conflict, true
	case status >= 500:
		return failure.Server, true
	default:
		return failure.Client, true
	}
}

// describeFailure mirrors describe in http.js / http_service.dart: the
// server's own wording beats anything this app could invent, because it is
// the only party that knows why it said no.
func describeFailure(kind failure.Kind, status int, entity any) string {
	if message, ok := serverMessage(entity); ok {
		return message
	}
	switch kind {
	case failure.Auth:
		return "auth rejected"
	case failure.Conflict:
		return "state changed"
	default:
		return fmt.Sprintf("HTTP %d", status)
	}
}

// serverMessage digs the server's own wording out of an error envelope --
// mirrors serverMessage in http.js / http_service.dart. Tolerant of
// anything that is not the expected shape, same as every other reader in
// this app: an error body that does not carry a message is not itself an
// error.
func serverMessage(entity any) (string, bool) {
	root, ok := entity.(map[string]any)
	if !ok {
		return "", false
	}
	properties, ok := root["properties"].(map[string]any)
	if !ok {
		return "", false
	}
	message, ok := properties["message"].(string)
	return message, ok
}

// parseBody decodes body into the response entity, or returns nil when it
// was empty or not JSON. An error response is allowed to carry an
// explanation in the same shape, so this runs regardless of status --
// mirrors parseBody in http.js / http_service.dart. A literal JSON "null"
// body also decodes to nil, which is indistinguishable from "not JSON" to
// the caller -- the same behaviour as the Dart port, where jsonDecode
// yields Dart's own null for that input.
func parseBody(body []byte) any {
	if len(body) == 0 {
		return nil
	}
	var entity any
	if err := json.Unmarshal(body, &entity); err != nil {
		return nil
	}
	return entity
}
