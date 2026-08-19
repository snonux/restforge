package action

import "strings"

// SafeMethods is RFC 9110's own safe/unsafe division, not a blocklist of
// scary-sounding action names -- an action called "detonate" over GET is
// still safe by this rule, and one called "refresh" over POST still asks.
// Carried over unchanged from SAFE_METHODS in actions.js / safeMethods in
// action_service.dart.
var SafeMethods = map[string]bool{
	"GET":     true,
	"HEAD":    true,
	"OPTIONS": true,
	"TRACE":   true,
}

// IsSafeMethod reports whether method is one of SafeMethods. Case-
// insensitive even though siren.Action.Method is already normalised to
// uppercase, so a caller never has to know that to ask the question
// safely.
func IsSafeMethod(method string) bool {
	return SafeMethods[strings.ToUpper(method)]
}
