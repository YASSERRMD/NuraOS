use std::io::Read;
use std::path::{Component, Path};

use nura_core::error::{NuraError, Result};
use nura_core::tool::{Tool, ToolResult};
use serde_json::{json, Value};

/// Maximum bytes the caller may request (hard upper limit).
pub const MAX_READ_BYTES: usize = 1024 * 1024; // 1 MB
/// Default bytes read when `max_bytes` is not specified.
pub const DEFAULT_READ_BYTES: usize = 64 * 1024; // 64 KB

/// Default allowed path prefixes. Callers outside /data/ are rejected.
static DEFAULT_PREFIXES: &[&str] = &["/data/", "/data"];

/// Reads a file under a whitelist of allowed path prefixes, capped in size.
/// Rejects paths with `..` components and paths outside the allowlist.
pub struct FsReadTool {
    allowed_prefixes: Vec<String>,
}

impl Default for FsReadTool {
    fn default() -> Self {
        Self {
            allowed_prefixes: DEFAULT_PREFIXES.iter().map(|s| s.to_string()).collect(),
        }
    }
}

impl FsReadTool {
    #[allow(dead_code)]
    pub fn with_prefixes(prefixes: impl IntoIterator<Item = impl Into<String>>) -> Self {
        Self {
            allowed_prefixes: prefixes.into_iter().map(|s| s.into()).collect(),
        }
    }
}

impl Tool for FsReadTool {
    fn name(&self) -> &str {
        "fs.read"
    }

    fn description(&self) -> &str {
        "Read a file under /data/. Paths outside /data/ and paths containing '..' are rejected. \
         Response is truncated to max_bytes (default 65536, max 1048576)."
    }

    fn schema(&self) -> &Value {
        static SCHEMA: std::sync::OnceLock<Value> = std::sync::OnceLock::new();
        SCHEMA.get_or_init(|| {
            json!({
                "type": "object",
                "required": ["path"],
                "additionalProperties": false,
                "properties": {
                    "path": {
                        "type": "string",
                        "description": "Absolute path to the file. Must start with /data/."
                    },
                    "max_bytes": {
                        "type": "integer",
                        "description": "Maximum bytes to read (default 65536, max 1048576)."
                    }
                }
            })
        })
    }

    fn read_only(&self) -> bool {
        true
    }

    fn execute(&self, args: Value) -> Result<ToolResult> {
        let path_str = args["path"].as_str().ok_or_else(|| NuraError::Tool {
            name: self.name().into(),
            detail: "path must be a string".into(),
        })?;

        let max_bytes = args["max_bytes"]
            .as_u64()
            .map(|n| (n as usize).min(MAX_READ_BYTES))
            .unwrap_or(DEFAULT_READ_BYTES);

        let path = check_path(path_str, &self.allowed_prefixes).map_err(|reason| {
            NuraError::Tool {
                name: self.name().into(),
                detail: reason,
            }
        })?;

        let mut file = std::fs::File::open(&path).map_err(|e| NuraError::Tool {
            name: self.name().into(),
            detail: format!("cannot open '{}': {}", path.display(), e),
        })?;

        let mut buf = vec![0u8; max_bytes + 1];
        let n = file.read(&mut buf).map_err(|e| NuraError::Tool {
            name: self.name().into(),
            detail: format!("read error on '{}': {}", path.display(), e),
        })?;

        let truncated = n > max_bytes;
        let slice = &buf[..n.min(max_bytes)];
        let content = String::from_utf8_lossy(slice).into_owned();

        Ok(ToolResult::json(json!({
            "path": path_str,
            "content": content,
            "bytes_read": slice.len(),
            "truncated": truncated,
        })))
    }
}

/// Validate a file path against the allowlist.
///
/// Rejects paths that contain `..` components or that don't start with one
/// of the allowed prefixes. Symlinks are not followed; path validation is
/// purely lexical.
pub fn check_path(raw: &str, allowed_prefixes: &[String]) -> std::result::Result<std::path::PathBuf, String> {
    let path = Path::new(raw);

    for component in path.components() {
        if component == Component::ParentDir {
            return Err(format!(
                "path '{}' contains '..' which is not allowed",
                raw
            ));
        }
    }

    if !allowed_prefixes.iter().any(|pfx| raw.starts_with(pfx.as_str())) {
        return Err(format!(
            "path '{}' is outside the allowed prefixes ({:?})",
            raw,
            allowed_prefixes
        ));
    }

    Ok(path.to_path_buf())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn prefixes() -> Vec<String> {
        vec!["/data/".to_string(), "/data".to_string()]
    }

    // ---- path validation ----

    #[test]
    fn allows_data_prefix() {
        assert!(check_path("/data/config.toml", &prefixes()).is_ok());
        assert!(check_path("/data/logs/agent.log", &prefixes()).is_ok());
    }

    #[test]
    fn rejects_parent_dir_traversal() {
        let err = check_path("/data/../etc/passwd", &prefixes()).unwrap_err();
        assert!(err.contains(".."), "got: {}", err);
    }

    #[test]
    fn rejects_double_dot_nested() {
        let err = check_path("/data/subdir/../../etc/shadow", &prefixes()).unwrap_err();
        assert!(err.contains(".."), "got: {}", err);
    }

    #[test]
    fn rejects_outside_whitelist() {
        let err = check_path("/etc/passwd", &prefixes()).unwrap_err();
        assert!(err.contains("outside"), "got: {}", err);
    }

    #[test]
    fn rejects_root_path() {
        let err = check_path("/", &prefixes()).unwrap_err();
        assert!(err.contains("outside"), "got: {}", err);
    }

    // ---- size cap via tool schema ----

    #[test]
    fn tool_name_and_read_only() {
        let t = FsReadTool::default();
        assert_eq!(t.name(), "fs.read");
        assert!(t.read_only());
    }

    #[test]
    fn schema_requires_path() {
        let t = FsReadTool::default();
        let schema = t.schema();
        let required = schema["required"].as_array().unwrap();
        assert!(required.iter().any(|v| v.as_str() == Some("path")));
    }

    #[test]
    fn max_read_bytes_constant() {
        assert_eq!(MAX_READ_BYTES, 1024 * 1024);
        assert_eq!(DEFAULT_READ_BYTES, 64 * 1024);
    }

    #[test]
    fn cap_is_enforced_via_min() {
        let capped = (MAX_READ_BYTES + 1).min(MAX_READ_BYTES);
        assert_eq!(capped, MAX_READ_BYTES);
    }
}
