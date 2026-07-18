package app

import "testing"

func TestIsExcludedSynologyPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		candidate    string
		excludePaths []string
		want         bool
	}{
		{name: "nil exclude paths", candidate: "/photo/private/file.jpg", want: false},
		{name: "empty exclude paths", candidate: "/photo/private/file.jpg", excludePaths: []string{}, want: false},
		{name: "exact file", candidate: "/photo/private/file.jpg", excludePaths: []string{"/photo/private/file.jpg"}, want: true},
		{name: "exact directory", candidate: "/photo/private", excludePaths: []string{"/photo/private"}, want: true},
		{name: "descendant file", candidate: "/photo/private/file.jpg", excludePaths: []string{"/photo/private"}, want: true},
		{name: "descendant nested directory", candidate: "/photo/private/album/file.jpg", excludePaths: []string{"/photo/private"}, want: true},
		{name: "safe segment boundary", candidate: "/photo/private2/file.jpg", excludePaths: []string{"/photo/private"}, want: false},
		{name: "basename is not enough", candidate: "/other/private/file.jpg", excludePaths: []string{"/photo/private"}, want: false},
		{name: "repeated separators", candidate: "/photo//private///file.jpg", excludePaths: []string{"/photo/private"}, want: true},
		{name: "dot segment", candidate: "/photo/./private/file.jpg", excludePaths: []string{"/photo/private"}, want: true},
		{name: "resolvable dot dot", candidate: "/photo/public/../private/file.jpg", excludePaths: []string{"/photo/private"}, want: true},
		{name: "trailing slash", candidate: "/photo/private/", excludePaths: []string{"/photo/private/"}, want: true},
		{name: "root excludes absolute candidate", candidate: "/photo/private/file.jpg", excludePaths: []string{"/"}, want: true},
		{name: "root excludes root candidate", candidate: "/", excludePaths: []string{"/"}, want: true},
		{name: "root does not exclude relative candidate", candidate: "photo/private/file.jpg", excludePaths: []string{"/"}, want: false},
		{name: "case mismatch", candidate: "/photo/Private/file.jpg", excludePaths: []string{"/photo/private"}, want: false},
		{name: "backslash is not a separator in candidate", candidate: `/photo/private\\file.jpg`, excludePaths: []string{"/photo/private"}, want: false},
		{name: "backslash is not a separator in exclude path", candidate: "/photo/private/file.jpg", excludePaths: []string{`/photo\\private`}, want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := isExcludedSynologyPath(tt.candidate, tt.excludePaths); got != tt.want {
				t.Fatalf("isExcludedSynologyPath(%q, %#v) = %v, want %v", tt.candidate, tt.excludePaths, got, tt.want)
			}
		})
	}
}
