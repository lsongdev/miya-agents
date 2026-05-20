package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

var ErrInterrupted = fmt.Errorf("interrupted")

// Readline provides basic line editing with proper UTF-8/multi-byte character support.
type Readline struct {
	prompt   string
	stdin    *bufio.Reader
	stdout   io.Writer
	history  []string
	histIdx  int
	line     []rune
	pos      int
	rawState *term.State
}

// New creates a new Readline instance.
func New(prompt string) *Readline {
	return &Readline{
		prompt:  prompt,
		stdin:   bufio.NewReader(os.Stdin),
		stdout:  os.Stdout,
		history: make([]string, 0, 100),
	}
}

// Readline reads a line with basic editing support.
func (r *Readline) Readline() (string, error) {
	// Enter raw mode for interactive editing
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		state, err := term.MakeRaw(fd)
		if err != nil {
			// Fall back to simple ReadString if we can't set raw mode
			return r.readSimple()
		}
		r.rawState = state
		defer term.Restore(fd, state)
	} else {
		return r.readSimple()
	}

	fmt.Fprint(r.stdout, r.prompt)
	r.line = nil
	r.pos = 0

	for {
		b, err := r.readRune()
		if err != nil {
			fmt.Fprintf(r.stdout, "\r\n")
			return string(r.line), err
		}

		switch b {
		case '\r', '\n':
			fmt.Fprintf(r.stdout, "\r\n")
			line := string(r.line)
			if line != "" {
				r.history = append(r.history, line)
				if len(r.history) > 100 {
					r.history = r.history[1:]
				}
			}
			r.histIdx = len(r.history)
			return line, nil

		case 3: // Ctrl+C
			fmt.Fprintf(r.stdout, "\r\n")
			return "", ErrInterrupted

		case 127, 8: // Backspace / Ctrl+H
			if r.pos > 0 {
				before := r.line[:r.pos-1]
				after := r.line[r.pos:]
				r.line = append(before, after...)
				r.pos--
				r.redraw()
			}

		case 4: // Ctrl+D (EOF)
			if len(r.line) == 0 {
				fmt.Fprintf(r.stdout, "\r\n")
				return "", io.EOF
			}

		case 1: // Ctrl+A - beginning of line
			r.pos = 0
			r.redraw()

		case 5: // Ctrl+E - end of line
			r.pos = len(r.line)
			r.redraw()

		case 23: // Ctrl+W - delete word
			for r.pos > 0 && (r.line[r.pos-1] == ' ' || r.line[r.pos-1] == '\t') {
				before := r.line[:r.pos-1]
				after := r.line[r.pos:]
				r.line = append(before, after...)
				r.pos--
			}
			for r.pos > 0 && r.line[r.pos-1] != ' ' && r.line[r.pos-1] != '\t' {
				before := r.line[:r.pos-1]
				after := r.line[r.pos:]
				r.line = append(before, after...)
				r.pos--
			}
			r.redraw()

		case 21: // Ctrl+U - delete to beginning
			r.line = r.line[r.pos:]
			r.pos = 0
			r.redraw()

		case 11: // Ctrl+K - delete to end
			r.line = r.line[:r.pos]
			r.redraw()

		case 27: // Escape sequence (arrow keys, etc.)
			seq, _ := r.readSequence()
			switch seq {
			case "[A": // Up arrow - history previous
				if r.histIdx > 0 {
					r.histIdx--
					r.line = []rune(r.history[r.histIdx])
					r.pos = len(r.line)
					r.redraw()
				}
			case "[B": // Down arrow - history next
				if r.histIdx < len(r.history)-1 {
					r.histIdx++
					r.line = []rune(r.history[r.histIdx])
					r.pos = len(r.line)
					r.redraw()
				} else {
					r.histIdx = len(r.history)
					r.line = nil
					r.pos = 0
					r.redraw()
				}
			case "[D": // Left arrow
				if r.pos > 0 {
					r.pos--
					r.moveCursorLeft(1)
				}
			case "[C": // Right arrow
				if r.pos < len(r.line) {
					r.pos++
					r.moveCursorRight(1)
				}
			case "[3": // Delete key
				if r.pos < len(r.line) {
					before := r.line[:r.pos]
					after := r.line[r.pos+1:]
					r.line = append(before, after...)
					r.redraw()
				}
				// consume trailing '~'
				r.readRune()
			}

		default:
			if b >= 32 { // Printable character
				// Insert rune at position
				before := r.line[:r.pos]
				after := r.line[r.pos:]
				r.line = append(append(before, b), after...)
				r.pos++
				r.redraw()
			}
		}
	}
}

// readRune reads a single rune from stdin, handling multi-byte UTF-8.
func (r *Readline) readRune() (rune, error) {
	b, err := r.stdin.ReadByte()
	if err != nil {
		return 0, err
	}

	// Check if this is a multi-byte UTF-8 sequence
	if b < 0x80 {
		return rune(b), nil
	}

	// Determine the number of bytes in this UTF-8 sequence
	var n int
	if b&0xE0 == 0xC0 {
		n = 2
	} else if b&0xF0 == 0xE0 {
		n = 3
	} else if b&0xF8 == 0xF0 {
		n = 4
	} else {
		return rune(b), nil
	}

	buf := make([]byte, n)
	buf[0] = b
	for i := 1; i < n; i++ {
		buf[i], err = r.stdin.ReadByte()
		if err != nil {
			return 0, err
		}
	}

	rv, _ := utf8.DecodeRune(buf)
	return rv, nil
}

// readSequence reads an escape sequence after the initial ESC byte.
func (r *Readline) readSequence() (string, error) {
	var seq []byte
	for {
		b, err := r.stdin.ReadByte()
		if err != nil {
			return string(seq), err
		}
		seq = append(seq, b)
		// Sequences end with a letter or '~'
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || b == '~' {
			break
		}
	}
	return string(seq), nil
}

// redraw clears the current line and redraws it with the prompt.
func (r *Readline) redraw() {
	// Move cursor to beginning of line and clear to end
	fmt.Fprint(r.stdout, "\r\033[K")
	fmt.Fprint(r.stdout, r.prompt)
	fmt.Fprint(r.stdout, string(r.line))
	// Move cursor to correct position
	if r.pos < len(r.line) {
		remaining := string(r.line[r.pos:])
		width := displayWidth(remaining)
		fmt.Fprintf(r.stdout, "\033[%dD", width)
	}
}

// moveCursorLeft moves the cursor left by n display columns.
func (r *Readline) moveCursorLeft(n int) {
	fmt.Fprintf(r.stdout, "\033[%dD", n)
}

// moveCursorRight moves the cursor right by n display columns.
func (r *Readline) moveCursorRight(n int) {
	fmt.Fprintf(r.stdout, "\033[%dC", n)
}

// readSimple falls back to bufio.ReadString for non-interactive input.
func (r *Readline) readSimple() (string, error) {
	line, err := r.stdin.ReadString('\n')
	if err != nil {
		return line, err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// displayWidth returns the display width of a string, accounting for
// wide characters like CJK (which take 2 columns).
func displayWidth(s string) int {
	width := 0
	for _, r := range s {
		if r >= 0x1100 && (r <= 0x115F || // Hangul Jamo
			r == 0x2329 || r == 0x232A ||
			(r >= 0x2E80 && r <= 0xA4CF && r != 0x303F) || // CJK
			(r >= 0xAC00 && r <= 0xD7A3) || // Hangul Syllables
			(r >= 0xF900 && r <= 0xFAFF) || // CJK Compatibility Ideographs
			(r >= 0xFE10 && r <= 0xFE19) || // Vertical forms
			(r >= 0xFE30 && r <= 0xFE6F) || // CJK Compatibility Forms
			(r >= 0xFF00 && r <= 0xFF60) || // Fullwidth ASCII
			(r >= 0xFFE0 && r <= 0xFFE6) ||
			(r >= 0x20000 && r <= 0x2FFFD) ||
			(r >= 0x30000 && r <= 0x3FFFD)) {
			width += 2
		} else {
			width += 1
		}
	}
	return width
}
