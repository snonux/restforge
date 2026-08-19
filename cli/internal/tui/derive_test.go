package tui

import (
	"testing"

	"github.com/snonux/restforge/cli/internal/session"
)

// fakeScreenSource is deriveScreen's test double for screenSource -- see
// that interface's doc comment for why deriveScreen takes it rather than
// *session.Session directly.
type fakeScreenSource struct {
	detail   *session.DetailView
	question session.SessionQuestion
}

func (f fakeScreenSource) Detail() *session.DetailView       { return f.detail }
func (f fakeScreenSource) Question() session.SessionQuestion { return f.question }

func TestDeriveScreen(t *testing.T) {
	tests := []struct {
		name string
		src  fakeScreenSource
		base screen
		want screen
	}{
		{
			name: "nothing overlaid falls back to base (Home)",
			src:  fakeScreenSource{},
			base: screenHome,
			want: screenHome,
		},
		{
			name: "nothing overlaid falls back to base (Document)",
			src:  fakeScreenSource{},
			base: screenDocument,
			want: screenDocument,
		},
		{
			name: "detail wins over base",
			src:  fakeScreenSource{detail: &session.DetailView{Heading: "h", Body: "b"}},
			base: screenDocument,
			want: screenDetail,
		},
		{
			name: "confirm question",
			src:  fakeScreenSource{question: session.ConfirmQuestion{Heading: "h", Body: "b"}},
			base: screenDocument,
			want: screenConfirm,
		},
		{
			name: "value question",
			src:  fakeScreenSource{question: session.ValueQuestion{Label: "l"}},
			base: screenDocument,
			want: screenValuePrompt,
		},
		{
			name: "detail wins over a pending question too",
			src: fakeScreenSource{
				detail:   &session.DetailView{Heading: "h", Body: "b"},
				question: session.ConfirmQuestion{Heading: "h", Body: "b"},
			},
			base: screenDocument,
			want: screenDetail,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveScreen(tt.src, tt.base); got != tt.want {
				t.Errorf("deriveScreen() = %v, want %v", got, tt.want)
			}
		})
	}
}
