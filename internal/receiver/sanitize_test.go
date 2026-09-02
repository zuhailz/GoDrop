package receiver

import "testing"

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain name", in: "readme.md", want: "readme.md"},
		{name: "relative traversal", in: "../../etc/passwd", want: "passwd"},
		{name: "absolute path", in: "/home/user/secret.txt", want: "secret.txt"},
		{name: "bare dot", in: ".", want: ""},
		{name: "bare dotdot", in: "..", want: ""},
		{name: "traversal to root", in: "../..", want: ""},
		{name: "empty", in: "", want: ""},
		{name: "whitespace only", in: "   ", want: ""},
		{name: "windows separators rejected", in: `..\..\x`, want: ""},
		{name: "windows absolute rejected", in: `C:\Users\x\f.txt`, want: ""},
		{name: "nul byte rejected", in: "foo\x00bar", want: ""},
		{name: "padded name trimmed", in: "  report.pdf  ", want: "report.pdf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeFilename(tt.in); got != tt.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
