# claude-session-recorder

Records terminal sessions and converts them to self-contained HTML files using xterm.js.

## Tools

### `record-session`

Records a command and saves the session as an interactive HTML file.

```
record-session <command> [args...]
```

Sessions are saved to `.record-sessions/` as both a raw `.ansi` file and a `.html` file. The HTML uses **snapshots mode** — navigate between screen states with the Prev/Next buttons or arrow keys.

### `converter`

Converts an existing `.ansi` file to HTML.

```
converter -mode=<joined|snapshots> <file.ansi>
```

**Modes:**

- `joined` — the full session is played back in a single scrollable xterm.js terminal. Screen clears are replaced with visible `--- screen cleared (N of M) ---` markers so history is preserved.
- `snapshots` — the session is split at each screen clear into frames. The HTML has a control bar to navigate between frames; each frame is loaded into a fresh terminal instance. Use arrow keys or the Prev/Next buttons.

## How it works

1. **Recording** — wraps the Unix `script` command to capture raw terminal output (ANSI/VT100 byte stream) to a `.ansi` file.
2. **Splitting** — `SplitFrames` splits the byte stream on screen-clear sequences (`ESC[2J`, `ESC[3J`), producing a slice of frames. Clear sequences are consumed; each frame is a subslice of the original buffer with no copy.
3. **Rendering** — either `ConvertJoined` (joins frames with text markers into one stream) or `ConvertSnapshots` (base64-encodes each frame separately, embeds all into the HTML).

The output is a single self-contained `.html` file with no external dependencies at view time (xterm.js is loaded from CDN on open).

## Building

```
go build -o record-session .
go build -o converter ./cmd/converter
```
