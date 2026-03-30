package ansi

import (
	"encoding/base64"
	"fmt"
	"io"
	"regexp"
	"strings"
	"text/template"
)

// clearGroupRe matches a logical screen-clear group: one or more ESC[2J/ESC[3J
// sequences followed by an optional cursor-home sequence.
var clearGroupRe = regexp.MustCompile(
	`(?:\x1b\[(?:2|3)J)+(?:\x1b\[(?:H|1;1H|f|1;1f))?`,
)

// Convert reads raw terminal bytes from r, estimates the terminal width,
// and writes a self-contained HTML document using xterm.js to w.
func Convert(r io.Reader, w io.Writer) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	data = preprocessClears(data)
	cols := estimateCols(data)
	encoded := base64.StdEncoding.EncodeToString(data)

	return htmlTmpl.Execute(w, map[string]any{
		"Cols":    cols,
		"Rows":    50,
		"RawB64":  encoded,
	})
}

// countClears returns the number of logical screen-clear groups in data.
// A group is one or more consecutive ESC[2J/ESC[3J sequences plus an optional
// trailing cursor-home, all treated as a single clear event.
func countClears(data []byte) int {
	return len(clearGroupRe.FindAllIndex(data, -1))
}

// preprocessClears replaces each logical screen-clear group with a visible
// "--- screen cleared (N of M) ---" marker so the viewer can scroll through
// the full session history. A group is one or more consecutive ESC[2J/ESC[3J
// sequences plus an optional trailing cursor-home, all consumed as one event.
func preprocessClears(data []byte) []byte {
	total := countClears(data)
	if total == 0 {
		return data
	}

	n := 0
	return clearGroupRe.ReplaceAllFunc(data, func(_ []byte) []byte {
		n++
		return []byte(fmt.Sprintf("\r\n\x1b[2m--- screen cleared (%d of %d) ---\x1b[0m\r\n", n, total))
	})
}

// estimateCols tracks cursor column position through both printable characters
// and cursor-movement CSI sequences, returning the maximum column reached.
// Falls back to 80 if nothing wider is found.
func estimateCols(data []byte) int {
	max := 80
	col := 0
	i := 0

	// csiNum parses an ASCII decimal string, returning def if empty/invalid.
	csiNum := func(s []byte, def int) int {
		if len(s) == 0 {
			return def
		}
		v := 0
		for _, c := range s {
			if c < '0' || c > '9' {
				return def
			}
			v = v*10 + int(c-'0')
		}
		return v
	}

	updateMax := func() {
		if col > max {
			max = col
		}
	}

	for i < len(data) {
		b := data[i]
		switch {
		case b == 0x1b: // ESC
			i++
			if i >= len(data) {
				break
			}
			switch data[i] {
			case '[': // CSI — collect param bytes then dispatch on final byte
				i++
				paramStart := i
				for i < len(data) && (data[i] < 0x40 || data[i] > 0x7e) {
					i++
				}
				if i >= len(data) {
					break
				}
				final := data[i]
				param := data[paramStart:i]
				i++ // consume final byte

				switch final {
				case 'C': // CUF — cursor forward
					col += csiNum(param, 1)
					updateMax()
				case 'D': // CUB — cursor backward
					col -= csiNum(param, 1)
					if col < 0 {
						col = 0
					}
				case 'G': // CHA — cursor absolute column (1-based)
					col = csiNum(param, 1) - 1
					if col < 0 {
						col = 0
					}
					updateMax()
				case 'H', 'f': // CUP / HVP — cursor position row;col (1-based)
					semi := -1
					for k, c := range param {
						if c == ';' {
							semi = k
							break
						}
					}
					if semi >= 0 {
						col = csiNum(param[semi+1:], 1) - 1
					} else {
						col = 0
					}
					if col < 0 {
						col = 0
					}
					updateMax()
				}
				// all other CSI sequences: no column effect

			case ']': // OSC — skip until BEL or ST (ESC \)
				i++
				for i < len(data) {
					if data[i] == 0x07 {
						i++
						break
					}
					if data[i] == 0x1b && i+1 < len(data) && data[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			default: // two-byte sequence
				i++
			}
		case b == '\n':
			updateMax()
			col = 0
			i++
		case b == '\r':
			col = 0
			i++
		case b >= 0x80: // UTF-8 lead byte — skip continuation bytes, count as 1 col
			col++
			i++
			for i < len(data) && data[i]&0xC0 == 0x80 {
				i++
			}
			updateMax()
		case b >= 0x20: // printable ASCII
			col++
			updateMax()
			i++
		default: // other control bytes
			i++
		}
	}
	updateMax()
	return max
}

var htmlTmpl = template.Must(template.New("html").Parse(strings.TrimSpace(`
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Session recording</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css"/>
<style>
  html, body { margin: 0; padding: 16px; background: #1e1e1e; }
</style>
</head>
<body>
<div id="t"></div>
<script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.js"></script>
<script>
  const term = new Terminal({
    cols: {{.Cols}},
    rows: {{.Rows}},
    scrollback: 50000,
    convertEol: false,
    theme: { background: '#1e1e1e' }
  });
  term.open(document.getElementById('t'));
  const b64 = '{{.RawB64}}';
  const raw = atob(b64);
  const buf = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) buf[i] = raw.charCodeAt(i);
  term.write(buf);
</script>
</body>
</html>
`)))

