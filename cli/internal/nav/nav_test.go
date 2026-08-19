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
