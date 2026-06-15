// Append-only JSONL provenance log with SHA-256 hash chaining.
//
// Each entry records one interaction event (prompt, tool call, tool result,
// or completion) under /data/sessions/. The `hash` field in every line
// commits to the previous line's hash, making truncation or edits detectable
// without a trusted external service.
//
// Chain genesis uses the all-zeros hash as `prev_hash`.
// Files rotate at MAX_FILE_BYTES; chain continuity is maintained across files.

use std::fs::{File, OpenOptions};
use std::io::{BufRead, BufReader, Write};
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

/// Starting prev_hash for the first entry in the chain.
pub const GENESIS_HASH: &str =
    "0000000000000000000000000000000000000000000000000000000000000000";

/// Rotate to a new file once the current one exceeds this size.
const MAX_FILE_BYTES: u64 = 10 * 1024 * 1024; // 10 MiB

/// Category of provenance event.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum EventKind {
    TurnStart,
    ToolCall,
    ToolResult,
    Completion,
    TurnEnd,
}

/// One JSONL line in the provenance log.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProvenanceEntry {
    /// Monotonically increasing sequence number across all files.
    pub seq: u64,
    /// UTC timestamp in ISO-8601 format.
    pub ts: String,
    /// Category of event.
    pub kind: EventKind,
    /// UUID of the turn this event belongs to.
    pub turn_id: String,
    /// Event payload (content depends on kind).
    pub content: serde_json::Value,
    /// Hash of the previous entry (GENESIS_HASH for the first).
    pub prev_hash: String,
    /// SHA-256(prev_hash_bytes || canonical_payload_bytes).
    pub hash: String,
}

/// Fields used to compute the hash (excludes the `hash` field itself).
#[derive(Serialize)]
struct HashPayload<'a> {
    seq: u64,
    ts: &'a str,
    kind: &'a EventKind,
    turn_id: &'a str,
    content: &'a serde_json::Value,
    prev_hash: &'a str,
}

fn compute_hash(prev_hash: &str, payload: &HashPayload<'_>) -> String {
    let payload_bytes =
        serde_json::to_vec(payload).expect("HashPayload serialisation must not fail");
    let mut h = Sha256::new();
    h.update(prev_hash.as_bytes());
    h.update(&payload_bytes);
    h.finalize().iter().fold(String::with_capacity(64), |mut s, b| {
        use std::fmt::Write as _;
        let _ = write!(s, "{:02x}", b);
        s
    })
}

/// Format seconds-since-epoch as `YYYY-MM-DDTHH:MM:SSZ` without external deps.
fn unix_to_utc(secs: u64) -> String {
    let s = secs % 60;
    let m = (secs / 60) % 60;
    let h = (secs / 3600) % 24;
    let mut days = (secs / 86400) as u32;

    let mut year = 1970u32;
    loop {
        let leap = year % 4 == 0 && (year % 100 != 0 || year % 400 == 0);
        let dy = if leap { 366 } else { 365 };
        if days < dy {
            break;
        }
        days -= dy;
        year += 1;
    }
    let leap = year % 4 == 0 && (year % 100 != 0 || year % 400 == 0);
    let month_days: [u32; 12] = [31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
    let mut month = 1u32;
    for (i, &dim) in month_days.iter().enumerate() {
        let d = if i == 1 && leap { 29 } else { dim };
        if days < d {
            break;
        }
        days -= d;
        month += 1;
    }
    format!(
        "{:04}-{:02}-{:02}T{:02}:{:02}:{:02}Z",
        year,
        month,
        days + 1,
        h,
        m,
        s
    )
}

fn now_utc() -> String {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| unix_to_utc(d.as_secs()))
        .unwrap_or_else(|_| "1970-01-01T00:00:00Z".to_string())
}

/// Append-only writer for the provenance JSONL log.
pub struct ProvenanceWriter {
    dir: PathBuf,
    file: File,
    seq: u64,
    last_hash: String,
    file_bytes: u64,
}

impl ProvenanceWriter {
    /// Open (or create) the provenance log directory and position at the end.
    pub fn open(dir: impl AsRef<Path>) -> std::io::Result<Self> {
        let dir = dir.as_ref().to_path_buf();
        std::fs::create_dir_all(&dir)?;
        let (file_path, seq, last_hash, file_bytes) = Self::scan(&dir)?;
        let file = OpenOptions::new().create(true).append(true).open(&file_path)?;
        Ok(Self { dir, file, seq, last_hash, file_bytes })
    }

    /// Scan the directory for the current write position and last hash.
    fn scan(dir: &Path) -> std::io::Result<(PathBuf, u64, String, u64)> {
        let mut files = Self::sorted_files(dir)?;
        if files.is_empty() {
            return Ok((dir.join("session-0001.jsonl"), 0, GENESIS_HASH.to_string(), 0));
        }
        let latest = files.pop().unwrap();
        let file_bytes = std::fs::metadata(&latest)?.len();
        let f = File::open(&latest)?;
        let mut last_entry: Option<ProvenanceEntry> = None;
        for line in BufReader::new(f).lines() {
            let line = line?;
            if line.trim().is_empty() {
                continue;
            }
            if let Ok(e) = serde_json::from_str::<ProvenanceEntry>(&line) {
                last_entry = Some(e);
            }
        }
        match last_entry {
            Some(e) => Ok((latest, e.seq, e.hash, file_bytes)),
            None => Ok((latest, 0, GENESIS_HASH.to_string(), file_bytes)),
        }
    }

    fn sorted_files(dir: &Path) -> std::io::Result<Vec<PathBuf>> {
        let mut v: Vec<PathBuf> = std::fs::read_dir(dir)?
            .filter_map(|e| e.ok())
            .map(|e| e.path())
            .filter(|p| p.extension().map(|e| e == "jsonl").unwrap_or(false))
            .collect();
        v.sort();
        Ok(v)
    }

    /// Append one provenance event. The file is flushed after each write.
    pub fn append(
        &mut self,
        kind: EventKind,
        turn_id: &str,
        content: serde_json::Value,
    ) -> std::io::Result<()> {
        if self.file_bytes >= MAX_FILE_BYTES {
            self.rotate()?;
        }
        self.seq += 1;
        let ts = now_utc();
        let prev_hash = self.last_hash.clone();

        let payload = HashPayload {
            seq: self.seq,
            ts: &ts,
            kind: &kind,
            turn_id,
            content: &content,
            prev_hash: &prev_hash,
        };
        let hash = compute_hash(&prev_hash, &payload);

        let entry = ProvenanceEntry {
            seq: self.seq,
            ts,
            kind,
            turn_id: turn_id.to_string(),
            content,
            prev_hash,
            hash: hash.clone(),
        };
        let mut line = serde_json::to_string(&entry)
            .map_err(|e| std::io::Error::new(std::io::ErrorKind::Other, e))?;
        line.push('\n');
        let bytes = line.as_bytes();
        self.file.write_all(bytes)?;
        self.file.flush()?;
        self.file_bytes += bytes.len() as u64;
        self.last_hash = hash;
        Ok(())
    }

    fn rotate(&mut self) -> std::io::Result<()> {
        let n = Self::sorted_files(&self.dir)?.len() as u32 + 1;
        let new_path = self.dir.join(format!("session-{:04}.jsonl", n));
        self.file = OpenOptions::new().create(true).append(true).open(&new_path)?;
        self.file_bytes = 0;
        // last_hash carries over for chain continuity
        Ok(())
    }
}

/// Walk all session JSONL files in sorted order and verify the hash chain.
///
/// Returns `Ok(n)` where `n` is the total number of entries verified.
/// Returns `Err(description)` naming the first broken link.
pub fn verify_chain(dir: impl AsRef<Path>) -> Result<u64, String> {
    let dir = dir.as_ref();
    let mut files: Vec<PathBuf> = std::fs::read_dir(dir)
        .map_err(|e| format!("cannot read {}: {}", dir.display(), e))?
        .filter_map(|e| e.ok())
        .map(|e| e.path())
        .filter(|p| p.extension().map(|e| e == "jsonl").unwrap_or(false))
        .collect();
    files.sort();

    if files.is_empty() {
        return Ok(0);
    }

    let mut expected_prev = GENESIS_HASH.to_string();
    let mut expected_seq: u64 = 1;
    let mut total: u64 = 0;

    for path in &files {
        let f = File::open(path).map_err(|e| format!("{}: {}", path.display(), e))?;
        for (ln, line) in BufReader::new(f).lines().enumerate() {
            let line = line.map_err(|e| format!("{}:{}: {}", path.display(), ln + 1, e))?;
            if line.trim().is_empty() {
                continue;
            }
            let entry: ProvenanceEntry = serde_json::from_str(&line).map_err(|e| {
                format!("{}:{}: parse error: {}", path.display(), ln + 1, e)
            })?;

            if entry.seq != expected_seq {
                return Err(format!(
                    "{}:{}: seq mismatch: want {}, got {}",
                    path.display(),
                    ln + 1,
                    expected_seq,
                    entry.seq
                ));
            }
            if entry.prev_hash != expected_prev {
                return Err(format!(
                    "{}:{}: prev_hash mismatch at seq {}",
                    path.display(),
                    ln + 1,
                    entry.seq
                ));
            }

            let payload = HashPayload {
                seq: entry.seq,
                ts: &entry.ts,
                kind: &entry.kind,
                turn_id: &entry.turn_id,
                content: &entry.content,
                prev_hash: &entry.prev_hash,
            };
            let expected_hash = compute_hash(&entry.prev_hash, &payload);
            if entry.hash != expected_hash {
                return Err(format!(
                    "{}:{}: hash mismatch at seq {}: stored={}, computed={}",
                    path.display(),
                    ln + 1,
                    entry.seq,
                    entry.hash,
                    expected_hash
                ));
            }

            expected_prev = entry.hash.clone();
            expected_seq += 1;
            total += 1;
        }
    }
    Ok(total)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write as _;

    fn tmp_dir() -> tempfile::TempDir {
        tempfile::tempdir().expect("tempdir")
    }

    #[test]
    fn test_write_and_verify_clean() {
        let dir = tmp_dir();
        let mut w = ProvenanceWriter::open(dir.path()).unwrap();
        w.append(
            EventKind::TurnStart,
            "turn-1",
            serde_json::json!({"prompt": "hello"}),
        )
        .unwrap();
        w.append(
            EventKind::Completion,
            "turn-1",
            serde_json::json!({"text": "world"}),
        )
        .unwrap();
        w.append(
            EventKind::TurnEnd,
            "turn-1",
            serde_json::json!({}),
        )
        .unwrap();

        let n = verify_chain(dir.path()).unwrap();
        assert_eq!(n, 3);
    }

    #[test]
    fn test_verify_detects_hash_tamper() {
        let dir = tmp_dir();
        let mut w = ProvenanceWriter::open(dir.path()).unwrap();
        w.append(EventKind::TurnStart, "t", serde_json::json!({})).unwrap();
        w.append(EventKind::TurnEnd, "t", serde_json::json!({})).unwrap();
        drop(w);

        // Tamper: replace a hash nibble in the first line.
        let path = dir.path().join("session-0001.jsonl");
        let content = std::fs::read_to_string(&path).unwrap();
        let tampered = content.replacen("\"hash\":\"", "\"hash\":\"X", 1);
        std::fs::write(&path, tampered).unwrap();

        let err = verify_chain(dir.path()).unwrap_err();
        assert!(
            err.contains("hash mismatch") || err.contains("parse error"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn test_verify_detects_line_deletion() {
        let dir = tmp_dir();
        let mut w = ProvenanceWriter::open(dir.path()).unwrap();
        for i in 0..5 {
            w.append(
                EventKind::TurnStart,
                "t",
                serde_json::json!({"i": i}),
            )
            .unwrap();
        }
        drop(w);

        let path = dir.path().join("session-0001.jsonl");
        let content = std::fs::read_to_string(&path).unwrap();
        let mut lines: Vec<&str> = content.lines().collect();
        lines.remove(2); // delete the third entry
        let tampered = lines.join("\n") + "\n";
        std::fs::write(&path, tampered).unwrap();

        let err = verify_chain(dir.path()).unwrap_err();
        assert!(
            err.contains("seq mismatch") || err.contains("hash mismatch") || err.contains("prev_hash mismatch"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn test_chain_continues_across_rotation() {
        let dir = tmp_dir();
        // Force rotation after first entry by setting file_bytes to MAX.
        let mut w = ProvenanceWriter::open(dir.path()).unwrap();
        // Write first entry normally, then simulate rotation threshold.
        w.append(EventKind::TurnStart, "t", serde_json::json!({})).unwrap();
        // Manually bump file_bytes to trigger rotation on next write.
        w.file_bytes = MAX_FILE_BYTES;
        w.append(EventKind::TurnEnd, "t", serde_json::json!({})).unwrap();
        drop(w);

        // Should have two files now.
        let files: Vec<_> = std::fs::read_dir(dir.path())
            .unwrap()
            .filter_map(|e| e.ok())
            .filter(|e| e.path().extension().map(|x| x == "jsonl").unwrap_or(false))
            .collect();
        assert_eq!(files.len(), 2, "expected two session files after rotation");

        // Chain must still verify across files.
        let n = verify_chain(dir.path()).unwrap();
        assert_eq!(n, 2);
    }

    #[test]
    fn test_unix_to_utc_epoch() {
        assert_eq!(unix_to_utc(0), "1970-01-01T00:00:00Z");
    }

    #[test]
    fn test_unix_to_utc_known() {
        // 2024-01-15 10:23:01 UTC = 1705314181
        assert_eq!(unix_to_utc(1_705_314_181), "2024-01-15T10:23:01Z");
    }

    #[test]
    fn test_genesis_hash_length() {
        assert_eq!(GENESIS_HASH.len(), 64);
    }

    #[test]
    fn test_hash_deterministic() {
        let prev = GENESIS_HASH;
        let payload = HashPayload {
            seq: 1,
            ts: "2024-01-01T00:00:00Z",
            kind: &EventKind::TurnStart,
            turn_id: "t1",
            content: &serde_json::json!({}),
            prev_hash: prev,
        };
        let h1 = compute_hash(prev, &payload);
        let h2 = compute_hash(prev, &payload);
        assert_eq!(h1, h2);
        assert_eq!(h1.len(), 64);
    }
}
