package curriculum

import (
	"maps"
	"slices"
)

// walk calls block for every block of the tree, depth first, including
// every branch of a :::when and both halves of a :::predict, and inline
// for every inline, including those nested in emphasis and links.
func walk(blocks []Block, block func(*Block), inline func(*Block, *Inline)) {
	var inl func(b *Block, xs []Inline)
	inl = func(b *Block, xs []Inline) {
		for i := range xs {
			inline(b, &xs[i])
			inl(b, xs[i].Inl)
		}
	}
	for i := range blocks {
		b := &blocks[i]
		block(b)
		inl(b, b.Inl)
		for _, item := range b.Items {
			walk(item, block, inline)
		}
		for _, cells := range append(slices.Clone(b.TableRows), b.Head) {
			for _, c := range cells {
				inl(b, c)
			}
		}
		for _, o := range b.Options {
			inl(b, o)
		}
		for _, sub := range [][]Block{b.Blocks, b.Question, b.Reveal, b.Then, b.Else, b.Unknown} {
			walk(sub, block, inline)
		}
	}
}

// references returns the observation and derived ids a parsed lesson's
// text refers to, sorted: chips, obs: links, evidence rows, data tables
// and branch conditions. It is the set "uses" must equal.
func references(l *Lesson) []string {
	ids := map[string]bool{}
	for _, s := range l.Sections {
		walk(s.Blocks, func(b *Block) {
			switch b.T {
			case "evidence":
				for _, r := range b.Evidence {
					ids[r.Ref] = true
				}
			case "datatable":
				ids[b.Ref] = true
			case "branch":
				ids[b.Fact] = true
			}
		}, func(_ *Block, x *Inline) {
			switch {
			case x.T == "chip":
				ids[x.Ref] = true
			case x.T == "link" && x.Kind == LinkObs:
				ids[x.Href] = true
			}
		})
	}
	return slices.Sorted(maps.Keys(ids))
}
