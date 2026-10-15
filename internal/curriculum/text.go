package curriculum

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const textWidth = 78

// RenderText renders a bound lesson as plain text for the terminal. Every
// paragraph, list item and evidence row carries its label as a badge
// ([OBSERVED], [DERIVED], [GENERAL], [SIMULATED]); branches show the text
// chosen for this machine; diagrams, simulations and experiments are named
// with where to find them, since a terminal cannot show them.
func RenderText(b *BoundLesson) string {
	r := &textRenderer{}
	r.line(b.Title)
	r.line(strings.Repeat("=", utf8.RuneCountInString(b.Title)))
	r.wrap("", "", b.Summary)
	r.blank()
	if len(b.Prerequisites) > 0 {
		r.line("Before this: " + strings.Join(b.Prerequisites, ", "))
	}
	r.line(fmt.Sprintf("Evidence on this machine: %d of %d ids available", b.Coverage.OK, b.Coverage.Total))
	for _, s := range b.Sections {
		r.blank()
		r.line(s.Title)
		r.line(strings.Repeat("-", utf8.RuneCountInString(s.Title)))
		r.blocks(s.Blocks, "")
	}
	if len(b.VersionNotes) > 0 {
		r.blank()
		r.line("Version notes")
		for _, v := range b.VersionNotes {
			r.wrap("- ", "  ", v.Applies+": "+v.Text)
		}
	}
	if len(b.References) > 0 {
		r.blank()
		r.line("References")
		for _, ref := range b.References {
			r.wrap("- ", "  ", ref.Title+" <"+ref.URL+">")
		}
	}
	if len(b.Next)+len(b.Deeper) > 0 {
		r.blank()
		if len(b.Next) > 0 {
			r.line("Next: " + strings.Join(b.Next, ", "))
		}
		if len(b.Deeper) > 0 {
			r.line("Deeper: " + strings.Join(b.Deeper, ", "))
		}
	}
	return terminalSafe(r.String())
}

// terminalSafe replaces control characters other than newlines, so strings
// from an imported snapshot cannot move the cursor or recolour a terminal.
func terminalSafe(s string) string {
	return strings.Map(func(c rune) rune {
		if c != '\n' && unicode.IsControl(c) {
			return utf8.RuneError
		}
		return c
	}, s)
}

type textRenderer struct {
	strings.Builder
	lastBlank bool
}

func (r *textRenderer) line(s string) {
	r.WriteString(s)
	r.WriteByte('\n')
	r.lastBlank = s == ""
}

func (r *textRenderer) blank() {
	if !r.lastBlank {
		r.line("")
	}
}

// wrap writes s word-wrapped, the first line after first and the rest
// after rest.
func (r *textRenderer) wrap(first, rest, s string) {
	prefix := first
	cur := prefix
	// Split on ASCII spaces only: values keep their thin spaces (8 192).
	for _, w := range strings.Split(s, " ") {
		if w == "" {
			continue
		}
		if utf8.RuneCountInString(cur)+1+utf8.RuneCountInString(w) > textWidth && cur != prefix {
			r.line(cur)
			prefix = rest
			cur = prefix
		}
		if cur != prefix {
			cur += " "
		}
		cur += w
	}
	r.line(cur)
}

func badge(label string) string { return "[" + strings.ToUpper(label) + "] " }

func (r *textRenderer) blocks(bs []Block, indent string) {
	for _, b := range bs {
		r.block(b, indent)
	}
}

func (r *textRenderer) block(b Block, in string) {
	pad := strings.Repeat(" ", utf8.RuneCountInString(badge(b.Label)))
	switch b.T {
	case "p":
		r.blank()
		r.wrap(in+badge(b.Label), in+pad, inlineText(b.Inl))
	case "h":
		r.blank()
		r.line(in + inlineText(b.Inl))
	case "ul", "ol":
		r.blank()
		for i, item := range b.Items {
			marker := "- "
			if b.T == "ol" {
				marker = fmt.Sprintf("%d. ", i+1)
			}
			for j, sub := range item {
				if j == 0 && sub.T == "p" {
					r.wrap(in+marker+badge(sub.Label), in+strings.Repeat(" ", len(marker))+pad, inlineText(sub.Inl))
					continue
				}
				r.block(sub, in+strings.Repeat(" ", len(marker)))
			}
		}
	case "code":
		r.blank()
		for _, l := range strings.Split(b.Text, "\n") {
			r.line(in + "    " + l)
		}
	case "table":
		r.blank()
		r.line(in + badge(b.Label) + cellsText(b.Head))
		for _, row := range b.TableRows {
			r.line(in + pad + cellsText(row))
		}
	case "quote":
		r.blank()
		r.blocks(b.Blocks, in+"> ")
	case "evidence":
		r.blank()
		r.line(in + "Evidence: " + b.Title)
		for _, e := range b.Evidence {
			r.wrap(in+"  "+badge(e.Label), in+"    ", fmt.Sprintf("%s: %s (%s; %s)", e.Caption, e.Display, e.Ref, e.Source))
		}
	case "datatable":
		r.blank()
		r.datatable(b, in)
	case "diagram":
		r.blank()
		r.wrap(in+badge(b.Label), in+pad, "Diagram "+b.Name+": open this lesson in the web interface (atlas serve) to see it.")
	case "sim":
		r.blank()
		r.wrap(in+badge(b.Label), in+pad, "Simulation "+b.Name+": open this lesson in the web interface (atlas serve) to run it.")
	case "experiment":
		r.blank()
		r.wrap(in+badge(b.Label), in+pad, "Experiment "+b.Name+": run it with atlas experiment run "+b.Name+" (it asks before it starts).")
	case "predict":
		r.blocks(b.Question, in+"  ")
		r.blank()
		for _, o := range b.Options {
			r.wrap(in+"  ( ) ", in+"      ", inlineText(o))
		}
		r.blank()
		r.line(in + "  Reveal")
		r.blocks(b.Reveal, in+"  ")
	case "callout":
		r.blank()
		head := "Misconception: "
		if b.Kind == "cannot" {
			head = "Cannot establish: "
		}
		r.line(in + head + b.Title)
		r.blocks(b.Blocks, in+"  ")
	case "branch":
		switch b.Chosen {
		case "then":
			r.blocks(b.Then, in)
		case "else":
			r.blocks(b.Else, in)
		default:
			r.blocks(b.Unknown, in)
		}
	}
}

func (r *textRenderer) datatable(b Block, in string) {
	head := fmt.Sprintf("Table %s", b.Ref)
	if b.Status != "ok" {
		r.line(in + badge(b.Label) + head + ": " + b.Display)
		return
	}
	if len(b.Rows) < b.Total {
		head += fmt.Sprintf(" (first %d of %d rows)", len(b.Rows), b.Total)
	}
	r.line(in + badge(b.Label) + head)
	widths := make([]int, len(b.Columns))
	for i, c := range b.Columns {
		widths[i] = utf8.RuneCountInString(c)
		for _, row := range b.Rows {
			widths[i] = max(widths[i], utf8.RuneCountInString(row[i]))
		}
	}
	format := func(cells []string) string {
		var s strings.Builder
		for i, c := range cells {
			s.WriteString(c)
			if i < len(cells)-1 {
				s.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(c)+2))
			}
		}
		return s.String()
	}
	r.line(in + "  " + format(b.Columns))
	for _, row := range b.Rows {
		r.line(in + "  " + format(row))
	}
}

func cellsText(cells [][]Inline) string {
	parts := make([]string, len(cells))
	for i, c := range cells {
		parts[i] = inlineText(c)
	}
	return strings.Join(parts, " | ")
}

// inlineText flattens inlines to plain text: chips become their display,
// links show where they lead.
func inlineText(xs []Inline) string {
	var b strings.Builder
	for _, x := range xs {
		switch x.T {
		case "text":
			b.WriteString(x.S)
		case "code":
			b.WriteString("`" + x.S + "`")
		case "em", "strong":
			b.WriteString(inlineText(x.Inl))
		case "chip":
			b.WriteString(x.Display)
		case "link":
			text := inlineText(x.Inl)
			switch x.Kind {
			case LinkURL:
				b.WriteString(text + " <" + x.Href + ">")
			default:
				b.WriteString(text + " (" + x.Kind + " " + x.Href + ")")
			}
		}
	}
	return b.String()
}
