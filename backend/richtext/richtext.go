// Package richtext converts the restricted Markdown subset for messages
// and descriptions into mail HTML and plain text.
//
// Why a small custom renderer instead of goldmark + bluemonday: the subset
// is tiny (paragraphs, bold, italic, lists, quotes, code, https links). The
// renderer builds the HTML exclusively from fixed tags and HTML-escaped
// text ("safe by construction"). There is no raw-HTML passthrough
// that a sanitizer would have to catch again, no new dependency
// and no sanitizer configuration that could be silently weakened. The plain
// text for chat and webhook is produced from the same syntax tree.
//
// Everything that is not part of the subset (headings, images, tables,
// raw HTML, scripts) stays as visible text. Legacy data (plain text
// with *, _, # or - at the start of a line) therefore stays readable; simple
// line breaks are preserved as breaks.
package richtext

import (
	"html"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxLen is the largest allowed length of a formatted message (characters).
const MaxLen = 5000

// MaxRenderLen and MaxBlockLen limit the effort when rendering legacy data
// that was never checked against MaxLen: if the whole text is longer than
// MaxRenderLen or a block (lines without a blank line) longer than MaxBlockLen,
// the text is output without formatting. The same values apply in the
// frontend (frontend/src/utils/richText.js), so that page and mail format the same
// text identically; both are above MaxLen, so allowed
// texts are always formatted. A test keeps both sides equal.
const (
	MaxRenderLen = 8000
	MaxBlockLen  = MaxLen
)

// maxInlineDepth limits the nesting of bold/italic/link.
const maxInlineDepth = 4

// TooLong reports whether s is longer than MaxLen characters (runes).
func TooLong(s string) bool { return utf8.RuneCountInString(s) > MaxLen }

type nodeKind int

const (
	kText nodeKind = iota
	kBr
	kStrong
	kEm
	kCode
	kLink
)

// node is an inline node. Text and code carry text; link carries href and
// children; strong/em carry children.
type node struct {
	kind     nodeKind
	text     string
	href     string
	children []node
}

type blockKind int

const (
	bPara blockKind = iota
	bUL
	bOL
	bQuote
)

// block is a paragraph (items[0]), a list (one entry per item) or a
// quote (items[0]).
type block struct {
	kind  blockKind
	items [][]node
	start int // first number of an ordered list, 0 = unknown
}

// ---------------------------------------------------------------- Output

// ToMailHTML returns the HTML for emails: only fixed tags with inline styles,
// links only https (target=_blank, rel=noopener noreferrer nofollow). The
// result is ALWAYS single-line (no CR/LF), so that the header-injection protection
// of sendAlertEmail (hasHeaderInjectionChars) does not trigger.
func ToMailHTML(md string) string {
	var sb strings.Builder
	for _, b := range parse(md) {
		switch b.kind {
		case bPara:
			sb.WriteString(`<p style="margin:0 0 10px;">`)
			writeInlineHTML(&sb, b.items[0])
			sb.WriteString(`</p>`)
		case bQuote:
			sb.WriteString(`<blockquote style="margin:0 0 10px;padding:2px 0 2px 12px;border-left:3px solid #d5d9e0;color:#57616f;">`)
			writeInlineHTML(&sb, b.items[0])
			sb.WriteString(`</blockquote>`)
		case bUL, bOL:
			tag, startAttr := "ul", ""
			if b.kind == bOL {
				tag = "ol"
				if b.start != 1 {
					startAttr = ` start="` + itoa(b.start) + `"`
				}
			}
			sb.WriteString(`<` + tag + startAttr + ` style="margin:0 0 10px;padding-left:22px;">`)
			for _, it := range b.items {
				sb.WriteString(`<li style="margin:0 0 4px;">`)
				writeInlineHTML(&sb, it)
				sb.WriteString(`</li>`)
			}
			sb.WriteString(`</` + tag + `>`)
		}
	}
	return sb.String()
}

func writeInlineHTML(sb *strings.Builder, nodes []node) {
	for _, n := range nodes {
		switch n.kind {
		case kText:
			sb.WriteString(html.EscapeString(n.text))
		case kBr:
			sb.WriteString(`<br>`)
		case kStrong:
			sb.WriteString(`<strong>`)
			writeInlineHTML(sb, n.children)
			sb.WriteString(`</strong>`)
		case kEm:
			sb.WriteString(`<em>`)
			writeInlineHTML(sb, n.children)
			sb.WriteString(`</em>`)
		case kCode:
			sb.WriteString(`<code style="font-family:ui-monospace,Menlo,Consolas,monospace;font-size:13px;background:#f1f2f4;padding:1px 4px;border-radius:3px;">`)
			sb.WriteString(strings.ReplaceAll(html.EscapeString(n.text), "\n", "<br>"))
			sb.WriteString(`</code>`)
		case kLink:
			sb.WriteString(`<a href="` + html.EscapeString(n.href) + `" target="_blank" rel="noopener noreferrer nofollow" style="color:#4c6ef5;text-decoration:underline;">`)
			writeInlineHTML(sb, n.children)
			sb.WriteString(`</a>`)
		}
	}
}

// ToPlainText returns readable plain text for chat, webhook and search: without
// Markdown characters, lists as "- " or "1. ", links as "Text (URL)".
func ToPlainText(md string) string {
	var parts []string
	for _, b := range parse(md) {
		switch b.kind {
		case bPara:
			parts = append(parts, plainInline(b.items[0]))
		case bQuote:
			parts = append(parts, "> "+strings.ReplaceAll(plainInline(b.items[0]), "\n", "\n> "))
		case bUL, bOL:
			lines := make([]string, 0, len(b.items))
			for i, it := range b.items {
				marker := "- "
				if b.kind == bOL {
					marker = itoa(b.start+i) + ". "
				}
				lines = append(lines, marker+strings.ReplaceAll(plainInline(it), "\n", "\n  "))
			}
			parts = append(parts, strings.Join(lines, "\n"))
		}
	}
	return strings.Join(parts, "\n\n")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}

func plainInline(nodes []node) string {
	var sb strings.Builder
	for _, n := range nodes {
		switch n.kind {
		case kText, kCode:
			sb.WriteString(n.text)
		case kBr:
			sb.WriteString("\n")
		case kStrong, kEm:
			sb.WriteString(plainInline(n.children))
		case kLink:
			text := plainInline(n.children)
			if text == n.href {
				sb.WriteString(n.href)
			} else {
				sb.WriteString(text + " (" + n.href + ")")
			}
		}
	}
	return sb.String()
}

// ---------------------------------------------------------------- Blocks

// normalize unifies line endings and replaces control characters (except
// tab and line break) with spaces.
func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Map(func(r rune) rune {
		if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

func parse(md string) []block {
	src := normalize(md)
	if utf8.RuneCountInString(src) > MaxRenderLen {
		return plainParagraphs(src)
	}
	lines := strings.Split(src, "\n")
	if hasOversizedBlock(lines) {
		return plainParagraphs(src)
	}
	var blocks []block
	for i := 0; i < len(lines); {
		line := lines[i]
		switch {
		case strings.TrimSpace(line) == "":
			i++
		case isRule(line):
			// Horizontal rules are not part of the subset: visible text, own paragraph.
			blocks = append(blocks, block{kind: bPara, items: [][]node{{{kind: kText, text: strings.TrimSpace(line)}}}})
			i++
		case isFence(line):
			var b block
			b, i = parseFence(lines, i)
			blocks = append(blocks, b)
		case isQuote(line):
			var b block
			b, i = parseQuote(lines, i)
			blocks = append(blocks, b)
		default:
			if kind, _, ok := listMarker(line, false); ok {
				var b block
				b, i = parseList(lines, i, kind)
				blocks = append(blocks, b)
				continue
			}
			var b block
			b, i = parseParagraph(lines, i)
			blocks = append(blocks, b)
		}
	}
	return blocks
}

// hasOversizedBlock reports whether lines without a blank line in between together are longer
// than MaxBlockLen characters (like the block limit in the frontend).
func hasOversizedBlock(lines []string) bool {
	size := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			size = 0
			continue
		}
		if size > 0 {
			size++ // line break
		}
		size += utf8.RuneCountInString(l)
		if size > MaxBlockLen {
			return true
		}
	}
	return false
}

// plainParagraphs outputs overlong legacy data without formatting.
func plainParagraphs(src string) []block {
	var blocks []block
	for _, para := range strings.Split(src, "\n\n") {
		if strings.TrimSpace(para) == "" {
			continue
		}
		blocks = append(blocks, block{kind: bPara, items: [][]node{literalLines(strings.Split(para, "\n"))}})
	}
	return blocks
}

// literalLines assembles lines without Markdown evaluation (text + break).
func literalLines(lines []string) []node {
	var out []node
	for i, l := range lines {
		if i > 0 {
			out = append(out, node{kind: kBr})
		}
		out = append(out, node{kind: kText, text: strings.TrimRight(l, " \t")})
	}
	return out
}

// isFence detects the start of a fenced code block. Per CommonMark the
// info text after ``` must not contain another backtick: "```npm install```" is
// a paragraph with inline code and not a fence (otherwise the content would
// be lost and the "fence" would swallow all following lines).
func isFence(line string) bool {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "```") {
		return !strings.Contains(strings.TrimLeft(t, "`"), "`")
	}
	return strings.HasPrefix(t, "~~~")
}

// parseFence: fenced code block. There is no <pre>; the content appears as
// code with line breaks.
func parseFence(lines []string, start int) (block, int) {
	fence := strings.TrimSpace(lines[start])[:3]
	var body []string
	i := start + 1
	for ; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), fence) {
			i++
			break
		}
		body = append(body, lines[i])
	}
	return block{kind: bPara, items: [][]node{{{kind: kCode, text: strings.Join(body, "\n")}}}}, i
}

func isQuote(line string) bool { return strings.HasPrefix(strings.TrimLeft(line, " "), ">") }

func parseQuote(lines []string, start int) (block, int) {
	var content []string
	i := start
	for ; i < len(lines) && isQuote(lines[i]); i++ {
		t := strings.TrimPrefix(strings.TrimLeft(lines[i], " "), ">")
		content = append(content, strings.TrimPrefix(t, " "))
	}
	return block{kind: bQuote, items: [][]node{parseInline([]rune(strings.Join(content, "\n")), 0, false)}}, i
}

// listMarker detects "- ", "* ", "+ " and "1. " / "1) ". interrupting=true
// (a paragraph is running) only allows bullet characters and "1.", so that a line
// like "2024. Das Jahr" does not tear a paragraph apart.
func listMarker(line string, interrupting bool) (kind blockKind, rest string, ok bool) {
	t := strings.TrimLeft(line, " \t")
	if len(t) >= 2 && (t[0] == '-' || t[0] == '*' || t[0] == '+') && (t[1] == ' ' || t[1] == '\t') {
		if r := strings.TrimSpace(t[2:]); r != "" {
			return bUL, r, true
		}
		return 0, "", false
	}
	j := 0
	for j < len(t) && j < 9 && t[j] >= '0' && t[j] <= '9' {
		j++
	}
	if j == 0 || j+1 >= len(t) || (t[j] != '.' && t[j] != ')') || (t[j+1] != ' ' && t[j+1] != '\t') {
		return 0, "", false
	}
	if interrupting && t[:j] != "1" {
		return 0, "", false
	}
	if r := strings.TrimSpace(t[j+2:]); r != "" {
		return bOL, r, true
	}
	return 0, "", false
}

// isRule detects horizontal rules (---, ***, ___): they stay visible text.
func isRule(line string) bool {
	t := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(line), " ", ""), "\t", "")
	if len(t) < 3 {
		return false
	}
	for _, c := range []byte{'-', '*', '_'} {
		if strings.Trim(t, string(c)) == "" {
			return true
		}
	}
	return false
}

// listNumber returns the number before "." or ")" of an ordered list line
// (listMarker has already recognized it as such: at most 9 digits).
func listNumber(line string) int {
	n := 0
	for _, c := range strings.TrimLeft(line, " \t") {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func parseList(lines []string, start int, kind blockKind) (block, int) {
	var items [][]string
	i := start
	for i < len(lines) {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			// Blank line: the list only continues if an item of the same kind follows.
			j := i
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				j++
			}
			if j < len(lines) {
				if k, _, ok := listMarker(lines[j], false); ok && k == kind && !isRule(lines[j]) {
					i = j
					continue
				}
			}
			break
		}
		if isRule(line) {
			break
		}
		if k, rest, ok := listMarker(line, false); ok {
			if k != kind {
				break
			}
			items = append(items, []string{rest})
		} else if len(items) > 0 && !isFence(line) && !isQuote(line) {
			items[len(items)-1] = append(items[len(items)-1], strings.TrimSpace(line))
		} else {
			break
		}
		i++
	}
	b := block{kind: kind, start: 1}
	if kind == bOL {
		b.start = listNumber(lines[start])
	}
	for _, it := range items {
		b.items = append(b.items, parseInline([]rune(strings.Join(it, "\n")), 0, false))
	}
	return b, i
}

func parseParagraph(lines []string, start int) (block, int) {
	var content []string
	i := start
	for ; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" || isFence(l) || isQuote(l) || isRule(l) {
			break
		}
		if _, _, ok := listMarker(l, true); ok && i > start {
			break
		}
		content = append(content, strings.TrimSpace(l))
	}
	return block{kind: bPara, items: [][]node{parseInline([]rune(strings.Join(content, "\n")), 0, false)}}, i
}

// ---------------------------------------------------------------- Inline

func isASCIIPunct(r rune) bool {
	return r < 128 && unicode.IsPunct(r) || strings.ContainsRune("$+<=>^`|~", r)
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func parseInline(rs []rune, depth int, inLink bool) []node {
	var out []node
	var buf []rune
	flush := func() {
		if len(buf) > 0 {
			out = append(out, node{kind: kText, text: string(buf)})
			buf = nil
		}
	}
	for i := 0; i < len(rs); {
		c := rs[i]
		switch {
		case c == '\n':
			for len(buf) > 0 && (buf[len(buf)-1] == ' ' || buf[len(buf)-1] == '\t') {
				buf = buf[:len(buf)-1]
			}
			flush()
			out = append(out, node{kind: kBr})
			i++
		case c == '\\' && i+1 < len(rs) && isASCIIPunct(rs[i+1]):
			buf = append(buf, rs[i+1])
			i += 2
		case c == '`':
			if n, next, ok := parseCodeSpan(rs, i); ok {
				flush()
				out = append(out, n)
				i = next
				continue
			}
			j := i
			for j < len(rs) && rs[j] == '`' {
				j++
			}
			buf = append(buf, rs[i:j]...)
			i = j
		case c == '[' && !inLink && depth < maxInlineDepth:
			if nodes, next, ok := parseLink(rs, i, depth); ok {
				if len(buf) > 0 && buf[len(buf)-1] == '!' {
					// Image syntax: no <img>, the notation stays visible as text.
					buf = append(buf, rs[i:next]...)
				} else {
					flush()
					out = append(out, nodes...)
				}
				i = next
				continue
			}
			buf = append(buf, c)
			i++
		case (c == '*' || c == '_') && depth < maxInlineDepth:
			if nodes, next, ok := parseEmphasis(rs, i, depth, inLink); ok {
				flush()
				out = append(out, nodes...)
				i = next
				continue
			}
			j := i
			for j < len(rs) && rs[j] == c {
				j++
			}
			buf = append(buf, rs[i:j]...)
			i = j
		case (c == 'h' || c == 'H') && !inLink && (i == 0 || !isWord(rs[i-1])):
			if n, next, ok := parseAutolink(rs, i); ok {
				flush()
				out = append(out, n)
				i = next
				continue
			}
			buf = append(buf, c)
			i++
		default:
			buf = append(buf, c)
			i++
		}
	}
	flush()
	return out
}

func parseCodeSpan(rs []rune, start int) (node, int, bool) {
	n := 0
	for start+n < len(rs) && rs[start+n] == '`' {
		n++
	}
	for j := start + n; j < len(rs); j++ {
		if rs[j] != '`' {
			continue
		}
		m := 0
		for j+m < len(rs) && rs[j+m] == '`' {
			m++
		}
		if m == n {
			code := strings.TrimSpace(string(rs[start+n : j]))
			if code == "" {
				return node{}, 0, false
			}
			return node{kind: kCode, text: strings.ReplaceAll(code, "\n", " ")}, j + m, true
		}
		j += m - 1
	}
	return node{}, 0, false
}

// parseEmphasis: **bold**, __bold__, *italic*, _italic_, ***both***. The
// opening marker stands before a non-space, the closing one after
// one; "_" does not apply mid-word (snake_case stays text). Surplus
// markers before the opening one stay as text.
func parseEmphasis(rs []rune, start, depth int, inLink bool) ([]node, int, bool) {
	c := rs[start]
	run := 0
	for start+run < len(rs) && rs[start+run] == c {
		run++
	}
	if start+run >= len(rs) || unicode.IsSpace(rs[start+run]) {
		return nil, 0, false
	}
	if c == '_' && start > 0 && isWord(rs[start-1]) {
		return nil, 0, false
	}
	for k := min(run, 3); k >= 1; k-- {
		from := start + (run - k)
		end, ok := findCloser(rs, from+k, c, k)
		if !ok {
			continue
		}
		inner := parseInline(rs[from+k:end], depth+1, inLink)
		n := emphasisNode(k, inner)
		if run > k {
			return []node{{kind: kText, text: strings.Repeat(string(c), run-k)}, n}, end + k, true
		}
		return []node{n}, end + k, true
	}
	return nil, 0, false
}

func emphasisNode(k int, inner []node) node {
	switch k {
	case 1:
		return node{kind: kEm, children: inner}
	case 2:
		return node{kind: kStrong, children: inner}
	}
	return node{kind: kEm, children: []node{{kind: kStrong, children: inner}}}
}

// findCloser searches from from for the closing marker of exactly k characters c.
func findCloser(rs []rune, from int, c rune, k int) (int, bool) {
	for j := from; j+k <= len(rs); j++ {
		if rs[j] != c {
			continue
		}
		m := 0
		for j+m < len(rs) && rs[j+m] == c {
			m++
		}
		if m == k && j > from && !unicode.IsSpace(rs[j-1]) && (c == '*' || j+m >= len(rs) || !isWord(rs[j+m])) {
			return j, true
		}
		j += m - 1
	}
	return 0, false
}

// parseLink: [text](https://target "title"). Only https becomes a link; a
// syntactically valid link with another scheme (http:, javascript:, data:, ...)
// appears as "Text (target)" without a link. Invalid notations stay text.
func parseLink(rs []rune, start, depth int) ([]node, int, bool) {
	closeIdx, ok := matchBracket(rs, start, '[', ']')
	if !ok || closeIdx+1 >= len(rs) || rs[closeIdx+1] != '(' {
		return nil, 0, false
	}
	parenIdx, ok := matchBracket(rs, closeIdx+1, '(', ')')
	if !ok {
		return nil, 0, false
	}
	dest, ok := linkDestination(string(rs[closeIdx+2 : parenIdx]))
	if !ok {
		return nil, 0, false
	}
	text := rs[start+1 : closeIdx]
	children := parseInline(text, depth+1, true)
	if href, ok := httpsURL(dest); ok {
		if len(children) == 0 {
			children = []node{{kind: kText, text: href}}
		}
		return []node{{kind: kLink, href: href, children: children}}, parenIdx + 1, true
	}
	return append(children, node{kind: kText, text: " (" + dest + ")"}), parenIdx + 1, true
}

// matchBracket finds the matching closing bracket (nested,
// backslash escapes respected).
func matchBracket(rs []rune, start int, open, closeCh rune) (int, bool) {
	level := 0
	for j := start; j < len(rs); j++ {
		switch rs[j] {
		case '\\':
			j++
		case open:
			level++
		case closeCh:
			level--
			if level == 0 {
				return j, true
			}
		}
	}
	return 0, false
}

// linkDestination splits the content of the round brackets into target and
// optional title (discarded). Whitespace, control characters or <>" in the target
// make the link invalid (no whitespace trick).
func linkDestination(inner string) (string, bool) {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return "", false
	}
	dest, rest := inner, ""
	if idx := strings.IndexFunc(inner, unicode.IsSpace); idx >= 0 {
		dest, rest = inner[:idx], strings.TrimSpace(inner[idx:])
	}
	if rest != "" && !validLinkTitle(rest) {
		return "", false
	}
	for _, r := range dest {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune(`<>"\`, r) {
			return "", false
		}
	}
	return dest, true
}

func validLinkTitle(t string) bool {
	if len(t) < 2 {
		return false
	}
	pairs := map[byte]byte{'"': '"', '\'': '\'', '(': ')'}
	end, ok := pairs[t[0]]
	return ok && t[len(t)-1] == end
}

// httpsURL returns the normalized https URL. Other schemes, missing
// host and credentials in the URL (https://user@host) are rejected.
func httpsURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Hostname() == "" {
		return "", false
	}
	return u.String(), true
}

// parseAutolink turns a bare https:// address into a link. Punctuation at the
// end does not belong to the address.
func parseAutolink(rs []rune, start int) (node, int, bool) {
	const prefix = "https://"
	if start+len(prefix) > len(rs) || !strings.EqualFold(string(rs[start:start+len(prefix)]), prefix) {
		return node{}, 0, false
	}
	end := start
	for end < len(rs) && !unicode.IsSpace(rs[end]) && rs[end] != '<' && rs[end] != '>' {
		end++
	}
	for end > start+len(prefix) && strings.ContainsRune(`.,;:!?'"*_~)`, rs[end-1]) {
		if rs[end-1] == ')' && strings.Count(string(rs[start:end]), "(") >= strings.Count(string(rs[start:end]), ")") {
			break
		}
		end--
	}
	raw := string(rs[start:end])
	href, ok := httpsURL(raw)
	if !ok {
		return node{}, 0, false
	}
	return node{kind: kLink, href: href, children: []node{{kind: kText, text: raw}}}, end, true
}
