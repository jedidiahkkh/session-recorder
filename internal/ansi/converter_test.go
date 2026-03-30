package ansi

import (
	"bytes"
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
		// Cursor-movement sequences
		{
			name:  "CUF pushes col past 80",
			input: strings.Repeat("x", 10) + "\x1b[75C|", // 10 + 75 + 1 = 86
			want:  86,
		},
		{
			name:  "CUF with default (no param) advances by 1",
			input: strings.Repeat("x", 80) + "\x1b[C|", // 80 + 1 + 1 = 82
			want:  82,
		},
		{
			name:  "CUB reduces col, does not go below 0",
			input: strings.Repeat("x", 10) + "\x1b[5D" + strings.Repeat("x", 76), // net 81
			want:  81,
		},
		{
			name:  "CHA sets absolute column (1-based)",
			input: "\x1b[100G|", // jump to col 99 (0-based) then print 1 char = 100
			want:  100,
		},
		{
			name:  "CHA with col 1 resets to 0",
			input: strings.Repeat("x", 90) + "\x1b[1G" + "hi", // 90 wide, then reset, 2 chars — max stays 90
			want:  90,
		},
		{
			name:  "CUP row;col sets column (1-based)",
			input: "\x1b[1;105Hx", // col = 105-1 = 104, then 1 char = 105
			want:  105,
		},
		{
			name:  "CUP row only resets col to 0",
			input: strings.Repeat("x", 90) + "\x1b[5H" + "hi", // max stays 90
			want:  90,
		},
		{
			name:  "HVP (ESC[f) sets column like CUP",
			input: "\x1b[1;90fx", // col = 89 then 1 char = 90
			want:  90,
		},
		// Claude-style UI: box-drawing chars separated by large CUF jumps.
		// Mimics lines like: │<ESC>[52C<dim>│<ESC>[1C text <ESC>[NnC│
		// which Claude uses to render its panel borders.
		{
			// │ (1 col) + ESC[52C + │ (1 col) + ESC[1C + text (6 cols) + ESC[34C + │ (1 col) = 96
			name: "claude-style panel line with CUF border jumps",
			input: "\x1b[38;2;215;119;87m" + // color
				"\xe2\x94\x82" + // │ (col 1)
				"\x1b[52C" + // jump to col 53
				"\x1b[2m\xe2\x94\x82\x1b[1C" + // │ col 54, fwd 1 = col 55
				"\x1b[22mhello!" + // 6 chars = col 61
				"\x1b[34C" + // jump fwd 34 = col 95
				"\xe2\x94\x82" + // │ col 96
				"\x1b[39m",
			want: 96,
		},
		{
			// Mimics word-spaced lines: word ESC[1C word ESC[1C ... pushing past 80
			// "Recent" ESC[1C "activity" ESC[1C ... ESC[35C │
			name: "claude-style words with ESC[1C spacing",
			input: "\xe2\x94\x82" + // │ col 1
				"\x1b[22C" + // fwd 22 = col 23
				"\x1b[1mRecent\x1b[1C" + // "Recent"=6 fwd 1 = col 30
				"activity" + // 8 = col 38
				"\x1b[35C" + // fwd 35 = col 73
				"\xe2\x94\x82\x1b[39m", // │ = col 74
			want: 80, // 74 < 80, falls back to 80
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

func TestCountClears(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"no escapes", "hello world", 0},
		{"one 2J", "before\x1b[2Jafter", 1},
		{"one 3J", "before\x1b[3Jafter", 1},
		{"mixed 2J and 3J separated by text", "\x1b[2Jmiddle\x1b[3J", 2},
		{"2J+3J consecutive counts as 1", "\x1b[2J\x1b[3J", 1},
		{"3J+2J consecutive counts as 1", "\x1b[3J\x1b[2J", 1},
		{"three consecutive clears count as 1", "\x1b[2J\x1b[3J\x1b[2J", 1},
		{"two runs separated by text count as 2", "\x1b[2J\x1b[3J" + "x" + "\x1b[2J\x1b[3J", 2},
		{"cursor-home included in group", "\x1b[2J\x1b[3J\x1b[H", 1},
		{"partial erase 0J not counted", "text\x1b[Jmore", 0},
		{"partial erase 1J not counted", "text\x1b[1Jmore", 0},
		{"color sequences not counted", "\x1b[1;32mhello\x1b[0m", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := countClears([]byte(tc.input))
			if got != tc.want {
				t.Errorf("countClears(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestPreprocessClears(t *testing.T) {
	clear2J := "\x1b[2J"
	clear3J := "\x1b[3J"
	cursorHome := "\x1b[H"
	cursorHome11 := "\x1b[1;1H"

	tests := []struct {
		name        string
		input       string
		mustContain []string
		mustAbsent  []string
	}{
		{
			name:        "no clears — passthrough",
			input:       "hello\nworld",
			mustContain: []string{"hello", "world"},
			mustAbsent:  []string{"screen cleared"},
		},
		{
			name:        "2J replaced with marker",
			input:       "before" + clear2J + "after",
			mustContain: []string{"before", "screen cleared", "1 of 1", "after"},
			mustAbsent:  []string{clear2J},
		},
		{
			name:        "3J replaced with marker",
			input:       "before" + clear3J + "after",
			mustContain: []string{"before", "screen cleared", "1 of 1", "after"},
			mustAbsent:  []string{clear3J},
		},
		{
			name:        "partial erase 0J left intact",
			input:       "text\x1b[Jmore",
			mustContain: []string{"\x1b[J"},
			mustAbsent:  []string{"screen cleared"},
		},
		{
			name:        "partial erase 1J left intact",
			input:       "text\x1b[1Jmore",
			mustContain: []string{"\x1b[1J"},
			mustAbsent:  []string{"screen cleared"},
		},
		{
			name:        "cursor-home after clear is stripped",
			input:       "text" + clear2J + cursorHome + "after",
			mustContain: []string{"screen cleared", "after"},
			mustAbsent:  []string{clear2J, cursorHome},
		},
		{
			name:        "cursor-home 1;1H after clear is stripped",
			input:       "text" + clear2J + cursorHome11 + "after",
			mustContain: []string{"screen cleared", "after"},
			mustAbsent:  []string{clear2J, cursorHome11},
		},
		{
			name:        "cursor-home not stripped if not after clear",
			input:       cursorHome + "text",
			mustContain: []string{cursorHome, "text"},
			mustAbsent:  []string{"screen cleared"},
		},
		{
			name:        "same-type consecutive clears treated as one",
			input:       clear2J + clear2J,
			mustContain: []string{"screen cleared", "1 of 1"},
			mustAbsent:  []string{clear2J},
		},
		{
			name:        "2J+3J pair produces single marker",
			input:       clear2J + clear3J,
			mustContain: []string{"screen cleared", "1 of 1"},
			mustAbsent:  []string{clear2J, clear3J},
		},
		{
			name:        "3J+2J pair produces single marker",
			input:       clear3J + clear2J,
			mustContain: []string{"screen cleared", "1 of 1"},
			mustAbsent:  []string{clear3J, clear2J},
		},
		{
			name:        "two pairs separated by text produce two markers",
			input:       clear2J + clear3J + "x" + clear2J + clear3J,
			mustContain: []string{"1 of 2", "2 of 2"},
		},
		{
			name:        "cursor-home after pair is absorbed into group",
			input:       clear2J + clear3J + cursorHome + "after",
			mustContain: []string{"screen cleared", "after"},
			mustAbsent:  []string{cursorHome},
		},
		{
			name:        "1;1H cursor-home after pair is absorbed",
			input:       clear2J + clear3J + cursorHome11 + "after",
			mustContain: []string{"screen cleared", "after"},
			mustAbsent:  []string{cursorHome11},
		},
		{
			name:        "clear at start of input",
			input:       clear2J + "content",
			mustContain: []string{"screen cleared", "1 of 1", "content"},
			mustAbsent:  []string{clear2J},
		},
		{
			name:        "other CSI sequences unchanged",
			input:       "\x1b[1;32mhello\x1b[0m",
			mustContain: []string{"\x1b[1;32m", "hello", "\x1b[0m"},
			mustAbsent:  []string{"screen cleared"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := preprocessClears([]byte(tc.input))
			for _, want := range tc.mustContain {
				if !bytes.Contains(got, []byte(want)) {
					t.Errorf("output missing %q\nfull output: %q", want, got)
				}
			}
			for _, absent := range tc.mustAbsent {
				if bytes.Contains(got, []byte(absent)) {
					t.Errorf("output should not contain %q\nfull output: %q", absent, got)
				}
			}
		})
	}
}

// Ensure Convert satisfies the io.Reader / io.Writer interface expectations
// at compile time — this is a compile-only check, not a runtime test.
var _ = func() {
	var r io.Reader
	var w io.Writer
	_ = Convert(r, w)
}
