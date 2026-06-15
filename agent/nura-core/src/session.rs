use std::path::PathBuf;
use std::time::{SystemTime, UNIX_EPOCH};

use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::error::{NuraError, Result};
use crate::provider::message::Message;

fn now_unix() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

/// One conversation session: an ordered list of messages plus metadata.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Session {
    /// Stable, URL-safe session identifier (UUID v4).
    pub id: String,
    /// Conversation history in chronological order.
    pub messages: Vec<Message>,
    /// UNIX timestamp when the session was created.
    pub created_at: u64,
    /// UNIX timestamp of the last modification (push or clear).
    pub updated_at: u64,
}

impl Default for Session {
    fn default() -> Self {
        Self::new()
    }
}

impl Session {
    pub fn new() -> Self {
        let now = now_unix();
        Self {
            id: Uuid::new_v4().to_string(),
            messages: Vec::new(),
            created_at: now,
            updated_at: now,
        }
    }

    /// Append a message and bump `updated_at`.
    pub fn push(&mut self, msg: Message) {
        self.messages.push(msg);
        self.updated_at = now_unix();
    }

    /// Remove all messages and bump `updated_at` (implements `:clear`).
    pub fn clear(&mut self) {
        self.messages.clear();
        self.updated_at = now_unix();
    }

    pub fn len(&self) -> usize {
        self.messages.len()
    }

    pub fn is_empty(&self) -> bool {
        self.messages.is_empty()
    }
}

// ---------------------------------------------------------------------------
// SessionStore -- persists sessions to /data/sessions/<id>.json
// ---------------------------------------------------------------------------

/// File-backed store for conversation sessions.
///
/// Each session is one JSON file under `sessions_dir`. The directory is created
/// on first use. `save()` writes atomically via a temp-file rename on Linux;
/// on other platforms it overwrites directly (acceptable since /data is a
/// local ephemeral volume).
pub struct SessionStore {
    sessions_dir: PathBuf,
}

impl SessionStore {
    /// Create a store rooted at `dir` (typically `/data/sessions`).
    pub fn new(dir: impl Into<PathBuf>) -> Self {
        Self {
            sessions_dir: dir.into(),
        }
    }

    fn path_for(&self, id: &str) -> PathBuf {
        self.sessions_dir.join(format!("{}.json", id))
    }

    fn ensure_dir(&self) -> Result<()> {
        std::fs::create_dir_all(&self.sessions_dir).map_err(|e| {
            NuraError::Session(format!(
                "cannot create sessions dir '{}': {}",
                self.sessions_dir.display(),
                e
            ))
        })
    }

    /// Load a session by ID. Returns `Err` if the file does not exist.
    pub fn load(&self, id: &str) -> Result<Session> {
        let path = self.path_for(id);
        let raw = std::fs::read_to_string(&path).map_err(|e| {
            NuraError::Session(format!("cannot read session '{}': {}", path.display(), e))
        })?;
        serde_json::from_str(&raw)
            .map_err(|e| NuraError::Session(format!("cannot parse session '{}': {}", id, e)))
    }

    /// Persist a session to disk.
    pub fn save(&self, session: &Session) -> Result<()> {
        self.ensure_dir()?;
        let path = self.path_for(&session.id);
        let json = serde_json::to_string(session).map_err(|e| {
            NuraError::Session(format!("cannot serialise session '{}': {}", session.id, e))
        })?;

        // Atomic write via temp file on the same filesystem.
        let tmp_path = self.sessions_dir.join(format!("{}.json.tmp", session.id));
        std::fs::write(&tmp_path, &json)
            .map_err(|e| NuraError::Session(format!("cannot write session tmp file: {}", e)))?;
        std::fs::rename(&tmp_path, &path)
            .map_err(|e| NuraError::Session(format!("cannot rename session file: {}", e)))
    }

    /// Delete a session file. Returns `Ok(())` if the file did not exist.
    pub fn delete(&self, id: &str) -> Result<()> {
        let path = self.path_for(id);
        match std::fs::remove_file(&path) {
            Ok(()) => Ok(()),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(e) => Err(NuraError::Session(format!(
                "cannot delete session '{}': {}",
                id, e
            ))),
        }
    }

    /// List all persisted session IDs in the store.
    pub fn list_ids(&self) -> Result<Vec<String>> {
        if !self.sessions_dir.exists() {
            return Ok(Vec::new());
        }
        let mut ids = Vec::new();
        for entry in std::fs::read_dir(&self.sessions_dir)
            .map_err(|e| NuraError::Session(format!("cannot read sessions dir: {}", e)))?
        {
            let entry = entry.map_err(|e| NuraError::Session(e.to_string()))?;
            let name = entry.file_name().to_string_lossy().to_string();
            if let Some(id) = name.strip_suffix(".json") {
                if !id.ends_with(".tmp") {
                    ids.push(id.to_string());
                }
            }
        }
        Ok(ids)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::provider::message::Message;

    fn tmp_store() -> (SessionStore, tempfile_path::TmpDir) {
        let dir = tempfile_path::TmpDir::new();
        (SessionStore::new(dir.path()), dir)
    }

    // Minimal temp directory helper (no external dep).
    mod tempfile_path {
        use std::path::{Path, PathBuf};

        pub struct TmpDir(PathBuf);

        impl TmpDir {
            pub fn new() -> Self {
                let base = std::env::temp_dir();
                let id = std::time::SystemTime::now()
                    .duration_since(std::time::UNIX_EPOCH)
                    .unwrap()
                    .subsec_nanos();
                let path = base.join(format!("nura-session-test-{}", id));
                std::fs::create_dir_all(&path).unwrap();
                Self(path)
            }

            pub fn path(&self) -> &Path {
                &self.0
            }
        }

        impl Drop for TmpDir {
            fn drop(&mut self) {
                let _ = std::fs::remove_dir_all(&self.0);
            }
        }
    }

    #[test]
    fn session_push_and_clear() {
        let mut s = Session::new();
        s.push(Message::user("hello"));
        assert_eq!(s.len(), 1);
        s.clear();
        assert!(s.is_empty());
    }

    #[test]
    fn session_roundtrip_through_store() {
        let (store, _dir) = tmp_store();
        let mut s = Session::new();
        s.push(Message::user("hello"));
        s.push(Message::assistant("world"));
        store.save(&s).unwrap();

        let loaded = store.load(&s.id).unwrap();
        assert_eq!(loaded.messages.len(), 2);
        assert_eq!(loaded.id, s.id);
    }

    #[test]
    fn store_load_missing_returns_err() {
        let (store, _dir) = tmp_store();
        assert!(store.load("does-not-exist").is_err());
    }

    #[test]
    fn store_delete_idempotent() {
        let (store, _dir) = tmp_store();
        assert!(store.delete("no-such-id").is_ok());
    }

    #[test]
    fn store_list_ids() {
        let (store, _dir) = tmp_store();
        let s1 = Session::new();
        let s2 = Session::new();
        store.save(&s1).unwrap();
        store.save(&s2).unwrap();

        let mut ids = store.list_ids().unwrap();
        ids.sort();
        let mut expected = vec![s1.id.clone(), s2.id.clone()];
        expected.sort();
        assert_eq!(ids, expected);
    }

    #[test]
    fn store_delete_removes_from_list() {
        let (store, _dir) = tmp_store();
        let s = Session::new();
        store.save(&s).unwrap();
        store.delete(&s.id).unwrap();
        assert!(store.list_ids().unwrap().is_empty());
    }
}
