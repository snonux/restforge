package live

import "github.com/snonux/restforge/cli/internal/siren"

// ShouldWatch decides whether an action's response is something to follow.
// Either signal is enough: the status code, or the entity's own account.
// Mirrors shouldWatch.
func ShouldWatch(result ActionOutcome) bool {
	return result.Status == 202 || running(result.Entity)
}

// PollTarget picks the resource to watch: a link on the origin document
// whose rel matches a class of the thing the action returned. Returns ""
// when nothing matches -- there is then nothing to follow, and saying so is
// the only honest answer, see the package comment. Mirrors pollTarget.
//
// Ordinary hypermedia rel/class matching: nothing here needs, or may ever
// gain, knowledge of what a particular resource is called.
func PollTarget(originEntity, resultEntity siren.Entity) string {
	for _, className := range resultEntity.Classes {
		if href := originEntity.Follow(className); href != "" {
			return href
		}
	}
	return ""
}
