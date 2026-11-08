package core

// Benchmarks of the pipeline: clipboard text -> parsed item -> matched stats -> trade query.
//
// They match against the synthetic corpus catalog of corpus_test.go (every corpus template
// known), which exercises the same code paths as the bundled catalog with cheaper lookups.
// BenchmarkPipelineP95 reports the per-item p50/p95/p99 of the full pipeline so the
// "< 1 ms p95" budget can be checked directly.

import (
	"slices"
	"testing"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core/coretest"
)

type benchFixture struct {
	corpus  []coretest.CorpusItem
	catalog Catalog
	opts    *QueryOptions
	parsed  []*ParsedItem
	matched []*ParsedItem
}

func newBenchFixture(b *testing.B) *benchFixture {
	b.Helper()
	corpus := loadCorpus(b)
	f := &benchFixture{
		corpus: corpus,
		opts:   &QueryOptions{League: "Standard", Status: StatusOnline, WidenPct: 10, EnableAllStats: false},
	}
	for _, it := range corpus {
		f.parsed = append(f.parsed, mustParse(b, it.Text))
	}
	f.catalog = newCorpusCatalog(f.parsed)
	for _, item := range f.parsed {
		m := cloneItem(item)
		MatchStats(m, f.catalog)
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
	MatchStats(item, f.catalog)
	filters := BuildFilters(item, f.opts)
	sink = filters.ToTradeQuery()
}

func BenchmarkParseItemCorpus(b *testing.B) {
	f := newBenchFixture(b)
	for b.Loop() {
		for _, it := range f.corpus {
			item, err := ParseItem(it.Text)
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
			items[i] = cloneItem(it)
		}
		b.StartTimer()
		for _, item := range items {
			MatchStats(item, f.catalog)
		}
		sink = items
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
			f.pipeline(it.Text)
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
				f.pipeline(it.Text)
				samples = append(samples, sample{time.Since(start), it.Entry.ID})
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
