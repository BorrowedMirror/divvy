package core

// Port of crates/core/benches/pipeline.rs: clipboard text -> parsed item -> matched stats ->
// trade query.
//
// The Rust benches match against the real bundled catalog from rkl-data; until its Go port
// exists they use the synthetic corpus catalog of corpus_test.go (every corpus template
// known), which exercises the same code paths with cheaper lookups.
// BenchmarkPipelineP95 reports the per-item p50/p95/p99 of the full pipeline so the
// "< 1 ms p95" budget can be checked directly.

import (
	"slices"
	"testing"
	"time"
)

type benchFixture struct {
	corpus  []corpusItem
	catalog Catalog
	opts    *QueryOptions
	parsed  []*ParsedItem
	matched []*ParsedItem
}

func newBenchFixture(b *testing.B) *benchFixture {
	corpus := loadCorpus(b)
	f := &benchFixture{
		corpus: corpus,
		opts:   &QueryOptions{League: "Standard", Status: StatusOnline, WidenPct: 10, EnableAllStats: false},
	}
	for _, it := range corpus {
		f.parsed = append(f.parsed, mustParse(b, it.text))
	}
	f.catalog = newCorpusCatalog(f.parsed)
	for _, item := range f.parsed {
		m := item.Clone()
		NewStatMatcher(f.catalog).MatchItem(m)
		f.matched = append(f.matched, m)
	}
	return f
}

var sink any

func (f *benchFixture) pipeline(text string) {
	item, err := ParseItem(text)
	if err != nil {
		panic(err)
	}
	outcomes := NewStatMatcher(f.catalog).MatchItem(item)
	filters := BuildFilters(item, f.opts)
	sink = []any{outcomes, filters.ToTradeQuery()}
}

func BenchmarkParseItemCorpus(b *testing.B) {
	f := newBenchFixture(b)
	for b.Loop() {
		for _, it := range f.corpus {
			item, err := ParseItem(it.text)
			if err != nil {
				b.Fatal(err)
			}
			sink = item
		}
	}
}

func BenchmarkTemplatizeLine(b *testing.B) {
	for b.Loop() {
		sink = Templatize("+42% increased Physical Damage")
	}
}

func BenchmarkMatchItemCorpus(b *testing.B) {
	f := newBenchFixture(b)
	for b.Loop() {
		b.StopTimer()
		items := make([]*ParsedItem, len(f.parsed))
		for i, it := range f.parsed {
			items[i] = it.Clone()
		}
		b.StartTimer()
		matcher := NewStatMatcher(f.catalog)
		for _, item := range items {
			sink = matcher.MatchItem(item)
		}
	}
}

func BenchmarkBuildFiltersToTradeQueryCorpus(b *testing.B) {
	f := newBenchFixture(b)
	for b.Loop() {
		for _, item := range f.matched {
			fs := BuildFilters(item, f.opts)
			sink = fs.ToTradeQuery()
		}
	}
}

func BenchmarkPipelineCorpus(b *testing.B) {
	f := newBenchFixture(b)
	for b.Loop() {
		for _, it := range f.corpus {
			f.pipeline(it.text)
		}
	}
}

// BenchmarkPipelineP95 is the per-item latency distribution of the full pipeline over the
// corpus (200 passes per benchmark iteration), reported as p50/p95/p99/max metrics.
func BenchmarkPipelineP95(b *testing.B) {
	f := newBenchFixture(b)
	type sample struct {
		d  time.Duration
		id string
	}
	var samples []sample
	for b.Loop() {
		samples = samples[:0]
		for range 200 {
			for _, it := range f.corpus {
				start := time.Now()
				f.pipeline(it.text)
				samples = append(samples, sample{time.Since(start), it.entry.ID})
			}
		}
	}
	slices.SortFunc(samples, func(a, c sample) int { return int(a.d - c.d) })
	at := func(q float64) time.Duration { return samples[int(float64(len(samples)-1)*q)].d }
	worst := samples[len(samples)-1]
	b.ReportMetric(float64(at(0.5).Nanoseconds()), "p50-ns/item")
	b.ReportMetric(float64(at(0.95).Nanoseconds()), "p95-ns/item")
	b.ReportMetric(float64(at(0.99).Nanoseconds()), "p99-ns/item")
	b.ReportMetric(float64(worst.d.Nanoseconds()), "max-ns/item")
	b.Logf("pipeline per item over %d items x200: p50 %v  p95 %v  p99 %v  max %v (%s)",
		len(f.corpus), at(0.5), at(0.95), at(0.99), worst.d, worst.id)
}
