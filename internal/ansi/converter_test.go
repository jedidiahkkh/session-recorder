package ansi

import (
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
)

// errReader always returns an error from Read.
type errReader struct{ err error }

func (e errReader) Read(_ []byte) (int, error) { return 0, e.err }

func TestEstimateCols(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "empty input falls back to 80",
			input: "",
			want:  80,
		},
		{
			name:  "single short line falls back to 80",
			input: "hello",
			want:  80,
		},
		{
			name:  "single line longer than 80",
			input: strings.Repeat("x", 81),
			want:  81,
		},
		{
			name:  "multiple lines picks longest when it exceeds 80",
			input: "aaa\n" + strings.Repeat("b", 90) + "\ncc",
			want:  90,
		},
		{
			name:  "trailing newline does not lose last line",
			input: "hello\n",
			want:  80,
		},
		{
			name:  "last line no trailing newline is longer",
			input: "short\n" + strings.Repeat("x", 90),
			want:  90,
		},
		{
			name:  "bare CR resets col but does not count toward max",
			input: "hello\rworld", // both segments are 5 chars
			want:  80,
		},
		{
			name:  "CRLF counts as newline",
			input: "hello\r\nworld", // both segments are 5 chars
			want:  80,
		},
		{
			name:  "CSI sequence is stripped when counting",
			input: "\x1b[1;32mhello\x1b[0m", // 5 visible chars
			want:  80,
		},
		{
			name:  "OSC sequence with BEL terminator is stripped",
			input: "\x1b]0;title\x07hello", // 5 visible chars
			want:  80,
		},
		{
			name:  "OSC sequence with ST terminator is stripped",
			input: "\x1b]0;title\x1b\\hello", // 5 visible chars
			want:  80,
		},
		{
			name:  "multi-byte UTF-8 rune counts as 1 col",
			input: "héllo", // 5 runes, é is 2 bytes
			want:  80,
		},
		{
			name:  "line of 85 UTF-8 runes is counted correctly",
			input: strings.Repeat("é", 85), // 85 runes, each 2 bytes
			want:  85,
		},
		{
			name:  "two-byte ESC sequence is skipped",
			input: "\x1bcABC", // ESC c = reset; 3 visible chars
			want:  80,
		},
		{
			name:  "mixed ANSI and text line longer than 80",
			input: "\x1b[1m" + strings.Repeat("x", 100) + "\x1b[0m",
			want:  100,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := estimateCols([]byte(tc.input))
			if got != tc.want {
				t.Errorf("estimateCols(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestConvert(t *testing.T) {
	t.Run("empty input produces valid HTML with cols 80", func(t *testing.T) {
		var out strings.Builder
		if err := Convert(strings.NewReader(""), &out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		html := out.String()
		if !strings.Contains(html, "<!DOCTYPE html>") {
			t.Error("output missing <!DOCTYPE html>")
		}
		if !strings.Contains(html, "cols: 80") {
			t.Errorf("expected cols: 80 in output, got:\n%s", html)
		}
		// base64 of empty input is the empty string ""
		emptyB64 := base64.StdEncoding.EncodeToString([]byte(""))
		if !strings.Contains(html, "'"+emptyB64+"'") {
			t.Error("expected empty base64 string in output")
		}
	})

	t.Run("input bytes are base64-encoded into output", func(t *testing.T) {
		input := "hello world\n"
		var out strings.Builder
		if err := Convert(strings.NewReader(input), &out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := base64.StdEncoding.EncodeToString([]byte(input))
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected base64 %q in output", want)
		}
	})

	t.Run("cols reflects longest line in input", func(t *testing.T) {
		input := strings.Repeat("x", 120) + "\n"
		var out strings.Builder
		if err := Convert(strings.NewReader(input), &out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out.String(), "cols: 120") {
			t.Error("expected cols: 120 in output")
		}
	})

	t.Run("reader error is propagated", func(t *testing.T) {
		sentinel := errors.New("read failed")
		var out strings.Builder
		err := Convert(errReader{sentinel}, &out)
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if !errors.Is(err, sentinel) {
			t.Errorf("expected sentinel error, got %v", err)
		}
	})

	t.Run("output contains xterm.js script tag", func(t *testing.T) {
		var out strings.Builder
		if err := Convert(strings.NewReader("test"), &out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out.String(), "xterm") {
			t.Error("expected xterm.js reference in output")
		}
	})
}

// Ensure Convert satisfies the io.Reader / io.Writer interface expectations
// at compile time — this is a compile-only check, not a runtime test.
var _ = func() {
	var r io.Reader
	var w io.Writer
	_ = Convert(r, w)
}
