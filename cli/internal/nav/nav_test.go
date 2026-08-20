// Ported from flutter/test/services/nav_service_test.dart -- the cases that
// belong to this package's scope, see nav_service_test.dart's own header
// for what does not carry over (the backend picker, and the watch-only
// overlay/seq protocol) and doc.go's package comment for what this package
// itself does not own.
//
// Deliberately NOT ported: the two "dispose during an in-flight fetch"
// cases. They exist to pin ChangeNotifier's "used after dispose" assert,
// which has no equivalent here -- this package adds no observer pattern at
// all (see doc.go), so there is nothing analogous to dispose to race
// against.
//
// The two "state transitions" cases need a fetch to still be in flight
// while the test inspects Nav -- Dart gets that for free from
// async/await's Completer-gated MockClient. This package's methods are
// synchronous, so the same effect is reached by running the method under
// test on its own goroutine and gating fakeClient.Get on a channel; the
// gate's close/receive and a "started" channel closed right as Get is
// entered establish the happens-before edges a data-race-free read of Nav's
// fields (from the test's goroutine) needs while the fetch is still
// in-flight on the other one.
package nav_test

import (
	"testing"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/failure"
	"github.com/snonux/restforge/cli/internal/httpclient"
	"github.com/snonux/restforge/cli/internal/nav"
	"github.com/snonux/restforge/cli/internal/render"
	"github.com/snonux/restforge/cli/internal/urlresolve"
)

const base = "https://pantry.example/"

func testBackend() backend.Backend {
	return backend.Backend{Name: "pantry", BaseURL: base, Secret: "open-sesame"}
}

// rootDoc and shelvesDoc are the same fixture documents
// nav_service_test.dart uses, as the map[string]any shape
// siren.EntityFromJSON accepts (the shape encoding/json would hand back for
// this JSON) -- a shelf embedded in the root, and a link to a second
// document ("shelves") that is not.
func rootDoc() map[string]any {
	return map[string]any{
		"class":      []any{"pantry"},
		"title":      "The pantry",
		"properties": map[string]any{"apiVersion": float64(1), "kettle": "cold"},
		"entities": []any{
			map[string]any{
				"class":      []any{"shelf"},
				"title":      "Top shelf",
				"properties": map[string]any{"name": "top", "jars": float64(4)},
			},
		},
		"links": []any{
			map[string]any{"rel": []any{"self"}, "href": "/"},
			map[string]any{"rel": []any{"shelves"}, "href": "/shelves"},
		},
		"actions": []any{},
	}
}

func shelvesDoc() map[string]any {
	return map[string]any{
		"class":      []any{"shelf-list"},
		"title":      "Shelves",
		"properties": map[string]any{"count": float64(1)},
		"links": []any{
			map[string]any{"rel": []any{"self"}, "href": "/shelves"},
		},
	}
}

// fakeClient is Nav's test double for its httpGetter seam: a route table
// keyed by the exact resolved URL, a log of what was requested, and a
// couple of hand-toggled switches (unreachable, block) for the failure and
// mid-flight cases those exist to cover -- mirrors
// nav_service_test.dart's Env/FakeXHR, adapted to fake at the Get()
// boundary directly since that is where Nav's dependency is injected (see
// nav's httpGetter), rather than at the underlying HTTP transport the way
// httpclient_test.go's httptest servers do -- that package is testing the
// transport itself, this one is not.
type fakeClient struct {
	routes      map[string]any
	requested   []string
	unreachable bool

	// block, when set, is called with the resolved target as Get is
	// entered, before routes is consulted -- the hook the "state
	// transitions" tests use to hold a fetch in flight.
	block func(target string)
}

func newFakeClient() *fakeClient {
	return &fakeClient{routes: map[string]any{
		base:             rootDoc(),
		base + "shelves": shelvesDoc(),
	}}
}

func (f *fakeClient) Get(be backend.Backend, href string) (httpclient.HTTPResponse, error) {
	target := urlresolve.Resolve(href, be.BaseURL)
	f.requested = append(f.requested, "GET "+target)
	if f.block != nil {
		f.block(target)
	}
	if f.unreachable {
		return httpclient.HTTPResponse{}, &failure.Failure{Kind: failure.Unreachable, Message: "no answer from " + target}
	}
	body, ok := f.routes[target]
	if !ok {
		return httpclient.HTTPResponse{}, &failure.Failure{Kind: failure.Client, Status: 404, Message: "no such thing here"}
	}
	return httpclient.HTTPResponse{Status: 200, URL: target, Entity: body}, nil
}

func (f *fakeClient) reset() { f.requested = nil }

// rowNamed finds the row with this label, failing the test if there is
// none -- mirrors nav_service_test.dart's rowNamed helper.
func rowNamed(t *testing.T, doc *render.RenderedDocument, label string) render.Row {
	t.Helper()
	for _, row := range doc.Rows {
		if row.Label == label {
			return row
		}
	}
	t.Fatalf("no row %q in %v", label, doc.Rows)
	return render.Row{}
}

// --- opening a backend ------------------------------------------------

func TestOpenRootFetchesBaseURLAndShowsDocument(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)

	n.OpenRoot(testBackend())

	if got, want := fake.requested, []string{"GET " + base}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("requested = %v, want %v", got, want)
	}
	if got := n.Document(); got == nil || got.Title != "The pantry" {
		t.Errorf("Document().Title = %v, want %q", got, "The pantry")
	}
	if n.State() != nav.StateOK {
		t.Errorf("State() = %v, want StateOK", n.State())
	}
	if n.Backend() != testBackend() {
		t.Errorf("Backend() = %+v, want %+v", n.Backend(), testBackend())
	}
}

func TestLinkRowCanBeFollowed(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)
	n.OpenRoot(testBackend())
	target, ok := rowNamed(t, n.Document(), "shelves").Target.(render.FetchTarget)
	if !ok {
		t.Fatalf("shelves row target is not a FetchTarget")
	}
	fake.reset()

	n.Fetch(target.Href, "shelves")

	if got, want := fake.requested, []string{"GET " + base + "shelves"}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("requested = %v, want %v", got, want)
	}
	if got := n.Document(); got == nil || got.Title != "Shelves" {
		t.Errorf("Document().Title = %v, want %q", got, "Shelves")
	}
}

func TestUnsupportedAPIVersionRootIsNotPushed(t *testing.T) {
	fake := newFakeClient()
	fake.routes[base] = map[string]any{
		"class":      []any{"pantry"},
		"properties": map[string]any{"apiVersion": float64(99)},
		"links":      []any{map[string]any{"rel": []any{"self"}, "href": "/"}},
	}
	n := nav.New(fake)

	n.OpenRoot(testBackend())

	if n.State() != nav.StateError {
		t.Errorf("State() = %v, want StateError", n.State())
	}
	if n.Failure() == nil || n.Failure().Kind != failure.Client {
		t.Errorf("Failure() = %v, want kind Client", n.Failure())
	}
	if n.Document() != nil {
		t.Errorf("Document() = %v, want nil: nothing was pushed", n.Document())
	}
}

// --- startRel -----------------------------------------------------------

func TestStartRelIsFollowedAfterRootPushingSecondDocument(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)
	be := testBackend()
	be.StartRel = "shelves"

	n.OpenRoot(be)

	want := []string{"GET " + base, "GET " + base + "shelves"}
	if got := fake.requested; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("requested = %v, want %v", got, want)
	}
	if got := n.Document(); got == nil || got.Title != "Shelves" {
		t.Errorf("Document().Title = %v, want %q", got, "Shelves")
	}
	if !n.CanGoBack() {
		t.Error("CanGoBack() = false, want true: the root is still underneath it on the stack")
	}
}

func TestStartRelNotOfferedLeavesRootOpen(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)
	be := testBackend()
	be.StartRel = "no-such-rel"

	n.OpenRoot(be)

	if got, want := fake.requested, []string{"GET " + base}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("requested = %v, want %v", got, want)
	}
	if got := n.Document(); got == nil || got.Title != "The pantry" {
		t.Errorf("Document().Title = %v, want %q", got, "The pantry")
	}
	if n.State() != nav.StateOK {
		t.Errorf("State() = %v, want StateOK", n.State())
	}
}

// --- an embedded entity ---------------------------------------------------

func TestEmbeddedEntityOpensWithoutRequestAndBackReturns(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)
	n.OpenRoot(testBackend())
	target, ok := rowNamed(t, n.Document(), "Top shelf").Target.(render.EmbeddedTarget)
	if !ok {
		t.Fatalf("Top shelf row target is not an EmbeddedTarget")
	}
	fake.reset()

	n.OpenEmbedded(target.Index)
	if len(fake.requested) != 0 {
		t.Errorf("requested = %v, want empty", fake.requested)
	}
	if got := n.Document(); got == nil || got.Title != "Top shelf" {
		t.Errorf("Document().Title = %v, want %q", got, "Top shelf")
	}
	if !n.CanGoBack() {
		t.Error("CanGoBack() = false, want true")
	}

	n.Back()
	if got := n.Document(); got == nil || got.Title != "The pantry" {
		t.Errorf("Document().Title = %v, want %q", got, "The pantry")
	}
	if n.CanGoBack() {
		t.Error("CanGoBack() = true, want false")
	}
}

func TestEmbeddedDocumentCannotBeRefreshedRerendersInstead(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)
	n.OpenRoot(testBackend())
	target := rowNamed(t, n.Document(), "Top shelf").Target.(render.EmbeddedTarget)
	n.OpenEmbedded(target.Index)
	fake.reset()

	n.Refresh()

	if len(fake.requested) != 0 {
		t.Errorf("requested = %v, want empty: an embedded document has no address to refresh from", fake.requested)
	}
	if got := n.Document(); got == nil || got.Title != "Top shelf" {
		t.Errorf("Document().Title = %v, want %q", got, "Top shelf")
	}
}

// --- back / refresh -------------------------------------------------------

func TestBackAtRootIsNoOp(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)
	n.OpenRoot(testBackend())

	n.Back()

	if got := n.Document(); got == nil || got.Title != "The pantry" {
		t.Errorf("Document().Title = %v, want %q", got, "The pantry")
	}
}

func TestRefreshRefetchesCurrentDocument(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)
	n.OpenRoot(testBackend())
	fake.reset()

	n.Refresh()

	if got, want := fake.requested, []string{"GET " + base}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("requested = %v, want %v", got, want)
	}
	if n.State() != nav.StateOK {
		t.Errorf("State() = %v, want StateOK", n.State())
	}
}

// --- opening a different backend -------------------------------------------

func TestOpeningDifferentBackendDiscardsOldStack(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)
	n.OpenRoot(testBackend())
	target := rowNamed(t, n.Document(), "shelves").Target.(render.FetchTarget)
	n.Fetch(target.Href, "shelves")
	if !n.CanGoBack() {
		t.Fatal("CanGoBack() = false, want true")
	}

	const otherBase = "https://other.example/"
	fake.routes[otherBase] = map[string]any{
		"title": "Other root",
		"links": []any{map[string]any{"rel": []any{"self"}, "href": "/"}},
	}
	other := backend.Backend{Name: "other", BaseURL: otherBase, Secret: "x"}
	fake.reset()

	n.OpenRoot(other)

	if n.CanGoBack() {
		t.Error("CanGoBack() = true, want false")
	}
	if got := n.Document(); got == nil || got.Title != "Other root" {
		t.Errorf("Document().Title = %v, want %q", got, "Other root")
	}
	if n.Backend() != other {
		t.Errorf("Backend() = %+v, want %+v", n.Backend(), other)
	}
}

// --- adopt ------------------------------------------------------------

// adopt exists for exactly one caller outside this package -- a saved
// shortcut's jump straight into a backend -- see Nav.Adopt's own doc
// comment for why it must not pay for the root fetch OpenRoot always makes.

func TestAdoptSwitchesBackendWithoutFetchingDiscardingOldStack(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)
	n.OpenRoot(testBackend())
	target := rowNamed(t, n.Document(), "shelves").Target.(render.FetchTarget)
	n.Fetch(target.Href, "shelves")
	if !n.CanGoBack() {
		t.Fatal("CanGoBack() = false, want true")
	}
	fake.reset()

	other := backend.Backend{Name: "other", BaseURL: "https://other.example/", Secret: "x"}
	n.Adopt(other)

	if len(fake.requested) != 0 {
		t.Errorf("requested = %v, want empty: adopt fetches nothing", fake.requested)
	}
	if n.Backend() != other {
		t.Errorf("Backend() = %+v, want %+v", n.Backend(), other)
	}
	if n.Document() != nil {
		t.Errorf("Document() = %v, want nil: the old stack is gone", n.Document())
	}
	if n.CanGoBack() {
		t.Error("CanGoBack() = true, want false")
	}
	if n.State() != nav.StateOK {
		t.Errorf("State() = %v, want StateOK", n.State())
	}
}

func TestFetchAfterAdoptPushesOntoFreshOneFrameStack(t *testing.T) {
	fake := newFakeClient()
	other := backend.Backend{Name: "other", BaseURL: "https://other.example/", Secret: "x"}
	fake.routes["https://other.example/somewhere"] = map[string]any{
		"title": "Elsewhere",
		"links": []any{map[string]any{"rel": []any{"self"}, "href": "/somewhere"}},
	}
	n := nav.New(fake)

	n.Adopt(other)
	n.Fetch("https://other.example/somewhere", "x")

	if got := n.Document(); got == nil || got.Title != "Elsewhere" {
		t.Errorf("Document().Title = %v, want %q", got, "Elsewhere")
	}
	if n.CanGoBack() {
		t.Error("CanGoBack() = true, want false: straight to the destination, nothing was walked to get here")
	}
}

// --- a failed request -------------------------------------------------

// This is the invariant docs/DESIGN.md calls "A failed request is not an
// answer": the document already on screen must survive exactly as it was,
// with the reason layered on top of it, never replaced by an empty one.

func TestFailedRequestNeverReplacesDocumentWithEmptyOne(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)
	n.OpenRoot(testBackend())
	before := n.Document()
	beforeLabels := labelsOf(before)
	fake.routes = map[string]any{}
	fake.reset()

	n.Refresh()

	after := n.Document()
	if got := labelsOf(after); !equalStrings(got, beforeLabels) {
		t.Errorf("row labels changed: got %v, want %v (the last good document must be untouched by a failure)", got, beforeLabels)
	}
	if after.Title != before.Title {
		t.Errorf("Title = %q, want %q", after.Title, before.Title)
	}
	if n.State() != nav.StateError {
		t.Errorf("State() = %v, want StateError", n.State())
	}
	if n.Failure() == nil || n.Failure().Message != "no such thing here" {
		t.Errorf("Failure() = %v, want message %q", n.Failure(), "no such thing here")
	}
}

func TestConnectionFailureMapsToUnreachableAndKeepsDocument(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)
	n.OpenRoot(testBackend())
	beforeTitle := n.Document().Title
	fake.unreachable = true
	fake.reset()

	n.Refresh()

	if n.State() != nav.StateUnreachable {
		t.Errorf("State() = %v, want StateUnreachable", n.State())
	}
	if got := n.Document(); got == nil || got.Title != beforeTitle {
		t.Errorf("Document().Title = %v, want %q", got, beforeTitle)
	}
}

// --- state transitions -------------------------------------------------

func TestFetchGoesThroughLoadingBeforeOk(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)

	started := make(chan struct{})
	gate := make(chan struct{})
	fake.block = func(string) {
		close(started)
		<-gate
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		n.OpenRoot(testBackend())
	}()

	<-started // happens-before: the Loading write above precedes this read
	if got := n.State(); got != nav.StateLoading {
		t.Errorf("State() while in flight = %v, want StateLoading", got)
	}

	close(gate)
	<-done // happens-before: OpenRoot's final writes precede reads below

	if got := n.State(); got != nav.StateOK {
		t.Errorf("State() after landing = %v, want StateOK", got)
	}
}

func TestDocumentUntouchedWhileFetchInFlight(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)
	n.OpenRoot(testBackend())
	beforeTitle := n.Document().Title

	started := make(chan struct{})
	gate := make(chan struct{})
	fake.block = func(string) {
		close(started)
		<-gate
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		n.Refresh()
	}()

	<-started
	if got := n.State(); got != nav.StateLoading {
		t.Errorf("State() while in flight = %v, want StateLoading", got)
	}
	if got := n.Document(); got == nil || got.Title != beforeTitle {
		t.Errorf("Document().Title while in flight = %v, want %q: a loading fetch must not blank the screen while it runs", got, beforeTitle)
	}

	close(gate)
	<-done

	if got := n.State(); got != nav.StateOK {
		t.Errorf("State() after landing = %v, want StateOK", got)
	}
}

// --- staleness: a superseded fetch must not win ---------------------------
//
// i31: nav's async fetches (Fetch/Refresh/OpenRoot, all funneled through
// fetch()/fetchAndApply()) used to have no staleness guard, unlike
// internal/live's isCurrent/stopIfCurrent pattern. These tests reproduce
// the bug report's concrete scenarios -- (1) the user presses Back while an
// earlier Fetch/Activate is still in flight, on both the success and
// failure branch, (2) two concurrent OpenRoot calls race for two different
// backends, (3) the same race against Adopt and against OpenEmbedded, the
// two other calls the fix also guards -- plus (4) a follow-up scenario the
// fix's own code review surfaced: OpenRoot's chained followStart fetch must
// keep targeting the backend it was opened for, not whatever backend is
// current by the time its own GET goes out. All of them use fakeClient's
// block hook, the same gating mechanism the "state transitions" tests above
// already use, to make the interleaving deterministic instead of relying on
// real wall-clock timing.

// TestBackDuringInFlightFetchIsNotOverwrittenByStaleFetch reproduces the
// bug's steps 1-3: a Fetch is started (mirroring activateDocumentSelection's
// sessionCmd), the user's Back runs synchronously while it is still in
// flight (mirroring handleBack, which is deliberately not wrapped in a
// cmd), and only then does the abandoned Fetch's response land. Before the
// generation guard, fetch()'s completion code appended a frame onto
// whatever n.stack was at that moment -- the post-Back stack -- silently
// undoing the Back. After the guard, the stale response must be discarded.
func TestBackDuringInFlightFetchIsNotOverwrittenByStaleFetch(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)
	n.OpenRoot(testBackend())
	target := rowNamed(t, n.Document(), "shelves").Target.(render.FetchTarget)
	n.Fetch(target.Href, "shelves") // stack: [root, shelves]
	if !n.CanGoBack() {
		t.Fatal("CanGoBack() = false, want true")
	}

	// A third document the in-flight fetch below will (attempt to) land as
	// a push on top of "shelves".
	fake.routes[base+"deep"] = map[string]any{
		"title": "Deep",
		"links": []any{map[string]any{"rel": []any{"self"}, "href": "/deep"}},
	}

	started := make(chan struct{})
	gate := make(chan struct{})
	fake.block = func(string) {
		close(started)
		<-gate
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		n.Fetch(base+"deep", "deep")
	}()
	<-started // "deep" fetch is in flight, blocked before it returns

	// The user's Back runs synchronously while that fetch is still in
	// flight -- exactly model.go's handleBack path.
	n.Back()
	if got := n.Document(); got == nil || got.Title != "The pantry" {
		t.Fatalf("Document().Title after Back = %v, want %q", got, "The pantry")
	}
	if n.CanGoBack() {
		t.Fatal("CanGoBack() = true after Back to root, want false")
	}

	close(gate) // let the stale "deep" fetch finally land
	<-done

	if got := n.Document(); got == nil || got.Title != "The pantry" {
		t.Errorf("Document().Title after stale fetch landed = %v, want %q: Back must win, not the abandoned fetch", got, "The pantry")
	}
	if n.CanGoBack() {
		t.Error("CanGoBack() = true, want false: the stale fetch must not resurrect a frame Back already popped")
	}
	if n.State() != nav.StateOK {
		t.Errorf("State() after stale fetch landed = %v, want StateOK", n.State())
	}
}

// TestBackDuringInFlightFetchDiscardsStaleFailureToo is the failure-branch
// sibling of the test above: the fetch that is in flight when Back runs
// ends in an error (a 404, since its route is never registered), not a
// success. applyFailureIfCurrent's guard (fetch.go) must discard that
// failure exactly as fetchAndApply's success-path guard discards a
// success -- otherwise a Back could still be silently overridden, just
// with an error screen instead of a resurrected frame.
func TestBackDuringInFlightFetchDiscardsStaleFailureToo(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)
	n.OpenRoot(testBackend())
	target := rowNamed(t, n.Document(), "shelves").Target.(render.FetchTarget)
	n.Fetch(target.Href, "shelves") // stack: [root, shelves]

	started := make(chan struct{})
	gate := make(chan struct{})
	fake.block = func(string) {
		close(started)
		<-gate
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		n.Fetch(base+"nonexistent", "nonexistent") // no such route: 404
	}()
	<-started // the failing fetch is in flight, blocked before it returns

	n.Back() // stack: [root]
	if got := n.Document(); got == nil || got.Title != "The pantry" {
		t.Fatalf("Document().Title after Back = %v, want %q", got, "The pantry")
	}

	close(gate) // let the stale, failing fetch finally land
	<-done

	if n.State() != nav.StateOK {
		t.Errorf("State() after stale failing fetch landed = %v, want StateOK: the abandoned fetch's failure must not override Back", n.State())
	}
	if n.Failure() != nil {
		t.Errorf("Failure() after stale failing fetch landed = %v, want nil", n.Failure())
	}
	if got := n.Document(); got == nil || got.Title != "The pantry" {
		t.Errorf("Document().Title after stale failing fetch landed = %v, want %q", got, "The pantry")
	}
}

// TestAdoptDuringInFlightFetchDiscardsStaleFetch covers Adopt, the third of
// the four generation-bumping calls the fix touches besides fetch() itself
// (OpenRoot and Back are covered above; OpenEmbedded is covered below). A
// fetch started before Adopt switches backends must not land afterwards and
// push its frame onto the new backend's (empty) stack.
func TestAdoptDuringInFlightFetchDiscardsStaleFetch(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)
	n.OpenRoot(testBackend())

	fake.routes[base+"deep"] = map[string]any{
		"title": "Deep",
		"links": []any{map[string]any{"rel": []any{"self"}, "href": "/deep"}},
	}

	started := make(chan struct{})
	gate := make(chan struct{})
	fake.block = func(string) {
		close(started)
		<-gate
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		n.Fetch(base+"deep", "deep")
	}()
	<-started // "deep" fetch is in flight, blocked before it returns

	other := backend.Backend{Name: "other", BaseURL: "https://other.example/", Secret: "x"}
	n.Adopt(other)
	if n.Document() != nil {
		t.Fatalf("Document() after Adopt = %v, want nil: Adopt starts with an empty stack", n.Document())
	}

	close(gate) // let the stale "deep" fetch finally land
	<-done

	if got := n.Backend(); got != other {
		t.Errorf("Backend() after stale fetch landed = %+v, want %+v", got, other)
	}
	if n.Document() != nil {
		t.Errorf("Document() after stale fetch landed = %v, want nil: the abandoned fetch (for the old backend) must not push onto other's fresh stack", n.Document())
	}
	if n.CanGoBack() {
		t.Error("CanGoBack() = true, want false")
	}
}

// TestOpenEmbeddedDuringInFlightFetchDiscardsStaleFetch covers OpenEmbedded,
// the fourth generation-bumping call: it pushes synchronously (no I/O of
// its own), but a fetch already in flight when it runs must still not land
// afterwards and shove its response on top of the frame OpenEmbedded just
// opened.
func TestOpenEmbeddedDuringInFlightFetchDiscardsStaleFetch(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)
	n.OpenRoot(testBackend())
	embedded := rowNamed(t, n.Document(), "Top shelf").Target.(render.EmbeddedTarget)

	fake.routes[base+"deep"] = map[string]any{
		"title": "Deep",
		"links": []any{map[string]any{"rel": []any{"self"}, "href": "/deep"}},
	}

	started := make(chan struct{})
	gate := make(chan struct{})
	fake.block = func(string) {
		close(started)
		<-gate
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		n.Fetch(base+"deep", "deep") // would push onto [root], making [root, deep]
	}()
	<-started // "deep" fetch is in flight, blocked before it returns

	n.OpenEmbedded(embedded.Index) // pushes onto the stack as it stands now: [root, "Top shelf"]
	if got := n.Document(); got == nil || got.Title != "Top shelf" {
		t.Fatalf("Document().Title after OpenEmbedded = %v, want %q", got, "Top shelf")
	}

	close(gate) // let the stale "deep" fetch finally land
	<-done

	if got := n.Document(); got == nil || got.Title != "Top shelf" {
		t.Errorf("Document().Title after stale fetch landed = %v, want %q: the stale fetch must not overwrite what OpenEmbedded opened", got, "Top shelf")
	}
}

// TestFollowStartAfterSupersedingOpenRootUsesOriginalBackend targets the gap
// the code review for i31 found: followStart's own chained fetch must keep
// talking to the backend its OpenRoot call was opened for, never whatever
// backend happens to be n.current by the time followStart's own GET goes
// out. It proves this two ways: (1) the request is resolved and sent
// against be's own server (base+"shelves"), never other's, and (2) other's
// state (already current by the time be's followStart fetch lands) survives
// untouched. Before followStart threaded OpenRoot's gen through explicitly,
// a version of this bug (reading n.current fresh inside a generic fetch())
// could have sent be's StartRel href to other's server instead.
func TestFollowStartAfterSupersedingOpenRootUsesOriginalBackend(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)
	be := testBackend()
	be.StartRel = "shelves"

	other := backend.Backend{Name: "other", BaseURL: "https://other.example/", Secret: "x"}
	fake.routes[other.BaseURL] = map[string]any{
		"title": "Other root",
		"links": []any{map[string]any{"rel": []any{"self"}, "href": "/"}},
	}

	started := make(chan struct{})
	gate := make(chan struct{})
	fake.block = func(target string) {
		if target != base+"shelves" {
			return // only gate followStart's own fetch, not either root
		}
		close(started)
		<-gate
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		n.OpenRoot(be) // root lands, then followStart's "shelves" fetch blocks
	}()
	<-started

	// A second, different backend opens fully while be's followStart fetch
	// is still in flight.
	n.OpenRoot(other)
	if got := n.Backend(); got != other {
		t.Fatalf("Backend() after second OpenRoot = %+v, want %+v", got, other)
	}

	close(gate) // let be's stale followStart fetch finally land
	<-done

	if got := n.Backend(); got != other {
		t.Errorf("Backend() after stale followStart fetch landed = %+v, want %+v", got, other)
	}
	if got := n.Document(); got == nil || got.Title != "Other root" {
		t.Errorf("Document().Title after stale followStart fetch landed = %v, want %q", got, "Other root")
	}

	found := false
	for _, r := range fake.requested {
		if r == "GET "+other.BaseURL+"shelves" {
			t.Fatalf("followStart's fetch was requested against other's server (%s): it must always target the backend it was opened for, not whatever is current when its GET goes out", r)
		}
		if r == "GET "+base+"shelves" {
			found = true
		}
	}
	if !found {
		t.Errorf("requested = %v, want a GET for %s", fake.requested, base+"shelves")
	}
}

// TestSecondOpenRootForDifferentBackendWinsOverSlowerFirst reproduces the
// bug's "worse case": two concurrent OpenRoot calls for two different
// backends (mirroring openBackendCmd firing for two Home rows before the
// first lands). Before the generation guard, whichever GET happened to
// land last won, regardless of which OpenRoot the user actually intended
// to be looking at last -- a slow first backend's response could land
// after a second, different backend's OpenRoot had already reset
// n.current, pushing the first backend's document under the second
// backend's name. After the guard, the later call always wins.
func TestSecondOpenRootForDifferentBackendWinsOverSlowerFirst(t *testing.T) {
	fake := newFakeClient()
	n := nav.New(fake)

	other := backend.Backend{Name: "other", BaseURL: "https://other.example/", Secret: "x"}
	fake.routes[other.BaseURL] = map[string]any{
		"title": "Other root",
		"links": []any{map[string]any{"rel": []any{"self"}, "href": "/"}},
	}

	started := make(chan struct{})
	gate := make(chan struct{})
	fake.block = func(target string) {
		if target != base {
			return // only the first backend's root fetch is gated
		}
		close(started)
		<-gate
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		n.OpenRoot(testBackend()) // slow: backend "pantry"
	}()
	<-started // pantry's GET is in flight, blocked

	n.OpenRoot(other) // "other" opens and lands fully before pantry's GET returns

	if got := n.Backend(); got != other {
		t.Fatalf("Backend() after second OpenRoot = %+v, want %+v", got, other)
	}
	if got := n.Document(); got == nil || got.Title != "Other root" {
		t.Fatalf("Document().Title after second OpenRoot = %v, want %q", got, "Other root")
	}

	close(gate) // let pantry's stale GET finally return
	<-done

	if got := n.Backend(); got != other {
		t.Errorf("Backend() after stale OpenRoot landed = %+v, want %+v: the abandoned OpenRoot for pantry must not resurrect it as current", got, other)
	}
	if got := n.Document(); got == nil || got.Title != "Other root" {
		t.Errorf("Document().Title after stale OpenRoot landed = %v, want %q: pantry's late response must not overwrite other's document", got, "Other root")
	}
	if n.State() != nav.StateOK {
		t.Errorf("State() after stale OpenRoot landed = %v, want StateOK", n.State())
	}
}

// --- small helpers -------------------------------------------------------

func labelsOf(doc *render.RenderedDocument) []string {
	labels := make([]string, len(doc.Rows))
	for i, row := range doc.Rows {
		labels[i] = row.Label
	}
	return labels
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
