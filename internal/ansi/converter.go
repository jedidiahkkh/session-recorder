package ansi

import (
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"text/template"
)

// Convert reads raw terminal bytes from r, estimates the terminal width,
// and writes a self-contained HTML document using xterm.js to w.
func Convert(r io.Reader, w io.Writer) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	cols := estimateCols(data)
	encoded := base64.StdEncoding.EncodeToString(data)

	return htmlTmpl.Execute(w, map[string]any{
		"Cols":    cols,
		"Rows":    50,
		"RawB64":  encoded,
	})
}

// estimateCols strips ANSI escape sequences and returns the length of the
// longest printable line, falling back to 80 if nothing is found.
func estimateCols(data []byte) int {
	max := 80
	col := 0
	i := 0
	for i < len(data) {
		b := data[i]
		switch {
		case b == 0x1b: // ESC
			i++
			if i >= len(data) {
				break
			}
			switch data[i] {
			case '[': // CSI — skip until final byte 0x40–0x7e
				i++
				for i < len(data) && (data[i] < 0x40 || data[i] > 0x7e) {
					i++
				}
				i++ // consume final byte
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
			if col > max {
				max = col
			}
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
		case b >= 0x20: // printable ASCII
			col++
			i++
		default: // other control bytes
			i++
		}
	}
	if col > max {
		max = col
	}
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

// Ensure fmt is used (it's available for future use).
var _ = fmt.Sprintf
