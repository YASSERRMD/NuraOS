use std::time::{SystemTime, UNIX_EPOCH};

use nura_core::error::Result;
use nura_core::tool::{Tool, ToolResult};
use serde_json::{json, Value};

/// Returns the current UTC time as a Unix timestamp and an RFC 3339 string.
pub struct TimeNowTool;

impl Tool for TimeNowTool {
    fn name(&self) -> &str {
        "time.now"
    }

    fn description(&self) -> &str {
        "Returns the current UTC time as unix_seconds (integer) and rfc3339 (string)."
    }

    fn schema(&self) -> &Value {
        static SCHEMA: std::sync::OnceLock<Value> = std::sync::OnceLock::new();
        SCHEMA.get_or_init(|| {
            json!({
                "type": "object",
                "additionalProperties": false,
                "properties": {}
            })
        })
    }

    fn read_only(&self) -> bool {
        true
    }

    fn execute(&self, _args: Value) -> Result<ToolResult> {
        let unix_secs = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_secs())
            .unwrap_or(0);

        let rfc3339 = unix_to_rfc3339(unix_secs);

        Ok(ToolResult::json(json!({
            "unix_seconds": unix_secs,
            "rfc3339": rfc3339,
        })))
    }
}

/// Convert a UNIX timestamp (seconds since 1970-01-01T00:00:00Z) to RFC 3339.
///
/// Uses Howard Hinnant's civil-from-days algorithm for Gregorian calendar
/// conversion without any external dependencies.
pub fn unix_to_rfc3339(unix_secs: u64) -> String {
    let days = (unix_secs / 86400) as i64;
    let time_of_day = unix_secs % 86400;
    let h = time_of_day / 3600;
    let m = (time_of_day % 3600) / 60;
    let s = time_of_day % 60;

    let z = days + 719468;
    let era = if z >= 0 { z } else { z - 146096 } / 146097;
    let doe = (z - era * 146097) as u64;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let y = yoe as i64 + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let mo = if mp < 10 { mp + 3 } else { mp - 9 };
    let yr = if mo <= 2 { y + 1 } else { y };

    format!("{:04}-{:02}-{:02}T{:02}:{:02}:{:02}Z", yr, mo, d, h, m, s)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn unix_epoch_formats_correctly() {
        assert_eq!(unix_to_rfc3339(0), "1970-01-01T00:00:00Z");
    }

    #[test]
    fn known_timestamp_formats_correctly() {
        // 2024-01-15T10:30:00Z = 1705314600
        assert_eq!(unix_to_rfc3339(1705314600), "2024-01-15T10:30:00Z");
    }

    #[test]
    fn midnight_on_leap_day_formats_correctly() {
        // 2024-02-29T00:00:00Z = 1709164800
        assert_eq!(unix_to_rfc3339(1709164800), "2024-02-29T00:00:00Z");
    }

    #[test]
    fn tool_is_read_only() {
        let t = TimeNowTool;
        assert!(t.read_only());
        assert_eq!(t.name(), "time.now");
    }

    #[test]
    fn execute_returns_positive_unix_seconds() {
        let t = TimeNowTool;
        let result = t.execute(json!({})).unwrap();
        let secs = result.output["unix_seconds"].as_u64().unwrap();
        assert!(secs > 1_000_000_000, "unix_seconds looks wrong: {}", secs);
    }
}
