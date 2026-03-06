package ansi

import (
	"bufio"
	"fmt"
	"html"
	"io"
	"strconv"
	"strings"
)

// style holds the current SGR rendering state.
type style struct {
	fg, bg         string // CSS color values, empty = default
	bold           bool
	dim            bool
	italic         bool
	underline      bool
	strikethrough  bool
}

func (s style) empty() bool {
	return s.fg == "" && s.bg == "" && !s.bold && !s.dim && !s.italic && !s.underline && !s.strikethrough
}

func (s style) css() string {
	var b strings.Builder
	if s.fg != "" {
		fmt.Fprintf(&b, "color:%s;", s.fg)
	}
	if s.bg != "" {
		fmt.Fprintf(&b, "background:%s;", s.bg)
	}
	if s.bold {
		b.WriteString("font-weight:bold;")
	}
	if s.dim {
		b.WriteString("opacity:0.5;")
	}
	if s.italic {
		b.WriteString("font-style:italic;")
	}
	if s.underline && s.strikethrough {
		b.WriteString("text-decoration:underline line-through;")
	} else if s.underline {
		b.WriteString("text-decoration:underline;")
	} else if s.strikethrough {
		b.WriteString("text-decoration:line-through;")
	}
	return b.String()
}

// standard 16-color palette (matches xterm defaults)
var ansiColors = [16]string{
	"#1e1e1e", // 0  black
	"#cd3131", // 1  red
	"#0dbc79", // 2  green
	"#e5e510", // 3  yellow
	"#2472c8", // 4  blue
	"#bc3fbc", // 5  magenta
	"#11a8cd", // 6  cyan
	"#e5e5e5", // 7  white
	"#666666", // 8  bright black
	"#f14c4c", // 9  bright red
	"#23d18b", // 10 bright green
	"#f5f543", // 11 bright yellow
	"#3b8eea", // 12 bright blue
	"#d670d6", // 13 bright magenta
	"#29b8db", // 14 bright cyan
	"#e5e5e5", // 15 bright white
}

// 256-color cube and grayscale ramp
func color256(n int) string {
	if n < 16 {
		return ansiColors[n]
	}
	if n >= 232 {
		// grayscale ramp
		v := 8 + (n-232)*10
		return fmt.Sprintf("#%02x%02x%02x", v, v, v)
	}
	// 6x6x6 color cube
	n -= 16
	b := n % 6
	g := (n / 6) % 6
	r := n / 36
	toV := func(i int) int {
		if i == 0 {
			return 0
		}
		return 55 + i*40
	}
	return fmt.Sprintf("#%02x%02x%02x", toV(r), toV(g), toV(b))
}

const htmlHeader = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Session recording</title>
<style>
  body {
    background: #1e1e1e;
    color: #e5e5e5;
    font-family: 'Menlo', 'Consolas', 'DejaVu Sans Mono', monospace;
    font-size: 13px;
    line-height: 1.5;
    margin: 0;
    padding: 16px;
  }
  pre {
    margin: 0;
    white-space: pre-wrap;
    word-break: break-all;
  }
  .screen-clear {
    display: block;
    border-top: 1px solid #444;
    border-bottom: 1px solid #444;
    color: #666;
    text-align: center;
    padding: 4px 0;
    margin: 8px 0;
    font-style: italic;
  }
</style>
</head>
<body><pre>`

const htmlFooter = `</pre></body></html>`

// Convert reads raw terminal bytes from r, interprets ANSI escape sequences,
// and writes a self-contained HTML document to w.
func Convert(r io.Reader, w io.Writer) error {
	br := bufio.NewReader(r)
	bw := bufio.NewWriter(w)
	defer bw.Flush()

	if _, err := bw.WriteString(htmlHeader); err != nil {
		return err
	}

	var cur style
	spanOpen := false

	// textBuf accumulates plain-text runes so that backspaces can erase them
	// before we commit to the HTML output.
	var textBuf []rune

	flushText := func() {
		if len(textBuf) == 0 {
			return
		}
		bw.WriteString(html.EscapeString(string(textBuf)))
		textBuf = textBuf[:0]
	}

	closeSpan := func() {
		flushText()
		if spanOpen {
			bw.WriteString("</span>")
			spanOpen = false
		}
	}

	openSpan := func(s style) {
		if s.empty() {
			return
		}
		fmt.Fprintf(bw, `<span style="%s">`, s.css())
		spanOpen = true
	}

	applyStyle := func(s style) {
		if s == cur {
			return
		}
		closeSpan()
		cur = s
		openSpan(cur)
	}

	for {
		b, err := br.ReadByte()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if b != 0x1b {
			// Backspace: erase last buffered rune (handles terminal overwrite tricks)
			if b == 0x08 {
				if len(textBuf) > 0 {
					textBuf = textBuf[:len(textBuf)-1]
				}
				continue
			}
			// Drop other non-printable control bytes (BEL, SOH, EOT, etc.)
			// Keep \t, \n, \r which are handled below.
			if b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
				continue
			}
			// Plain byte — handle \r\n and \r
			if b == '\r' {
				next, err := br.ReadByte()
				if err == nil && next == '\n' {
					textBuf = append(textBuf, '\n')
				} else if err == nil {
					br.UnreadByte()
					// bare \r — ignore (cursor to col 0 in live terminal,
					// meaningless in static HTML)
				}
				continue
			}
			textBuf = append(textBuf, rune(b))
			continue
		}

		// ESC — peek next byte
		next, err := br.ReadByte()
		if err != nil {
			break
		}

		if next != '[' {
			// OSC, ESC c, ESC M, etc. — read and discard
			if next == ']' {
				// OSC: read until ST (ESC \) or BEL
				discardOSC(br)
			}
			// other two-byte or simple sequences: already consumed `next`, done
			continue
		}

		// CSI sequence: read parameter bytes and the final command byte
		params, cmd := readCSI(br)

		switch cmd {
		case 'm':
			// SGR
			newStyle := applySGR(cur, params)
			applyStyle(newStyle)

		case 'J':
			// Erase in display
			if params == "" || params == "2" || params == "3" {
				closeSpan() // also flushes textBuf
				cur = style{}
				bw.WriteString(`<span class="screen-clear">--- screen cleared ---</span>`)
			}

		case 'H', 'f':
			// Cursor position — discard

		default:
			// All other CSI sequences — discard
		}
	}

	closeSpan() // flushes textBuf and closes any open span
	_, err := bw.WriteString(htmlFooter)
	return err
}

// readCSI reads CSI parameter bytes (0x30–0x3f) and intermediate bytes
// (0x20–0x2f) until it hits a final byte (0x40–0x7e).
func readCSI(br *bufio.Reader) (params string, cmd byte) {
	var buf strings.Builder
	for {
		b, err := br.ReadByte()
		if err != nil {
			return buf.String(), 0
		}
		if b >= 0x40 && b <= 0x7e {
			return buf.String(), b
		}
		buf.WriteByte(b)
	}
}

// discardOSC reads until ST (ESC \) or BEL, discarding the OSC payload.
func discardOSC(br *bufio.Reader) {
	for {
		b, err := br.ReadByte()
		if err != nil {
			return
		}
		if b == 0x07 { // BEL
			return
		}
		if b == 0x1b { // ESC — expect \ next
			next, err := br.ReadByte()
			if err != nil || next == '\\' {
				return
			}
			br.UnreadByte()
		}
	}
}

// applySGR returns a new style based on the SGR parameter string.
func applySGR(cur style, params string) style {
	if params == "" || params == "0" {
		return style{}
	}

	parts := strings.Split(params, ";")
	s := cur
	i := 0
	for i < len(parts) {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			i++
			continue
		}
		switch {
		case n == 0:
			s = style{}
		case n == 1:
			s.bold = true
		case n == 2:
			s.dim = true
		case n == 3:
			s.italic = true
		case n == 4:
			s.underline = true
		case n == 9:
			s.strikethrough = true
		case n == 22:
			s.bold, s.dim = false, false
		case n == 23:
			s.italic = false
		case n == 24:
			s.underline = false
		case n == 29:
			s.strikethrough = false
		case n == 39:
			s.fg = ""
		case n == 49:
			s.bg = ""

		// standard fg 30-37, bright fg 90-97
		case n >= 30 && n <= 37:
			s.fg = ansiColors[n-30]
		case n >= 90 && n <= 97:
			s.fg = ansiColors[n-90+8]

		// standard bg 40-47, bright bg 100-107
		case n >= 40 && n <= 47:
			s.bg = ansiColors[n-40]
		case n >= 100 && n <= 107:
			s.bg = ansiColors[n-100+8]

		// 256-color / true color fg
		case n == 38:
			if i+1 < len(parts) {
				mode, _ := strconv.Atoi(parts[i+1])
				if mode == 5 && i+2 < len(parts) {
					idx, _ := strconv.Atoi(parts[i+2])
					s.fg = color256(idx)
					i += 2
				} else if mode == 2 && i+4 < len(parts) {
					r, _ := strconv.Atoi(parts[i+2])
					g, _ := strconv.Atoi(parts[i+3])
					b, _ := strconv.Atoi(parts[i+4])
					s.fg = fmt.Sprintf("#%02x%02x%02x", r, g, b)
					i += 4
				}
			}

		// 256-color / true color bg
		case n == 48:
			if i+1 < len(parts) {
				mode, _ := strconv.Atoi(parts[i+1])
				if mode == 5 && i+2 < len(parts) {
					idx, _ := strconv.Atoi(parts[i+2])
					s.bg = color256(idx)
					i += 2
				} else if mode == 2 && i+4 < len(parts) {
					r, _ := strconv.Atoi(parts[i+2])
					g, _ := strconv.Atoi(parts[i+3])
					b, _ := strconv.Atoi(parts[i+4])
					s.bg = fmt.Sprintf("#%02x%02x%02x", r, g, b)
					i += 4
				}
			}
		}
		i++
	}
	return s
}
