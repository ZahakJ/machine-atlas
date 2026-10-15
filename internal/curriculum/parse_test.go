package curriculum

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// parseBody parses a lesson whose How it works section holds body, and
// returns that section's blocks.
func parseBody(t *testing.T, body string) []Block {
	t.Helper()
	l, err := Parse("lessons/alpha.md", []byte(lessonDoc(nil, fullBody(body))))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range l.Sections {
		if s.ID == "how-it-works" {
			return s.Blocks
		}
	}
	t.Fatal("no how-it-works section")
	return nil
}

func TestParseSectionsAndMeta(t *testing.T) {
	l, err := Parse("lessons/alpha.md", []byte(lessonDoc(map[string]any{
		"relevance": []any{map[string]any{"when": "derived.numa.multi", "note": "n"}, map[string]any{"when": "derived.paging.levels", "is": 5, "note": "n"}},
	}, fullBody("Text."))))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range l.Sections {
		ids = append(ids, s.ID)
	}
	want := []string{"question", "why-it-exists", "how-it-works", "on-this-machine", "predict", "try-it", "limits", "go-deeper"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("sections %v, want %v", ids, want)
	}
	if l.ID != "alpha" || l.Kind != "core" || len(l.References) != 1 || len(l.Relevance) != 2 {
		t.Errorf("meta %+v", l.Meta)
	}
	if e, _ := expectOf(l.Relevance[1].Is); e == nil || *e != "5" {
		t.Errorf("relevance is = %v, want 5", e)
	}
}

func TestParseMarkdown(t *testing.T) {
	blocks := parseBody(t, strings.Join([]string{
		"### Heading with `code`",
		"A paragraph with *em*, **strong**, `code`,",
		"a [link](https://docs.kernel.org/mm/) and [a lesson](lesson:page-faults).",
		"",
		"- one",
		"  continued",
		"  - nested",
		"- two",
		"",
		"1. first",
		"2. second",
		"",
		"| a | b |",
		"|---|:-:|",
		"| `x` | {{obs:kernel.arch}} |",
		"",
		"> quoted *text*",
		"> more",
		"",
		"```sh",
		"cat /proc/meminfo",
		"  indented",
		"```",
	}, "\n"))
	types := []string{}
	for _, b := range blocks {
		types = append(types, b.T)
	}
	if want := []string{"h", "p", "ul", "ol", "table", "quote", "code"}; !reflect.DeepEqual(types, want) {
		t.Fatalf("block types %v, want %v", types, want)
	}
	h, p, ul, table, quote, code := blocks[0], blocks[1], blocks[2], blocks[4], blocks[5], blocks[6]
	if h.Level != 3 || h.Inl[1].T != "code" {
		t.Errorf("heading %+v", h)
	}
	gotKinds := []string{}
	for _, x := range p.Inl {
		gotKinds = append(gotKinds, x.T+":"+x.Kind)
	}
	wantKinds := []string{"text:", "em:", "text:", "strong:", "text:", "code:", "text:", "link:url", "text:", "link:lesson", "text:"}
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Errorf("paragraph inlines %v, want %v", gotKinds, wantKinds)
	}
	if len(ul.Items) != 2 || inlineText(ul.Items[0][0].Inl) != "one continued" || ul.Items[0][1].T != "ul" {
		t.Errorf("list %+v", ul)
	}
	if len(table.Head) != 2 || len(table.TableRows) != 1 || table.TableRows[0][1][0].T != "chip" {
		t.Errorf("table %+v", table)
	}
	if len(quote.Blocks) != 1 || inlineText(quote.Blocks[0].Inl) != "quoted text more" {
		t.Errorf("quote %+v", quote)
	}
	if code.Lang != "sh" || code.Text != "cat /proc/meminfo\n  indented" {
		t.Errorf("code %+v", code)
	}
}

func TestParseDirectives(t *testing.T) {
	blocks := parseBody(t, strings.Join([]string{
		"Value {{obs:mem.meminfo.MemTotal|bytes}} and {{derived:derived.swap.configured}}.",
		":::when derived.thp.mode is always",
		"Always.",
		":::else",
		"Not always.",
		":::unknown",
		"Cannot tell.",
		":::end",
		":::when derived.swap.configured",
		"Swap.",
		":::end",
		":::evidence Memory",
		"- mem.meminfo.MemTotal",
		"- derived.mem.total_pages | Frames",
		":::end",
		":::table mem.zones columns=node,zone,free limit=5",
		":::diagram zone-watermarks",
		":::sim reclaim",
		":::experiment fault-count",
		":::misconception Free memory is wasted",
		"It is not.",
		":::end",
		":::predict",
		"How many?",
		"- {{obs:kernel.page_size_bytes}}",
		"- none",
		":::reveal",
		":::when derived.thp.enabled",
		"Few.",
		":::end",
		":::end",
	}, "\n"))
	types := []string{}
	for _, b := range blocks {
		types = append(types, b.T)
	}
	want := []string{"p", "branch", "branch", "evidence", "datatable", "diagram", "sim", "experiment", "callout", "predict"}
	if !reflect.DeepEqual(types, want) {
		t.Fatalf("block types %v, want %v", types, want)
	}
	chip := blocks[0].Inl[1]
	if chip.T != "chip" || chip.Kind != RefObs || chip.Ref != "mem.meminfo.MemTotal" || chip.Fmt != "bytes" {
		t.Errorf("chip %+v", chip)
	}
	br := blocks[1]
	if br.Fact != "derived.thp.mode" || br.Expect == nil || *br.Expect != "always" || len(br.Then) != 1 || len(br.Else) != 1 || len(br.Unknown) != 1 {
		t.Errorf("branch %+v", br)
	}
	if b := blocks[2]; b.Expect != nil || b.Else != nil || b.Unknown != nil {
		t.Errorf("truthy branch %+v", b)
	}
	ev := blocks[3]
	if ev.Title != "Memory" || len(ev.Evidence) != 2 || ev.Evidence[1].Kind != RefDerived || ev.Evidence[1].Caption != "Frames" {
		t.Errorf("evidence %+v", ev)
	}
	dt := blocks[4]
	if dt.Ref != "mem.zones" || !reflect.DeepEqual(dt.Columns, []string{"node", "zone", "free"}) || dt.Limit != 5 {
		t.Errorf("datatable %+v", dt)
	}
	if blocks[5].Name != "zone-watermarks" || blocks[6].Name != "reclaim" || blocks[7].Name != "fault-count" {
		t.Errorf("named blocks %+v %+v %+v", blocks[5], blocks[6], blocks[7])
	}
	if c := blocks[8]; c.Kind != "misconception" || c.Title != "Free memory is wasted" || len(c.Blocks) != 1 {
		t.Errorf("callout %+v", c)
	}
	pr := blocks[9]
	if len(pr.Question) != 1 || len(pr.Options) != 2 || pr.Options[0][0].T != "chip" || len(pr.Reveal) != 1 || pr.Reveal[0].T != "branch" {
		t.Errorf("predict %+v", pr)
	}
}

func TestRawHTMLStaysText(t *testing.T) {
	blocks := parseBody(t, `<script>alert(1)</script> and <b>bold</b> & [x](lesson:a-b)`)
	if blocks[0].T != "p" || blocks[0].Inl[0].T != "text" || blocks[0].Inl[0].S != "<script>alert(1)</script> and <b>bold</b> & " {
		t.Fatalf("raw HTML became %+v", blocks[0].Inl)
	}
	if len(blocks[0].Inl) != 2 || blocks[0].Inl[1].T != "link" {
		t.Errorf("only the Markdown link should be structure: %+v", blocks[0].Inl)
	}
}

func TestParseErrors(t *testing.T) {
	// Line numbers: the front matter of lessonDoc(nil, …) occupies lines 1
	// to frontLines, so body line k is file line frontLines+k; fullBody puts
	// "## How it works" on body line 7 and the inserted text after it.
	frontLines := strings.Count(lessonDoc(nil, ""), "\n")
	how := frontLines + 7
	tests := []struct {
		name string
		doc  string
		line int
		msg  string
	}{
		{"no front matter", "## Question\n", 1, "starts with a ---"},
		{"unclosed front matter", "---\n{}\n", 1, "not closed"},
		{"bad JSON", "---\n{\n\"id\": \"a\",,\n}\n---\n", 3, "front matter"},
		{"unknown field", lessonDoc(map[string]any{"colour": "red"}, fullBody("x")), 0, "unknown field"},
		{"bad relevance is", lessonDoc(map[string]any{"relevance": []any{map[string]any{"when": "a.b", "is": []int{1}, "note": "n"}}}, fullBody("x")), 0, "relevance[0].is"},
		{"unknown section", lessonDoc(nil, "## Question\n\n## Background\n"), frontLines + 3, "unknown section"},
		{"section order", lessonDoc(nil, "## Limits\n\n## Question\n"), frontLines + 3, "out of order"},
		{"text before section", lessonDoc(nil, "hello\n## Question\n"), frontLines + 1, "before the first"},
		{"unknown directive", lessonDoc(nil, fullBody(":::frobnicate")), how + 1, "unknown directive"},
		{"unclosed when", lessonDoc(nil, "## Question\n:::when kernel.arch\nx\n"), frontLines + 2, "end of file before :::end"},
		{"stray end", lessonDoc(nil, fullBody(":::end")), how + 1, "without a matching"},
		{"nesting too deep", lessonDoc(nil, fullBody(":::when a.b\n:::when a.c\n:::when a.d\nx\n:::end\n:::end\n:::end")), how + 3, "nested too deeply"},
		{"bad chip kind", lessonDoc(nil, fullBody("{{value:kernel.arch}}")), how + 1, "must be {{obs:ID}}"},
		{"derived chip as obs", lessonDoc(nil, fullBody("{{obs:derived.swap.configured}}")), how + 1, "derived ids"},
		{"unknown format", lessonDoc(nil, fullBody("{{obs:kernel.arch|fancy}}")), how + 1, "unknown format"},
		{"unclosed chip", lessonDoc(nil, fullBody("{{obs:kernel.arch")), how + 1, "not closed"},
		{"bad link target", lessonDoc(nil, fullBody("[x](javascript:alert(1))")), how + 1, "link target"},
		{"http link", lessonDoc(nil, fullBody("[x](http://example.com)")), how + 1, "link target"},
		{"evidence row", lessonDoc(nil, fullBody(":::evidence T\nkernel.arch\n:::end")), how + 2, "rows are"},
		{"evidence title", lessonDoc(nil, fullBody(":::evidence\n- kernel.arch\n:::end")), how + 1, "needs a title"},
		{"table argument", lessonDoc(nil, fullBody(":::table mem.zones rows=3")), how + 1, "unknown argument"},
		{"diagram without name", lessonDoc(nil, fullBody(":::diagram")), how + 1, "exactly one name"},
		{"predict without options", lessonDoc(nil, fullBody(":::predict\nQ?\n:::reveal\nA.\n:::end")), how + 1, "option"},
		{"predict without reveal", lessonDoc(nil, "## Question\n:::predict\nQ?\n- a\n:::end\n"), frontLines + 5, "without a matching"},
		{"misconception title", lessonDoc(nil, fullBody(":::misconception\nx\n:::end")), how + 1, "needs a title"},
		{"table separator", lessonDoc(nil, fullBody("| a | b |\n| c | d |")), how + 2, "separator"},
		{"table width", lessonDoc(nil, fullBody("| a | b |\n|---|---|\n| c |")), how + 3, "cells"},
		{"unclosed fence", lessonDoc(nil, "## Question\n```\ncode\n"), frontLines + 2, "not closed"},
		{"deep list", lessonDoc(nil, fullBody("- a\n  - b\n    - c")), how + 3, "one level"},
		{"h2 inside directive", lessonDoc(nil, "## Question\n:::cannot\n## Limits\n"), frontLines + 3, "section heading"},
		{"h1 heading", lessonDoc(nil, fullBody("# Big")), how + 1, "### and ####"},
	}
	for _, tt := range tests {
		_, err := Parse("lessons/alpha.md", []byte(tt.doc))
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("%s: error %v, want a ParseError", tt.name, err)
			continue
		}
		if pe.File != "lessons/alpha.md" || !strings.Contains(pe.Msg, tt.msg) || (tt.line > 0 && pe.Line != tt.line) {
			t.Errorf("%s: %v, want line %d containing %q", tt.name, err, tt.line, tt.msg)
		}
	}
}
