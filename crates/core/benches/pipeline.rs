//! Hot-path benches: clipboard text -> parsed item -> matched stats -> trade query.
//!
//! Besides the criterion groups, `main` prints a per-item p95 of the full pipeline
//! (parse + match + filters + query) so the "< 1 ms p95" budget can be checked directly.
#![allow(missing_docs, clippy::missing_docs_in_private_items)]
use std::{
    hint::black_box,
    path::PathBuf,
    time::{Duration, Instant},
};

use criterion::{Criterion, criterion_group};
use kalandra_core::{
    matcher::{StatMatcher, templatize},
    parser::parse_item,
    query::{QueryOptions, build_filters},
};
use serde::Deserialize;

#[derive(Deserialize)]
struct Entry {
    id: String,
}

#[derive(Deserialize)]
struct Index {
    item: Vec<Entry>,
}

/// Every corpus item's clipboard text, in index order.
fn corpus() -> Vec<(String, String)> {
    let dir = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../testkit/corpus");
    let index: Index = toml::from_str(&std::fs::read_to_string(dir.join("index.toml")).expect("index")).expect("toml");
    index
        .item
        .into_iter()
        .map(|e| {
            let text = std::fs::read_to_string(dir.join(format!("{}.txt", e.id))).expect("corpus item");
            (e.id, text)
        })
        .collect()
}

fn opts() -> QueryOptions {
    QueryOptions { league: "Standard".into(), online_only: true, widen_pct: 10.0, enable_all_stats: false }
}

fn pipeline(data: &kalandra_data::GameData, text: &str, opts: &QueryOptions) {
    let mut item = parse_item(text).expect("corpus parses");
    let outcomes = StatMatcher::new(data).match_item(&mut item);
    let filters = build_filters(&item, opts);
    black_box((outcomes, filters.to_trade_query()));
}

fn benches(c: &mut Criterion) {
    let corpus = corpus();
    let data = kalandra_data::bundled().expect("bundled data");
    let opts = opts();
    let parsed: Vec<_> = corpus.iter().map(|(_, t)| parse_item(t).expect("corpus parses")).collect();
    let matched: Vec<_> = parsed
        .iter()
        .cloned()
        .map(|mut item| {
            StatMatcher::new(&data).match_item(&mut item);
            item
        })
        .collect();

    c.bench_function("parse_item/corpus", |b| {
        b.iter(|| {
            for (_, text) in &corpus {
                black_box(parse_item(black_box(text)).expect("parses"));
            }
        });
    });
    c.bench_function("templatize/line", |b| b.iter(|| templatize(black_box("+42% increased Physical Damage"))));
    c.bench_function("match_item/corpus", |b| {
        b.iter_batched_ref(
            || parsed.clone(),
            |items| {
                let matcher = StatMatcher::new(&data);
                for item in items.iter_mut() {
                    black_box(matcher.match_item(item));
                }
            },
            criterion::BatchSize::SmallInput,
        );
    });
    c.bench_function("build_filters+to_trade_query/corpus", |b| {
        b.iter(|| {
            for item in &matched {
                black_box(build_filters(black_box(item), &opts).to_trade_query());
            }
        });
    });
    c.bench_function("pipeline/corpus", |b| {
        b.iter(|| {
            for (_, text) in &corpus {
                pipeline(&data, text, &opts);
            }
        });
    });
}

/// Per-item latency distribution of the full pipeline over the corpus.
fn report_p95() {
    let corpus = corpus();
    let data = kalandra_data::bundled().expect("bundled data");
    let opts = opts();
    let mut samples: Vec<(Duration, &str)> = Vec::new();
    for _ in 0..200 {
        for (id, text) in &corpus {
            let start = Instant::now();
            pipeline(&data, text, &opts);
            samples.push((start.elapsed(), id));
        }
    }
    samples.sort_by_key(|s| s.0);
    let at = |q: f64| samples[((samples.len() - 1) as f64 * q) as usize];
    let (p50, p95, p99, max) = (at(0.5).0, at(0.95).0, at(0.99).0, samples[samples.len() - 1]);
    eprintln!(
        "pipeline per item over {} items x200: p50 {p50:?}  p95 {p95:?}  p99 {p99:?}  max {:?} ({})",
        corpus.len(),
        max.0,
        max.1
    );
}

criterion_group!(group, benches);

fn main() {
    report_p95();
    group();
    Criterion::default().configure_from_args().final_summary();
}
