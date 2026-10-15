package curriculum

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// ParseError is a problem in a lesson file, located by file and line.
type ParseError struct {
	File string
	Line int
	Msg  string
}

func (e *ParseError) Error() string { return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Msg) }

// Names of the client-side pieces a lesson may embed. The web UI implements
// exactly these; lint rejects any other name.
var (
	Diagrams    = []string{"address-space-map", "meminfo-breakdown", "zone-watermarks", "probe-regions", "fault-flow", "lru-lists", "memory-layers", "cpu-topology", "packet-path", "io-stack"}
	Sims        = []string{"translation-walker", "reclaim", "fair-share"}
	Experiments = []string{"fault-count", "page-cache-residency"}
	// dataDiagrams draw observations of this machine; the others are
	// authored illustrations.
	dataDiagrams = []string{"address-space-map", "meminfo-breakdown", "zone-watermarks", "probe-regions", "cpu-topology", "packet-path", "io-stack"}
)

const defaultCannotTitle = "What these measurements cannot establish"

// maxDepth is how deeply block directives may nest: a directive inside a
// directive, such as a :::when inside a :::predict reveal, but no further.
const maxDepth = 2

var lessonID = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Parse reads one lesson file. Errors carry the file name and line.
func Parse(file string, data []byte) (*Lesson, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	raw := strings.Split(text, "\n")
	if len(raw) == 0 || raw[0] != "---" {
		return nil, &ParseError{file, 1, "a lesson starts with a --- line opening the JSON front matter"}
	}
	end := slices.Index(raw[1:], "---")
	if end < 0 {
		return nil, &ParseError{file, 1, "the front matter is not closed by a --- line"}
	}
	end++ // index in raw
	l := &Lesson{File: file}
	if err := decodeMeta(strings.Join(raw[1:end], "\n"), &l.Meta); err != nil {
		var pe *ParseError
		if errors.As(err, &pe) {
			pe.File, pe.Line = file, pe.Line+1
			return nil, pe
		}
		return nil, &ParseError{file, 2, err.Error()}
	}
	p := &parser{file: file}
	for i := end + 1; i < len(raw); i++ {
		p.lines = append(p.lines, srcLine{i + 1, strings.TrimRight(raw[i], " \t")})
	}
	sections, err := p.sections()
	if err != nil {
		return nil, err
	}
	l.Sections = sections
	return l, nil
}

// decodeMeta decodes the front matter strictly: unknown fields and
// trailing data are errors, so a typo cannot silently drop a prerequisite.
// A returned ParseError's Line is relative to the first JSON line.
func decodeMeta(js string, m *Meta) error {
	dec := json.NewDecoder(strings.NewReader(js))
	dec.DisallowUnknownFields()
	if err := dec.Decode(m); err != nil {
		line := 1
		var se *json.SyntaxError
		var te *json.UnmarshalTypeError
		switch {
		case errors.As(err, &se):
			line += strings.Count(js[:min(int(se.Offset), len(js))], "\n")
		case errors.As(err, &te):
			line += strings.Count(js[:min(int(te.Offset), len(js))], "\n")
		}
		return &ParseError{Line: line, Msg: "front matter: " + err.Error()}
	}
	if dec.More() {
		return &ParseError{Line: 1, Msg: "front matter: unexpected data after the JSON object"}
	}
	for i, r := range m.Relevance {
		if _, err := expectOf(r.Is); err != nil {
			return &ParseError{Line: 1, Msg: fmt.Sprintf("front matter: relevance[%d].is: %v", i, err)}
		}
	}
	return nil
}

// expectOf returns the expected value of a relevance rule as the string a
// value is compared with, or nil for "truthy".
func expectOf(raw json.RawMessage) (*string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case bool, float64:
		s = string(raw)
	default:
		return nil, fmt.Errorf("must be a string, number or boolean")
	}
	return &s, nil
}

type srcLine struct {
	n int
	s string
}

type parser struct {
	file  string
	lines []srcLine
	pos   int
}

func (p *parser) errorf(n int, format string, args ...any) error {
	return &ParseError{p.file, n, fmt.Sprintf(format, args...)}
}

func (p *parser) eof() bool { return p.pos >= len(p.lines) }

// sections splits the body at "## " headings, which must name the known
// sections in their fixed order.
func (p *parser) sections() ([]Section, error) {
	var out []Section
	next := 0 // index into sectionOrder of the earliest allowed section
	for !p.eof() {
		l := p.lines[p.pos]
		if l.s == "" {
			p.pos++
			continue
		}
		title, ok := strings.CutPrefix(l.s, "## ")
		if !ok {
			return nil, p.errorf(l.n, "text before the first ## section heading")
		}
		title = strings.TrimSpace(title)
		idx := slices.IndexFunc(sectionOrder, func(s struct{ id, title string }) bool { return s.title == title })
		switch {
		case idx < 0:
			return nil, p.errorf(l.n, "unknown section %q (sections: %s)", title, sectionTitles())
		case idx < next:
			return nil, p.errorf(l.n, "section %q is out of order or repeated (order: %s)", title, sectionTitles())
		}
		next = idx + 1
		p.pos++
		blocks, _, err := p.blocks(0, nil, 0)
		if err != nil {
			return nil, err
		}
		out = append(out, Section{ID: sectionOrder[idx].id, Title: title, Blocks: blocks})
	}
	return out, nil
}

func sectionTitles() string {
	t := make([]string, len(sectionOrder))
	for i, s := range sectionOrder {
		t[i] = s.title
	}
	return strings.Join(t, ", ")
}

// directive splits a ":::name args" line.
func directive(s string) (name, args string, ok bool) {
	rest, ok := strings.CutPrefix(s, ":::")
	if !ok {
		return "", "", false
	}
	name, args, _ = strings.Cut(strings.TrimSpace(rest), " ")
	return name, strings.TrimSpace(args), true
}

// blocks parses blocks until end of input, a "## " heading (top level
// only), or one of the closing directives in stops, which it consumes and
// returns. open is the line of the directive being parsed, for the error
// when its closing line never comes.
func (p *parser) blocks(depth int, stops []string, open int) ([]Block, srcLine, error) {
	var out []Block
	for !p.eof() {
		l := p.lines[p.pos]
		s := l.s
		switch {
		case strings.TrimSpace(s) == "":
			p.pos++
			continue
		case strings.HasPrefix(s, "## "):
			if depth > 0 || stops != nil {
				return nil, l, p.errorf(l.n, "section heading inside an open block directive (missing :::end?)")
			}
			return out, l, nil
		}
		if name, args, ok := directive(s); ok {
			if slices.Contains(stops, name) {
				p.pos++
				return out, l, nil
			}
			b, err := p.directiveBlock(l, name, args, depth)
			if err != nil {
				return nil, l, err
			}
			out = append(out, b)
			continue
		}
		b, err := p.markdownBlock(depth)
		if err != nil {
			return nil, l, err
		}
		out = append(out, b)
	}
	if stops != nil {
		return nil, srcLine{}, p.errorf(open, "end of file before :::%s closes the directive on this line", stops[len(stops)-1])
	}
	return out, srcLine{}, nil
}

func (p *parser) directiveBlock(l srcLine, name, args string, depth int) (Block, error) {
	container := func() error {
		if depth+1 > maxDepth {
			return p.errorf(l.n, ":::%s nested too deeply: block directives nest one level", name)
		}
		return nil
	}
	leafArg := func() (string, error) {
		if args == "" || strings.Contains(args, " ") {
			return "", p.errorf(l.n, ":::%s takes exactly one name", name)
		}
		return args, nil
	}
	p.pos++
	switch name {
	case "when":
		if err := container(); err != nil {
			return Block{}, err
		}
		return p.when(l, args, depth+1)
	case "predict":
		if err := container(); err != nil {
			return Block{}, err
		}
		if args != "" {
			return Block{}, p.errorf(l.n, ":::predict takes no arguments; write the question on the following lines")
		}
		return p.predict(l, depth+1)
	case "misconception", "cannot":
		if err := container(); err != nil {
			return Block{}, err
		}
		if name == "misconception" && args == "" {
			return Block{}, p.errorf(l.n, ":::misconception needs a title")
		}
		if args == "" {
			args = defaultCannotTitle
		}
		blocks, _, err := p.blocks(depth+1, []string{"end"}, l.n)
		if err != nil {
			return Block{}, err
		}
		return Block{T: "callout", Kind: name, Title: args, Blocks: blocks, Line: l.n}, nil
	case "evidence":
		return p.evidence(l, args)
	case "table":
		return p.datatable(l, args)
	case "diagram", "sim", "experiment":
		n, err := leafArg()
		return Block{T: name, Name: n, Line: l.n}, err
	case "end", "else", "unknown", "reveal":
		return Block{}, p.errorf(l.n, ":::%s without a matching opening directive", name)
	}
	return Block{}, p.errorf(l.n, "unknown directive :::%s", name)
}

func (p *parser) when(l srcLine, args string, depth int) (Block, error) {
	fact, value, hasValue := strings.Cut(args, " is ")
	fact = strings.TrimSpace(fact)
	if !obs.ValidID(fact) || strings.Contains(fact, " ") {
		return Block{}, p.errorf(l.n, ":::when needs an observation or derived id, optionally followed by \"is VALUE\"")
	}
	b := Block{T: "branch", Fact: fact, Line: l.n}
	if hasValue {
		v := strings.TrimSpace(value)
		if v == "" {
			return Block{}, p.errorf(l.n, ":::when %s is: missing value", fact)
		}
		b.Expect = &v
	}
	var err error
	var stop srcLine
	b.Then, stop, err = p.blocks(depth, []string{"else", "unknown", "end"}, l.n)
	if err != nil {
		return Block{}, err
	}
	name, _, _ := directive(stop.s)
	if name == "else" {
		if b.Else, stop, err = p.blocks(depth, []string{"unknown", "end"}, l.n); err != nil {
			return Block{}, err
		}
		name, _, _ = directive(stop.s)
	}
	if name == "unknown" {
		if b.Unknown, _, err = p.blocks(depth, []string{"end"}, l.n); err != nil {
			return Block{}, err
		}
	}
	return b, nil
}

func (p *parser) predict(l srcLine, depth int) (Block, error) {
	pre, _, err := p.blocks(depth, []string{"reveal"}, l.n)
	if err != nil {
		return Block{}, err
	}
	b := Block{T: "predict", Line: l.n}
	opts := slices.IndexFunc(pre, func(b Block) bool { return b.T == "ul" })
	if opts < 0 {
		return Block{}, p.errorf(l.n, ":::predict needs a \"- option\" list before :::reveal")
	}
	if opts == 0 {
		return Block{}, p.errorf(l.n, ":::predict needs a question before its options")
	}
	if opts != len(pre)-1 {
		return Block{}, p.errorf(pre[opts+1].Line, "nothing may follow the options of :::predict before :::reveal")
	}
	b.Question = pre[:opts]
	for _, item := range pre[opts].Items {
		if len(item) != 1 || item[0].T != "p" {
			return Block{}, p.errorf(pre[opts].Line, "each :::predict option is a single line of text")
		}
		b.Options = append(b.Options, item[0].Inl)
	}
	if b.Reveal, _, err = p.blocks(depth, []string{"end"}, l.n); err != nil {
		return Block{}, err
	}
	return b, nil
}

func (p *parser) evidence(l srcLine, title string) (Block, error) {
	if title == "" {
		return Block{}, p.errorf(l.n, ":::evidence needs a title")
	}
	b := Block{T: "evidence", Title: title, Line: l.n}
	for ; !p.eof(); p.pos++ {
		r := p.lines[p.pos]
		if strings.TrimSpace(r.s) == "" {
			continue
		}
		if name, _, ok := directive(r.s); ok && name == "end" {
			p.pos++
			if len(b.Evidence) == 0 {
				return Block{}, p.errorf(l.n, ":::evidence lists no ids")
			}
			return b, nil
		}
		item, ok := strings.CutPrefix(r.s, "- ")
		if !ok {
			return Block{}, p.errorf(r.n, ":::evidence rows are \"- ID\" or \"- ID | caption\"")
		}
		id, caption, _ := strings.Cut(item, "|")
		id = strings.TrimSpace(id)
		if !obs.ValidID(id) {
			return Block{}, p.errorf(r.n, "%q is not an observation or derived id", id)
		}
		b.Evidence = append(b.Evidence, EvidenceRow{Ref: id, Kind: refKind(id), Caption: strings.TrimSpace(caption)})
	}
	return Block{}, p.errorf(l.n, ":::evidence is not closed by :::end")
}

func (p *parser) datatable(l srcLine, args string) (Block, error) {
	fields := strings.Fields(args)
	if len(fields) == 0 || !obs.ValidID(fields[0]) {
		return Block{}, p.errorf(l.n, ":::table needs a table observation id")
	}
	b := Block{T: "datatable", Ref: fields[0], Line: l.n}
	for _, f := range fields[1:] {
		key, val, _ := strings.Cut(f, "=")
		switch key {
		case "columns":
			b.Columns = strings.Split(val, ",")
			if slices.Contains(b.Columns, "") {
				return Block{}, p.errorf(l.n, ":::table columns= lists an empty column name")
			}
		case "limit":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 {
				return Block{}, p.errorf(l.n, ":::table limit= must be a positive integer")
			}
			b.Limit = n
		default:
			return Block{}, p.errorf(l.n, ":::table: unknown argument %q (columns=a,b,c limit=N)", f)
		}
	}
	return b, nil
}

func refKind(id string) string {
	if strings.HasPrefix(id, "derived.") {
		return RefDerived
	}
	return RefObs
}

var (
	bulletItem  = regexp.MustCompile(`^- `)
	orderedItem = regexp.MustCompile(`^[0-9]+\. `)
)

// startsBlock reports whether a line begins a block other than a
// paragraph, which ends a paragraph in progress.
func startsBlock(s string) bool {
	return strings.HasPrefix(s, ":::") || strings.HasPrefix(s, "```") || strings.HasPrefix(s, "#") ||
		bulletItem.MatchString(s) || orderedItem.MatchString(s) || strings.HasPrefix(s, "|") || strings.HasPrefix(s, ">")
}

func (p *parser) markdownBlock(depth int) (Block, error) {
	l := p.lines[p.pos]
	s := l.s
	switch {
	case strings.HasPrefix(s, "```"):
		return p.fence(l)
	case strings.HasPrefix(s, "### "), strings.HasPrefix(s, "#### "):
		level := strings.Index(s, " ")
		inl, err := parseInline(strings.TrimSpace(s[level:]), l.n, p.file)
		p.pos++
		return Block{T: "h", Level: level, Inl: inl, Line: l.n}, err
	case strings.HasPrefix(s, "#"):
		return Block{}, p.errorf(l.n, "inside a section only ### and #### headings are allowed")
	case bulletItem.MatchString(s), orderedItem.MatchString(s):
		return p.list(0)
	case strings.HasPrefix(s, "|"):
		return p.table()
	case strings.HasPrefix(s, ">"):
		return p.quote(depth)
	}
	return p.paragraph()
}

func (p *parser) paragraph() (Block, error) {
	first := p.lines[p.pos]
	var parts []string
	for !p.eof() {
		s := p.lines[p.pos].s
		if strings.TrimSpace(s) == "" || (len(parts) > 0 && startsBlock(s)) {
			break
		}
		parts = append(parts, strings.TrimSpace(s))
		p.pos++
	}
	inl, err := parseInline(strings.Join(parts, " "), first.n, p.file)
	return Block{T: "p", Inl: inl, Line: first.n}, err
}

func (p *parser) fence(open srcLine) (Block, error) {
	lang := strings.TrimSpace(strings.TrimPrefix(open.s, "```"))
	p.pos++
	var body []string
	for ; !p.eof(); p.pos++ {
		if strings.TrimSpace(p.lines[p.pos].s) == "```" {
			p.pos++
			return Block{T: "code", Lang: lang, Text: strings.Join(body, "\n"), Line: open.n}, nil
		}
		body = append(body, p.lines[p.pos].s)
	}
	return Block{}, p.errorf(open.n, "code block is not closed by ```")
}

// list parses a "-" or "1." list whose markers are indented by indent
// spaces. Items continue on lines indented further; a list indented under
// an item is its nested list, one level deep.
func (p *parser) list(indent int) (Block, error) {
	first := p.lines[p.pos]
	marker := bulletItem
	b := Block{T: "ul", Line: first.n}
	if orderedItem.MatchString(first.s[indent:]) {
		marker, b.T = orderedItem, "ol"
	}
	for !p.eof() {
		l := p.lines[p.pos]
		if strings.TrimSpace(l.s) == "" {
			// A blank line ends the list unless the next item follows.
			k := p.pos + 1
			for k < len(p.lines) && strings.TrimSpace(p.lines[k].s) == "" {
				k++
			}
			if k < len(p.lines) && leading(p.lines[k].s) == indent && marker.MatchString(p.lines[k].s[indent:]) {
				p.pos = k
				continue
			}
			break
		}
		if leading(l.s) != indent || !marker.MatchString(l.s[indent:]) {
			break
		}
		text := marker.ReplaceAllString(l.s[indent:], "")
		p.pos++
		var nested *Block
		for !p.eof() {
			c := p.lines[p.pos]
			ind := leading(c.s)
			if strings.TrimSpace(c.s) == "" || ind <= indent {
				break
			}
			rest := c.s[ind:]
			if bulletItem.MatchString(rest) || orderedItem.MatchString(rest) {
				if indent > 0 {
					return Block{}, p.errorf(c.n, "lists nest one level only")
				}
				if nested != nil {
					return Block{}, p.errorf(c.n, "an item has one nested list")
				}
				nb, err := p.list(ind)
				if err != nil {
					return Block{}, err
				}
				nested = &nb
				continue
			}
			if nested != nil {
				return Block{}, p.errorf(c.n, "item text after a nested list; start a new item")
			}
			if strings.HasPrefix(rest, ":::") {
				return Block{}, p.errorf(c.n, "block directives cannot be placed inside a list item")
			}
			text += " " + strings.TrimSpace(rest)
			p.pos++
		}
		inl, err := parseInline(strings.TrimSpace(text), l.n, p.file)
		if err != nil {
			return Block{}, err
		}
		item := []Block{{T: "p", Inl: inl, Line: l.n}}
		if nested != nil {
			item = append(item, *nested)
		}
		b.Items = append(b.Items, item)
	}
	return b, nil
}

func leading(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

var separatorCell = regexp.MustCompile(`^:?-+:?$`)

func (p *parser) table() (Block, error) {
	head := p.lines[p.pos]
	cells := splitRow(head.s)
	p.pos++
	if p.eof() {
		return Block{}, p.errorf(head.n, "a table needs a |---| separator line after its header")
	}
	sep := p.lines[p.pos]
	sepCells := splitRow(sep.s)
	if len(sepCells) != len(cells) || slices.IndexFunc(sepCells, func(c string) bool { return !separatorCell.MatchString(c) }) >= 0 {
		return Block{}, p.errorf(sep.n, "a table needs a |---| separator line with one cell per header column")
	}
	p.pos++
	b := Block{T: "table", Line: head.n}
	row := func(l srcLine, cells []string) ([][]Inline, error) {
		if len(cells) != len(b.Head) && b.Head != nil {
			return nil, p.errorf(l.n, "table row has %d cells, the header has %d", len(cells), len(b.Head))
		}
		out := make([][]Inline, len(cells))
		for i, c := range cells {
			inl, err := parseInline(c, l.n, p.file)
			if err != nil {
				return nil, err
			}
			out[i] = inl
		}
		return out, nil
	}
	var err error
	if b.Head, err = row(head, cells); err != nil {
		return Block{}, err
	}
	for !p.eof() && strings.HasPrefix(p.lines[p.pos].s, "|") {
		l := p.lines[p.pos]
		r, err := row(l, splitRow(l.s))
		if err != nil {
			return Block{}, err
		}
		b.TableRows = append(b.TableRows, r)
		p.pos++
	}
	return b, nil
}

// splitRow splits "| a | b |" into trimmed cells; "\|" is a literal bar.
func splitRow(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "|")
	if strings.HasSuffix(s, "|") && !strings.HasSuffix(s, `\|`) {
		s = s[:len(s)-1]
	}
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == '|':
			cur.WriteByte('|')
			i++
		case s[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(s[i])
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

// quote parses consecutive "> " lines as the blocks of a quotation.
func (p *parser) quote(depth int) (Block, error) {
	first := p.lines[p.pos]
	sub := &parser{file: p.file}
	for ; !p.eof() && strings.HasPrefix(p.lines[p.pos].s, ">"); p.pos++ {
		l := p.lines[p.pos]
		text := strings.TrimPrefix(strings.TrimPrefix(l.s, ">"), " ")
		if strings.HasPrefix(text, ":::") {
			return Block{}, p.errorf(l.n, "block directives cannot be placed inside a quote")
		}
		sub.lines = append(sub.lines, srcLine{l.n, text})
	}
	blocks, _, err := sub.blocks(depth+1, nil, first.n)
	return Block{T: "quote", Blocks: blocks, Line: first.n}, err
}
