//! Read files out of an installed PoE2 client (`Bundles2/`), and the `.datc64` tables in them.
//!
//! Layout: `_.index.bin` is a bundle whose payload lists every bundle and, per file, the hash
//! of its lower-cased path, which bundle holds it and where. A bundle is a header plus
//! Oodle-compressed blocks of a fixed uncompressed size.
use std::{
    collections::HashMap,
    path::{Path, PathBuf},
};

use anyhow::{Context as _, bail, ensure};

struct Reader<'a> {
    buf: &'a [u8],
    pos: usize,
}

impl<'a> Reader<'a> {
    fn new(buf: &'a [u8]) -> Self {
        Self { buf, pos: 0 }
    }
    fn take(&mut self, n: usize) -> anyhow::Result<&'a [u8]> {
        let end = self.pos.checked_add(n).filter(|e| *e <= self.buf.len()).context("unexpected end of data")?;
        let out = &self.buf[self.pos..end];
        self.pos = end;
        Ok(out)
    }
    fn u32(&mut self) -> anyhow::Result<u32> {
        Ok(u32::from_le_bytes(self.take(4)?.try_into()?))
    }
    fn u64(&mut self) -> anyhow::Result<u64> {
        Ok(u64::from_le_bytes(self.take(8)?.try_into()?))
    }
}

fn usize_of(n: u32) -> usize {
    usize::try_from(n).expect("u32 fits usize")
}

/// A bundle's header: block sizes and where the compressed blocks start.
struct Bundle<'a> {
    data: &'a [u8],
    uncompressed: usize,
    granularity: usize,
    /// (offset in `data`, compressed length) per block.
    blocks: Vec<(usize, usize)>,
}

impl<'a> Bundle<'a> {
    fn parse(data: &'a [u8]) -> anyhow::Result<Self> {
        let mut r = Reader::new(data);
        let uncompressed = usize_of(r.u32()?);
        let _total_payload = r.u32()?;
        let head = usize_of(r.u32()?);
        let _encoding = r.u32()?;
        let _unknown = r.u32()?;
        let _uncompressed64 = r.u64()?;
        let _payload64 = r.u64()?;
        let count = usize_of(r.u32()?);
        let granularity = usize_of(r.u32()?);
        r.take(16)?;
        let mut pos = 12 + head;
        let mut blocks = Vec::with_capacity(count);
        for _ in 0..count {
            let len = usize_of(r.u32()?);
            blocks.push((pos, len));
            pos += len;
        }
        ensure!(pos <= data.len(), "bundle is truncated");
        ensure!(granularity > 0, "bundle has no block size");
        Ok(Self { data, uncompressed, granularity, blocks })
    }

    /// Decompress the bytes `[start, start + len)` of the bundle's payload.
    fn read(&self, start: usize, len: usize) -> anyhow::Result<Vec<u8>> {
        ensure!(start + len <= self.uncompressed, "file runs past the end of its bundle");
        if len == 0 {
            return Ok(Vec::new());
        }
        let first = start / self.granularity;
        let last = (start + len - 1) / self.granularity;
        let mut out = Vec::with_capacity((last - first + 1) * self.granularity);
        let mut ex = oozextract::Extractor::new();
        for i in first..=last {
            let (off, clen) = self.blocks[i];
            let size = self.granularity.min(self.uncompressed - i * self.granularity);
            let mut block = vec![0u8; size];
            let n = ex
                .read_from_slice(&self.data[off..off + clen], &mut block)
                .map_err(|e| anyhow::anyhow!("decompress block {i}: {e:?}"))?;
            ensure!(n == size, "block {i} decompressed to {n} bytes, expected {size}");
            out.extend_from_slice(&block);
        }
        let skip = start - first * self.granularity;
        Ok(out[skip..skip + len].to_vec())
    }

    fn read_all(&self) -> anyhow::Result<Vec<u8>> {
        self.read(0, self.uncompressed)
    }
}

/// MurmurHash64A, the hash the PoE2 index uses for lower-cased file paths (seed `0x1337b33f`).
pub fn murmur64a(key: &[u8], seed: u64) -> u64 {
    const M: u64 = 0xc6a4_a793_5bd1_e995;
    const R: u32 = 47;
    let mut h = seed ^ (key.len() as u64).wrapping_mul(M);
    let (chunks, tail) = key.as_chunks::<8>();
    for chunk in chunks {
        let mut k = u64::from_le_bytes(*chunk);
        k = k.wrapping_mul(M);
        k ^= k >> R;
        k = k.wrapping_mul(M);
        h ^= k;
        h = h.wrapping_mul(M);
    }
    if !tail.is_empty() {
        for (i, b) in tail.iter().enumerate() {
            h ^= u64::from(*b) << (8 * i);
        }
        h = h.wrapping_mul(M);
    }
    h ^= h >> R;
    h = h.wrapping_mul(M);
    h ^= h >> R;
    h
}

fn path_hash(path: &str) -> u64 {
    murmur64a(path.to_lowercase().as_bytes(), 0x1337_b33f)
}

/// One directory's path fragments: an index of 0 toggles between collecting base strings and
/// emitting paths; otherwise `index - 1` names a base to prefix the following string with.
fn decode_paths(mut data: &[u8]) -> anyhow::Result<Vec<String>> {
    let mut bases: Vec<String> = Vec::new();
    let mut out = Vec::new();
    let mut base_phase = false;
    while data.len() >= 4 {
        let idx = usize_of(u32::from_le_bytes(data[..4].try_into()?));
        data = &data[4..];
        if idx == 0 {
            base_phase = !base_phase;
            if base_phase {
                bases.clear();
            }
            continue;
        }
        let end = data.iter().position(|b| *b == 0).context("unterminated path fragment")?;
        let frag = std::str::from_utf8(&data[..end])?;
        data = &data[end + 1..];
        let full = match bases.get(idx - 1) {
            Some(b) => format!("{b}{frag}"),
            None => frag.to_owned(),
        };
        if base_phase {
            bases.push(full);
        } else {
            out.push(full);
        }
    }
    Ok(out)
}

struct FileRecord {
    bundle: usize,
    offset: usize,
    size: usize,
}

/// The installed game's file index.
pub struct GameFiles {
    dir: PathBuf,
    bundles: Vec<String>,
    files: HashMap<u64, FileRecord>,
    paths: Vec<String>,
}

impl GameFiles {
    /// Open `<game_dir>/Bundles2/_.index.bin`.
    pub fn open(game_dir: &Path) -> anyhow::Result<Self> {
        let dir = game_dir.join("Bundles2");
        let raw = std::fs::read(dir.join("_.index.bin"))
            .with_context(|| format!("read {}", dir.join("_.index.bin").display()))?;
        let index = Bundle::parse(&raw)?.read_all()?;
        let mut r = Reader::new(&index);
        let count = usize_of(r.u32()?);
        let mut bundles = Vec::with_capacity(count);
        for _ in 0..count {
            let len = usize_of(r.u32()?);
            bundles.push(String::from_utf8(r.take(len)?.to_vec())?);
            let _size = r.u32()?;
        }
        let count = usize_of(r.u32()?);
        let mut files = HashMap::with_capacity(count);
        for _ in 0..count {
            let hash = r.u64()?;
            let rec = FileRecord { bundle: usize_of(r.u32()?), offset: usize_of(r.u32()?), size: usize_of(r.u32()?) };
            files.insert(hash, rec);
        }
        // Then the path list: per directory a slice of a compressed blob of path fragments.
        let count = usize_of(r.u32()?);
        let mut reps = Vec::with_capacity(count);
        for _ in 0..count {
            let _hash = r.u64()?;
            let (offset, size) = (usize_of(r.u32()?), usize_of(r.u32()?));
            let _recursive = r.u32()?;
            reps.push((offset, size));
        }
        let blob = Bundle::parse(&index[r.pos..])?.read_all()?;
        let mut paths = Vec::new();
        for (offset, size) in reps {
            paths.extend(decode_paths(blob.get(offset..offset + size).context("path list out of range")?)?);
        }
        Ok(Self { dir, bundles, files, paths })
    }

    /// Every file path in the index.
    pub fn paths(&self) -> &[String] {
        &self.paths
    }

    /// The contents of the file at `path` (e.g. `data/german/clientstrings.datc64`).
    pub fn read(&self, path: &str) -> anyhow::Result<Vec<u8>> {
        let rec = self.files.get(&path_hash(path)).with_context(|| format!("{path} is not in the game index"))?;
        let name = self.bundles.get(rec.bundle).context("file points at a missing bundle")?;
        let file = self.dir.join(format!("{name}.bundle.bin"));
        let raw = std::fs::read(&file).with_context(|| format!("read {}", file.display()))?;
        Bundle::parse(&raw)?.read(rec.offset, rec.size)
    }
}

/// A `.datc64` table: fixed-size rows, then a variable section (after eight `0xBB` bytes)
/// that string columns point into.
pub struct Dat {
    pub rows: usize,
    pub width: usize,
    fixed: Vec<u8>,
    var: Vec<u8>,
}

impl Dat {
    pub fn parse(data: &[u8]) -> anyhow::Result<Self> {
        let mut r = Reader::new(data);
        let rows = usize_of(r.u32()?);
        let marker = data.windows(8).position(|w| w == [0xBB; 8]).context("no variable-data marker")?;
        let fixed_len = marker - 4;
        if rows == 0 {
            bail!("empty table");
        }
        ensure!(fixed_len % rows == 0, "{fixed_len} bytes of rows do not divide into {rows} rows");
        Ok(Self { rows, width: fixed_len / rows, fixed: data[4..marker].to_vec(), var: data[marker..].to_vec() })
    }

    fn cell(&self, row: usize, offset: usize, len: usize) -> &[u8] {
        let start = row * self.width + offset;
        &self.fixed[start..start + len]
    }

    /// The UTF-16 string a column at `offset` points to (`None` if it points nowhere).
    pub fn string(&self, row: usize, offset: usize) -> Option<String> {
        let at = usize::try_from(u64::from_le_bytes(self.cell(row, offset, 8).try_into().expect("8 bytes"))).ok()?;
        let bytes = self.var.get(at..)?;
        let units: Vec<u16> =
            bytes.as_chunks::<2>().0.iter().map(|c| u16::from_le_bytes(*c)).take_while(|u| *u != 0).collect();
        String::from_utf16(&units).ok()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn murmur_matches_reference_values() {
        // Values from a separate port of the reference C implementation; the real check is
        // that paths resolve in an installed client's index.
        assert_eq!(murmur64a(b"", 0), 0);
        assert_eq!(murmur64a(b"hello", 0), 0x1e68_d17c_457b_f117);
        assert_eq!(path_hash("Data/ClientStrings.datc64"), 0x36a0_726a_43b3_178b);
    }

    #[test]
    fn dat_rows_and_strings() {
        // Two rows of one string column each, then the marker and UTF-16 strings.
        let mut data = 2u32.to_le_bytes().to_vec();
        data.extend(8u64.to_le_bytes());
        data.extend(14u64.to_le_bytes());
        data.extend([0xBB; 8]);
        for s in ["ab", "cd"] {
            data.extend(s.encode_utf16().flat_map(u16::to_le_bytes));
            data.extend([0, 0]);
        }
        let dat = Dat::parse(&data).unwrap();
        assert_eq!((dat.rows, dat.width), (2, 8));
        assert_eq!(dat.string(0, 0).as_deref(), Some("ab"));
        assert_eq!(dat.string(1, 0).as_deref(), Some("cd"));
    }
}
