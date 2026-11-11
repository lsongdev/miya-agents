package main

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// ANSI color codes
const (
	reset     = "\033[0m"
	bold      = "\033[1m"
	dim       = "\033[2m"
	italic    = "\033[3m"
	underline = "\033[4m"

	black   = "\033[30m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
	white   = "\033[37m"

	brightBlack   = "\033[90m"
	brightRed     = "\033[91m"
	brightGreen   = "\033[92m"
	brightYellow  = "\033[93m"
	brightBlue    = "\033[94m"
	brightMagenta = "\033[95m"
	brightCyan    = "\033[96m"
	brightWhite   = "\033[97m"
)

// MarkdownRenderer renders markdown text to the terminal with basic formatting.
type MarkdownRenderer struct {
	w         io.Writer
	lineBuf   strings.Builder
	inCode    bool
	codeLang  string
	codeBuf   strings.Builder
	col       int // current column position
}

// NewMarkdownRenderer creates a new renderer.
func NewMarkdownRenderer(w io.Writer) *MarkdownRenderer {
	return &MarkdownRenderer{w: w}
}

// Write processes incoming markdown text and renders character by character.
func (r *MarkdownRenderer) Write(text string) error {
	for _, ch := range text {
		if r.inCode {
			if ch == '\n' {
				// Check if the current buffered line is a closing fence (```)
				// Find the last newline in codeBuf to get the current line
				buf := r.codeBuf.String()
				lastNewline := strings.LastIndex(buf, "\n")
				var currentLine string
				if lastNewline == -1 {
					currentLine = buf
				} else {
					currentLine = buf[lastNewline+1:]
				}
				if strings.TrimSpace(currentLine) == "```" {
					// Remove the closing fence from codeBuf before rendering
					if lastNewline == -1 {
						r.codeBuf.Reset()
					} else {
						r.codeBuf = strings.Builder{}
						r.codeBuf.WriteString(buf[:lastNewline])
						r.codeBuf.WriteByte('\n')
					}
					r.renderCodeBlock()
					r.inCode = false
					r.codeBuf.Reset()
					r.col = 0
					continue
				}
				r.codeBuf.WriteByte('\n')
				r.col = 0
			} else {
				r.codeBuf.WriteRune(ch)
				r.col++
			}
			continue
		}

		if ch == '\n' {
			r.flushLine()
			r.lineBuf.Reset()
			r.col = 0
			continue
		}

		r.lineBuf.WriteRune(ch)
		r.col++

		// Render completed inline patterns immediately
		s := r.lineBuf.String()

		// **bold**
		if idx := strings.Index(s, "**"); idx != -1 {
			end := strings.Index(s[idx+2:], "**")
			if end != -1 {
				fmt.Fprint(r.w, s[:idx])
				fmt.Fprintf(r.w, "%s%s%s", bold, s[idx+2:idx+2+end], reset)
				r.lineBuf.Reset()
				r.lineBuf.WriteString(s[idx+2+end+2:])
				continue
			}
		}

		// `code` inline - but skip if this looks like a code fence (```)
		trimmed := strings.TrimSpace(s)
		if strings.HasPrefix(trimmed, "```") {
			// This is a code fence start, skip inline code handling
		} else if idx := strings.Index(s, "`"); idx != -1 {
			end := strings.Index(s[idx+1:], "`")
			if end != -1 {
				content := s[idx+1 : idx+1+end]
				// Skip empty inline code (``) which could be part of a code fence (```)
				if content == "" {
					// Two backticks with nothing between - could be start of a fence, skip
				} else {
					fmt.Fprint(r.w, s[:idx])
					fmt.Fprintf(r.w, "%s%s%s", green, content, reset)
					r.lineBuf.Reset()
					r.lineBuf.WriteString(s[idx+1+end+1:])
					continue
				}
			}
		}

		// [link](url)
		if idx := strings.Index(s, "]("); idx != -1 {
			start := strings.LastIndex(s[:idx], "[")
			if start != -1 {
				end := strings.Index(s[idx+2:], ")")
				if end != -1 {
					txt := s[start+1 : idx]
					url := s[idx+2 : idx+2+end]
					fmt.Fprint(r.w, s[:start])
					fmt.Fprintf(r.w, "%s%s%s%s(%s)%s", underline, blue, txt, reset, dim, url)
					fmt.Fprint(r.w, reset)
					r.lineBuf.Reset()
					r.lineBuf.WriteString(s[idx+2+end+1:])
					continue
				}
			}
		}

		// *italic* or _italic_
		for _, mark := range []string{"*", "_"} {
			if idx := strings.Index(s, mark); idx != -1 {
				end := strings.Index(s[idx+1:], mark)
				if end != -1 && idx+1+end < len(s) {
					inner := s[idx+1 : idx+1+end]
					if inner != "" && !strings.Contains(inner, " ") {
						fmt.Fprint(r.w, s[:idx])
						fmt.Fprintf(r.w, "%s%s%s", italic, inner, reset)
						r.lineBuf.Reset()
						r.lineBuf.WriteString(s[idx+1+end+1:])
						break
					}
				}
			}
		}
	}
	return nil
}

func (r *MarkdownRenderer) flushLine() {
	line := r.lineBuf.String()
	trimmed := strings.TrimSpace(line)

	// Code fence toggle
	if strings.HasPrefix(trimmed, "```") {
		if r.inCode {
			r.renderCodeBlock()
			r.inCode = false
			r.codeBuf.Reset()
		} else {
			r.inCode = true
			r.codeLang = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
		}
		return
	}

	if r.inCode {
		r.codeBuf.WriteString(line)
		r.codeBuf.WriteByte('\n')
		return
	}

	// Render remaining plain text in buffer
	if r.lineBuf.Len() > 0 {
		fmt.Fprint(r.w, line)
	}
	fmt.Fprintln(r.w)
}

func (r *MarkdownRenderer) renderLine(line string) {
	trimmed := strings.TrimSpace(line)

	switch {
	case trimmed == "":
		fmt.Fprintln(r.w)

	case strings.HasPrefix(trimmed, "# "):
		fmt.Fprintf(r.w, "%s%s%s%s\n", bold, cyan, trimmed[2:], reset)

	case strings.HasPrefix(trimmed, "## "):
		fmt.Fprintf(r.w, "%s%s%s%s\n", bold, cyan, trimmed[3:], reset)

	case strings.HasPrefix(trimmed, "### "):
		fmt.Fprintf(r.w, "%s%s%s%s\n", bold, cyan, trimmed[4:], reset)

	case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
		_, rest := utf8.DecodeRuneInString(trimmed)
		content := strings.TrimSpace(trimmed[rest:])
		fmt.Fprintf(r.w, "  %s•%s %s\n", dim, reset, r.inlineFormat(content))

	case len(trimmed) > 2 && trimmed[0] >= '1' && trimmed[0] <= '9' && trimmed[1] == '.' && trimmed[2] == ' ':
		idx := strings.Index(trimmed, ". ")
		num := trimmed[:idx+1]
		content := trimmed[idx+2:]
		fmt.Fprintf(r.w, "  %s%s%s %s\n", dim, num, reset, r.inlineFormat(content))

	case strings.HasPrefix(trimmed, "> "):
		content := trimmed[2:]
		fmt.Fprintf(r.w, "  %s│%s %s\n", brightBlack, reset, r.inlineFormat(content))

	case trimmed == "---" || trimmed == "***":
		fmt.Fprintf(r.w, "%s%s%s\n", dim, strings.Repeat("─", 40), reset)

	default:
		// Render remaining buffer with inline formatting
		fmt.Fprintf(r.w, "%s\n", r.inlineFormat(line))
	}
}

func (r *MarkdownRenderer) renderCodeBlock() {
	code := r.codeBuf.String()
	if r.codeLang != "" {
		fmt.Fprintf(r.w, "\n%s%s ▸ %s%s\n", dim, "```", r.codeLang, reset)
	} else {
		fmt.Fprintf(r.w, "\n%s%s%s\n", dim, "```", reset)
	}
	highlighted := highlightCode(code, r.codeLang)
	fmt.Fprint(r.w, highlighted)
	fmt.Fprintf(r.w, "%s%s%s\n\n", dim, "```", reset)
}

// Flush renders any remaining buffered text.
func (r *MarkdownRenderer) Flush() error {
	if r.lineBuf.Len() > 0 {
		r.flushLine()
		r.lineBuf.Reset()
	}
	if r.inCode {
		r.renderCodeBlock()
		r.inCode = false
	}
	return nil
}

// inlineFormat processes inline markdown: **bold**, *italic*, `code`, [link](url)
func (r *MarkdownRenderer) inlineFormat(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		switch {
		case i+1 < len(s) && s[i:i+2] == "**":
			end := strings.Index(s[i+2:], "**")
			if end == -1 {
				out.WriteString("**")
				i += 2
				continue
			}
			out.WriteString(bold)
			out.WriteString(s[i+2 : i+2+end])
			out.WriteString(reset)
			i += 4 + end

		case i+1 < len(s) && (s[i:i+2] == "*_" || s[i:i+2] == "_*"):
			end := strings.Index(s[i+2:], s[i:i+2])
			if end == -1 {
				out.WriteByte(s[i])
				i++
				continue
			}
			out.WriteString(bold + italic)
			out.WriteString(s[i+2 : i+2+end])
			out.WriteString(reset)
			i += 4 + end

		case s[i] == '*' || s[i] == '_':
			mark := string(s[i])
			end := strings.Index(s[i+1:], mark)
			if end == -1 {
				out.WriteByte(s[i])
				i++
				continue
			}
			inner := s[i+1 : i+1+end]
			if inner == "" {
				out.WriteByte(s[i])
				i++
				continue
			}
			out.WriteString(italic)
			out.WriteString(inner)
			out.WriteString(reset)
			i += 2 + end

		case s[i] == '`':
			end := strings.Index(s[i+1:], "`")
			if end == -1 {
				out.WriteByte(s[i])
				i++
				continue
			}
			out.WriteString(green)
			out.WriteString(s[i+1 : i+1+end])
			out.WriteString(reset)
			i += 2 + end

		case s[i] == '[':
			closeBracket := strings.Index(s[i:], "]")
			if closeBracket == -1 || i+closeBracket+1 >= len(s) || s[i+closeBracket+1] != '(' {
				out.WriteByte(s[i])
				i++
				continue
			}
			closeParen := strings.Index(s[i+closeBracket+1:], ")")
			if closeParen == -1 {
				out.WriteByte(s[i])
				i++
				continue
			}
			text := s[i+1 : i+closeBracket]
			url := s[i+closeBracket+2 : i+closeBracket+1+closeParen]
			out.WriteString(underline + blue)
			out.WriteString(text)
			out.WriteString(reset)
			out.WriteString(dim)
			out.WriteString(fmt.Sprintf(" (%s)", url))
			out.WriteString(reset)
			i += closeBracket + closeParen + 3

		default:
			out.WriteByte(s[i])
			i++
		}
	}
	return out.String()
}

// highlightCode applies basic syntax highlighting to code.
func highlightCode(code, lang string) string {
	keywords := getKeywords(lang)
	if len(keywords) == 0 {
		return code
	}

	var out strings.Builder
	i := 0
	for i < len(code) {
		matched := false
		for _, kw := range keywords {
			if !strings.HasPrefix(code[i:], kw) {
				continue
			}
			// Check word boundary
			end := i + len(kw)
			if end < len(code) && isWordChar(code[end]) {
				continue
			}
			if i > 0 && isWordChar(code[i-1]) {
				continue
			}
			out.WriteString(yellow)
			out.WriteString(kw)
			out.WriteString(reset)
			i = end
			matched = true
			break
		}
		if !matched {
			// Highlight strings
			if code[i] == '"' || code[i] == '\'' || code[i] == '`' {
				quote := code[i]
				out.WriteString(green)
				out.WriteByte(code[i])
				i++
				for i < len(code) && code[i] != quote {
					if code[i] == '\\' && i+1 < len(code) {
						out.WriteByte(code[i])
						out.WriteByte(code[i+1])
						i += 2
						continue
					}
					out.WriteByte(code[i])
					i++
				}
				if i < len(code) {
					out.WriteByte(code[i])
					i++
				}
				out.WriteString(reset)
				continue
			}
			// Highlight comments
			if i+1 < len(code) && code[i] == '/' && code[i+1] == '/' {
				out.WriteString(brightBlack + italic)
				for i < len(code) && code[i] != '\n' {
					out.WriteByte(code[i])
					i++
				}
				out.WriteString(reset)
				continue
			}
			// Highlight numbers
			if i < len(code) && code[i] >= '0' && code[i] <= '9' {
				out.WriteString(magenta)
				for i < len(code) && ((code[i] >= '0' && code[i] <= '9') || code[i] == '.' || code[i] == 'x' || code[i] == 'X') {
					out.WriteByte(code[i])
					i++
				}
				out.WriteString(reset)
				continue
			}
			out.WriteByte(code[i])
			i++
		}
	}
	return out.String()
}

func getKeywords(lang string) []string {
	switch strings.ToLower(lang) {
	case "go", "golang":
		return []string{
			"func", "return", "if", "else", "for", "range", "switch", "case",
			"type", "struct", "interface", "map", "chan", "go", "defer",
			"package", "import", "var", "const", "select", "break", "continue",
			"fallthrough", "goto", "default", "nil", "true", "false", "make",
			"append", "len", "cap", "close", "delete", "copy", "panic", "recover",
		}
	case "python", "py":
		return []string{
			"def", "return", "if", "elif", "else", "for", "while", "class",
			"import", "from", "as", "try", "except", "finally", "raise",
			"with", "yield", "lambda", "pass", "break", "continue", "and",
			"or", "not", "in", "is", "None", "True", "False", "self",
			"async", "await",
		}
	case "javascript", "js", "typescript", "ts":
		return []string{
			"function", "return", "if", "else", "for", "while", "switch", "case",
			"class", "const", "let", "var", "import", "export", "from", "default",
			"async", "await", "try", "catch", "finally", "throw", "new", "this",
			"true", "false", "null", "undefined", "typeof", "instanceof",
		}
	case "bash", "sh", "shell", "zsh":
		return []string{
			"if", "then", "else", "elif", "fi", "for", "while", "do", "done",
			"case", "esac", "function", "return", "exit", "echo", "export",
			"local", "source", "alias", "cd", "mkdir", "rm", "cp", "mv",
		}
	case "rust", "rs":
		return []string{
			"fn", "return", "if", "else", "for", "while", "loop", "match",
			"struct", "enum", "impl", "trait", "pub", "use", "mod", "crate",
			"let", "mut", "const", "static", "self", "Self", "super",
			"true", "false", "None", "Some", "Ok", "Err",
		}
	default:
		return nil
	}
}

func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}
