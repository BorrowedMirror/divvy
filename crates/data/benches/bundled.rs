//! Startup cost of the bundled game data: zstd decode + sha256 + JSON + index build.
//!
//! `bundled()` caches in a process-wide `OnceLock`, so `main` times the first (cold) call
//! once in this fresh process; criterion then measures the same work through the public
//! decode/index API, which bypasses the cache.
#![allow(missing_docs)]
use std::{hint::black_box, time::Instant};

use criterion::{Criterion, criterion_group};
use rkl_core::catalog::{BaseDef, StatDef, StaticDef, UniqueDef};
use rkl_data::{Bundle, GameData, decode_asset, parse_manifest};

const MANIFEST: &[u8] = include_bytes!("../assets/manifest.json");
const STATS: &[u8] = include_bytes!("../assets/stats.json.zst");
const BASES: &[u8] = include_bytes!("../assets/bases.json.zst");
const UNIQUES: &[u8] = include_bytes!("../assets/uniques.json.zst");
const STATICS: &[u8] = include_bytes!("../assets/statics.json.zst");

fn decode() -> (rkl_data::Manifest, Bundle) {
    let m = parse_manifest(MANIFEST).expect("manifest");
    let bundle = Bundle {
        stats: decode_asset::<Vec<StatDef>>(&m.stats, STATS).expect("stats"),
        bases: decode_asset::<Vec<BaseDef>>(&m.bases, BASES).expect("bases"),
        uniques: decode_asset::<Vec<UniqueDef>>(&m.uniques, UNIQUES).expect("uniques"),
        statics: decode_asset::<Vec<StaticDef>>(&m.statics, STATICS).expect("statics"),
        languages: Vec::new(),
    };
    (m, bundle)
}

fn benches(c: &mut Criterion) {
    let m = parse_manifest(MANIFEST).expect("manifest");
    c.bench_function("decode_asset/stats", |b| b.iter(|| decode_asset::<Vec<StatDef>>(&m.stats, black_box(STATS))));
    let json = zstd::decode_all(STATS).expect("zstd");
    c.bench_function("zstd/stats", |b| b.iter(|| zstd::decode_all(black_box(STATS))));
    c.bench_function("sha256/stats", |b| b.iter(|| rkl_data::sha256_hex(black_box(&json))));
    c.bench_function("serde_json/stats", |b| b.iter(|| serde_json::from_slice::<Vec<StatDef>>(black_box(&json))));
    c.bench_function("decode_asset/all", |b| b.iter(decode));
    c.bench_function("index_build", |b| {
        b.iter_batched(decode, |(m, bundle)| GameData::from_bundle(m, bundle), criterion::BatchSize::SmallInput);
    });
    c.bench_function("decode+index (bundled uncached)", |b| {
        b.iter(|| {
            let (m, bundle) = decode();
            GameData::from_bundle(m, bundle)
        });
    });
}

criterion_group!(group, benches);

fn main() {
    let start = Instant::now();
    let data = rkl_data::bundled().expect("bundled");
    eprintln!("bundled() cold, first call in fresh process: {:?}", start.elapsed());
    black_box(data);
    group();
    Criterion::default().configure_from_args().final_summary();
}
