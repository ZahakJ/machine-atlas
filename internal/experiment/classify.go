package experiment

import (
	"encoding/json"
	"fmt"
)

// Outcome rules. They are pure functions of the measurements, so a result
// can be re-checked by hand from its numbers, and each outcome is reachable
// in tests from synthetic measurements. The thresholds are deliberately
// loose: the rule tells patterns apart; it never promises an exact count.

// Rule texts, recorded in the result's notes.
const (
	faultRule = "Rule: pages = region size / base page size. Region B faults per page when its first-touch minor faults are at least 90% of pages. " +
		"Region A faults per page when its first-touch faults are at least 90% of pages and it has no AnonHugePages; it is huge when its faults are at most 10% of pages and AnonHugePages cover at least 90% of it. " +
		"B not per page: unexpected; A per page: small-pages; A huge: huge-pages; A with any AnonHugePages: partial-huge; otherwise unexpected."
	residencyRule = "Rule: on tmpfs or ramfs with at least 90% resident after the advice: not-evictable. Otherwise, when at least 95% is resident after the cold read: " +
		"at most 5% resident after the advice is evicted-then-cached, under 95% is partially-evicted. Anything else is unexpected."
)

// classify returns the outcome id that matches ms for experiment id.
func classify(id string, ms []Measurement) string {
	switch id {
	case idFaultCount:
		return classifyFaultCount(ms)
	case idResidency:
		return classifyResidency(ms)
	}
	return "unexpected"
}

func classifyFaultCount(ms []Measurement) string {
	v := values(ms)
	pages, ok1 := v.num("region_pages")
	aFaults, ok2 := v.num("a.first_touch.minflt")
	aHuge, ok3 := v.num("a.first_touch.anon_huge_bytes")
	bFaults, ok4 := v.num("b.first_touch.minflt")
	size, ok5 := v.num("region_bytes")
	if !(ok1 && ok2 && ok3 && ok4 && ok5) || pages <= 0 {
		return "unexpected"
	}
	switch {
	case bFaults < 0.9*pages:
		return "unexpected"
	case aFaults >= 0.9*pages && aHuge == 0:
		return "small-pages"
	case aFaults <= 0.1*pages && aHuge >= 0.9*size:
		return "huge-pages"
	case aHuge > 0:
		return "partial-huge"
	}
	return "unexpected"
}

func classifyResidency(ms []Measurement) string {
	v := values(ms)
	fs, _ := v["fs.type"].(string)
	advice, ok1 := v.num("residency.after_advice")
	cold, ok2 := v.num("residency.after_cold_read")
	if !ok1 || !ok2 {
		return "unexpected"
	}
	switch {
	case (fs == "tmpfs" || fs == "ramfs") && advice >= 90:
		return "not-evictable"
	case cold < 95:
		return "unexpected"
	case advice <= 5:
		return "evicted-then-cached"
	case advice < 95:
		return "partially-evicted"
	}
	return "unexpected"
}

// measured maps measurement ids to values.
type measured map[string]any

func values(ms []Measurement) measured {
	m := make(measured, len(ms))
	for _, x := range ms {
		m[x.ID] = x.Value
	}
	return m
}

// num returns a numeric value as float64, whichever type it arrived as: the
// child records int64, JSON decoding produces float64.
func (m measured) num(id string) (float64, bool) {
	switch n := m[id].(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// outcomeIDs lists the outcomes of info, for validation.
func outcomeIDs(info Info) map[string]bool {
	ids := map[string]bool{}
	for _, o := range info.Outcomes {
		ids[o.ID] = true
	}
	return ids
}

// ruleFor returns the rule text for id.
func ruleFor(id string) string {
	switch id {
	case idFaultCount:
		return faultRule
	case idResidency:
		return residencyRule
	}
	panic(fmt.Sprintf("experiment: no rule for %q", id))
}
