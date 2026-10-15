package curriculum

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ZahakJ/machine-atlas/internal/obs"
)

// Link kinds.
const (
	LinkURL       = "url"
	LinkLesson    = "lesson"
	LinkComponent = "component"
	LinkObs       = "obs"
)

// parseInline parses the inline Markdown subset of one logical line:
// `code`, *em*, **strong**, [text](target) and {{obs:ID|fmt}} chips.
// Markers that are not closed stay literal text, and so does anything that
// looks like HTML: lesson text is never markup.
func parseInline(s string, line int, file string) ([]Inline, error) {
	ip := inlineParser{file: file, line: line}
	out, err := ip.parse(s)
	return mergeText(out), err
}

type inlineParser struct {
	file string
	line int
}

func (ip inlineParser) errorf(format string, args ...any) error {
	return &ParseError{ip.file, ip.line, fmt.Sprintf(format, args...)}
}

func (ip inlineParser) parse(s string) ([]Inline, error) {
	var out []Inline
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			out = append(out, Inline{T: "text", S: text.String()})
			text.Reset()
		}
	}
	for i := 0; i < len(s); {
		rest := s[i:]
		switch {
		case rest[0] == '\\' && len(rest) > 1 && strings.ContainsRune("\\`*[]{}|()", rune(rest[1])):
			text.WriteByte(rest[1])
			i += 2
			continue
		case rest[0] == '`':
			if end := strings.IndexByte(rest[1:], '`'); end >= 0 {
				flush()
				out = append(out, Inline{T: "code", S: rest[1 : 1+end]})
				i += end + 2
				continue
			}
		case strings.HasPrefix(rest, "{{"):
			end := strings.Index(rest, "}}")
			if end < 0 {
				return nil, ip.errorf("chip {{… is not closed by }}")
			}
			chip, err := ip.chip(rest[2:end])
			if err != nil {
				return nil, err
			}
			flush()
			out = append(out, chip)
			i += end + 2
			continue
		case strings.HasPrefix(rest, "**"):
			if end := strings.Index(rest[2:], "**"); end > 0 {
				inner, err := ip.parse(rest[2 : 2+end])
				if err != nil {
					return nil, err
				}
				flush()
				out = append(out, Inline{T: "strong", Inl: mergeText(inner)})
				i += end + 4
				continue
			}
		case rest[0] == '*':
			if end := closingStar(rest[1:]); end > 0 {
				inner, err := ip.parse(rest[1 : 1+end])
				if err != nil {
					return nil, err
				}
				flush()
				out = append(out, Inline{T: "em", Inl: mergeText(inner)})
				i += end + 2
				continue
			}
		case rest[0] == '[':
			if link, n, err := ip.link(rest); err != nil {
				return nil, err
			} else if n > 0 {
				flush()
				out = append(out, link)
				i += n
				continue
			}
		}
		text.WriteByte(rest[0])
		i++
	}
	flush()
	return out, nil
}

// closingStar finds the * that closes an emphasis, skipping ** pairs.
func closingStar(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] != '*' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '*' {
			i++
			continue
		}
		return i
	}
	return -1
}

// link parses [text](target) at the start of s and returns the number of
// bytes consumed, or 0 if s does not start a link.
func (ip inlineParser) link(s string) (Inline, int, error) {
	depth, mid := 0, -1
	for i := 0; i < len(s) && mid < 0; i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				if i+1 < len(s) && s[i+1] == '(' {
					mid = i
				} else {
					return Inline{}, 0, nil
				}
			}
		}
	}
	if mid < 0 {
		return Inline{}, 0, nil
	}
	end := strings.IndexByte(s[mid+2:], ')')
	if end < 0 {
		return Inline{}, 0, nil
	}
	target := s[mid+2 : mid+2+end]
	kind, href, err := ip.target(target)
	if err != nil {
		return Inline{}, 0, err
	}
	inner, err := ip.parse(s[1:mid])
	if err != nil {
		return Inline{}, 0, err
	}
	return Inline{T: "link", Kind: kind, Href: href, Inl: mergeText(inner)}, mid + 3 + end, nil
}

// target validates a link target: an https URL, or lesson:, component: or
// obs: followed by an id. Anything else, including http: and javascript:,
// is an error rather than a silently dead link.
func (ip inlineParser) target(t string) (kind, href string, err error) {
	if strings.HasPrefix(t, "https://") && len(t) > len("https://") && !strings.ContainsAny(t, " \"'<>") {
		return LinkURL, t, nil
	}
	scheme, id, ok := strings.Cut(t, ":")
	if ok {
		switch scheme {
		case LinkLesson, LinkComponent:
			if lessonID.MatchString(id) {
				return scheme, id, nil
			}
		case LinkObs:
			if obs.ValidID(id) {
				return scheme, id, nil
			}
		}
	}
	return "", "", ip.errorf("link target %q is not https://…, lesson:ID, component:ID or obs:ID", t)
}

// chip parses the inside of {{kind:ID|fmt}}.
func (ip inlineParser) chip(s string) (Inline, error) {
	ref, format, _ := strings.Cut(s, "|")
	kind, id, ok := strings.Cut(strings.TrimSpace(ref), ":")
	format = strings.TrimSpace(format)
	switch {
	case !ok || (kind != RefObs && kind != RefDerived):
		return Inline{}, ip.errorf("chip {{%s}} must be {{obs:ID}} or {{derived:ID}}", s)
	case !obs.ValidID(id):
		return Inline{}, ip.errorf("chip {{%s}}: %q is not a valid id", s, id)
	case (kind == RefDerived) != strings.HasPrefix(id, "derived."):
		return Inline{}, ip.errorf("chip {{%s}}: derived ids start with derived. and use {{derived:…}}; observations use {{obs:…}}", s)
	case format != "" && !slices.Contains(formats, format):
		return Inline{}, ip.errorf("chip {{%s}}: unknown format %q (one of %s)", s, format, strings.Join(formats, ", "))
	}
	return Inline{T: "chip", Kind: kind, Ref: id, Fmt: format}, nil
}

// mergeText joins adjacent text runs, which the parser produces around
// markers that turned out not to be markup.
func mergeText(in []Inline) []Inline {
	var out []Inline
	for _, x := range in {
		if x.T == "text" && len(out) > 0 && out[len(out)-1].T == "text" {
			out[len(out)-1].S += x.S
			continue
		}
		out = append(out, x)
	}
	return out
}
