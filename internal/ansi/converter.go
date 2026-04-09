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

// SplitFrames splits raw ANSI data into frames on screen-clear boundaries.
// Clear sequences are consumed and not included in any frame.
// Returns at least one frame (may be empty if input is empty or starts/ends with a clear).
func SplitFrames(data []byte) [][]byte {
	indices := clearGroupRe.FindAllIndex(data, -1)
	if len(indices) == 0 {
		return [][]byte{data}
	}
	frames := make([][]byte, 0, len(indices)+1)
	prev := 0
	for _, idx := range indices {
		frames = append(frames, data[prev:idx[0]])
		prev = idx[1]
	}
	return append(frames, data[prev:])
}

// joinFrames concatenates frames with a visible "--- screen cleared (N of M) ---"
// ANSI marker inserted between each pair.
func joinFrames(frames [][]byte) []byte {
	if len(frames) == 0 {
		return nil
	}
	total := len(frames) - 1
	var out []byte
	for i, frame := range frames {
		out = append(out, frame...)
		if i < total {
			out = append(out, []byte(fmt.Sprintf(
				"\r\n\x1b[2m--- screen cleared (%d of %d) ---\x1b[0m\r\n", i+1, total,
			))...)
		}
	}
	return out
}

// preprocessClears replaces each logical screen-clear group with a visible
// "--- screen cleared (N of M) ---" marker so the viewer can scroll through
// the full session history.
func preprocessClears(data []byte) []byte {
	frames := SplitFrames(data)
	if len(frames) <= 1 {
		return data
	}
	return joinFrames(frames)
}

// ConvertJoined reads raw terminal bytes from r, estimates the terminal width,
// and writes a self-contained HTML document using xterm.js to w.
// Frames between screen-clears are joined with visible "screen cleared" markers.
func ConvertJoined(r io.Reader, w io.Writer) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	joined := joinFrames(SplitFrames(data))
	cols := estimateCols(joined)
	encoded := base64.StdEncoding.EncodeToString(joined)

	return htmlTmpl.Execute(w, map[string]any{
		"Cols":   cols,
		"Rows":   50,
		"RawB64": encoded,
	})
}

// ConvertSnapshots reads raw terminal bytes from r and writes an interactive
// HTML document where the user can navigate between frames using buttons or
// arrow keys. Empty leading/trailing frames are filtered out.
func ConvertSnapshots(r io.Reader, w io.Writer) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	frames := SplitFrames(data)

	// Filter leading empty frames.
	for len(frames) > 0 && len(frames[0]) == 0 {
		frames = frames[1:]
	}
	// Filter trailing empty frames.
	for len(frames) > 0 && len(frames[len(frames)-1]) == 0 {
		frames = frames[:len(frames)-1]
	}
	// Ensure at least one frame.
	if len(frames) == 0 {
		frames = [][]byte{{}}
	}

	// Estimate cols across all frames combined.
	var all []byte
	for _, f := range frames {
		all = append(all, f...)
	}
	cols := estimateCols(all)

	// Base64 encode each frame individually.
	encoded := make([]string, len(frames))
	for i, f := range frames {
		encoded[i] = base64.StdEncoding.EncodeToString(f)
	}

	return htmlSnapshotsTmpl.Execute(w, snapshotsTemplateData{
		Cols:   cols,
		Rows:   50,
		Frames: encoded,
	})
}

type snapshotsTemplateData struct {
	Cols   int
	Rows   int
	Frames []string
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
    scrollback: 9999999,
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

var htmlSnapshotsTmpl = template.Must(template.New("snapshots").Parse(strings.TrimSpace(`
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Session recording</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css"/>
<style>
  html, body { margin: 0; padding: 16px; background: #1e1e1e; font-family: sans-serif; }
  #controls { color: #ccc; margin-bottom: 8px; display: flex; align-items: center; gap: 10px; }
  button { background: #333; color: #fff; border: 1px solid #555; padding: 4px 10px; cursor: pointer; border-radius: 4px; font-size: 13px; }
  button:hover:not(:disabled) { background: #444; }
  button:disabled { opacity: 0.4; cursor: default; }
  #counter { font-size: 13px; }
</style>
</head>
<body>
<div id="controls">
  <button id="prev" onclick="navigate(-1)">&#9664; Prev</button>
  <span id="counter"></span>
  <button id="next" onclick="navigate(1)">Next &#9654;</button>
</div>
<div id="t"></div>
<script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.js"></script>
<script>
  const frames = [{{range .Frames}}'{{.}}',{{end}}];
  let current = 0;
  let term = new Terminal({
    cols: {{.Cols}},
    rows: {{.Rows}},
    scrollback: 9999999,
    convertEol: false,
    theme: { background: '#1e1e1e' }
  });
  term.open(document.getElementById('t'));

  function loadFrame(idx) {
    term.dispose();
    term = new Terminal({
      cols: {{.Cols}},
      rows: {{.Rows}},
      scrollback: 100000,
      convertEol: false,
      theme: { background: '#1e1e1e' }
    });
    term.open(document.getElementById('t'));
    const raw = atob(frames[idx]);
    const buf = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i++) buf[i] = raw.charCodeAt(i);
    term.write(buf);
    document.getElementById('counter').textContent = 'Snapshot ' + (idx + 1) + ' of ' + frames.length;
    document.getElementById('prev').disabled = idx === 0;
    document.getElementById('next').disabled = idx === frames.length - 1;
  }

  function navigate(delta) {
    current = Math.max(0, Math.min(frames.length - 1, current + delta));
    loadFrame(current);
  }

  document.addEventListener('keydown', function(e) {
    if (e.key === 'ArrowLeft') navigate(-1);
    if (e.key === 'ArrowRight') navigate(1);
  });

  loadFrame(0);
</script>
</body>
</html>
`)))
