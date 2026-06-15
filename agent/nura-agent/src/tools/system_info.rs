use nura_core::error::{NuraError, Result};
use nura_core::tool::{Tool, ToolResult};
use serde_json::{json, Value};

/// Returns system information: hostname, OS version, uptime, memory, and
/// load averages. Reads /proc files; returns an error on non-Linux hosts.
pub struct SystemInfoTool;

impl Tool for SystemInfoTool {
    fn name(&self) -> &str {
        "system.info"
    }

    fn description(&self) -> &str {
        "Returns system information: hostname, OS version, uptime, memory usage, and load averages."
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
        let hostname = read_proc_string("/proc/sys/kernel/hostname")
            .unwrap_or_else(|_| "unknown".into())
            .trim()
            .to_string();

        let os_version = read_proc_string("/proc/version")
            .unwrap_or_else(|_| "unknown".into())
            .trim()
            .to_string();

        let uptime_seconds = parse_uptime(
            &read_proc_string("/proc/uptime")
                .unwrap_or_default(),
        );

        let (memory_total_kb, memory_free_kb, memory_available_kb) =
            parse_meminfo(&read_proc_string("/proc/meminfo").unwrap_or_default());

        let (load_1, load_5, load_15) =
            parse_loadavg(&read_proc_string("/proc/loadavg").unwrap_or_default());

        Ok(ToolResult::json(json!({
            "hostname": hostname,
            "os_version": os_version,
            "uptime_seconds": uptime_seconds,
            "memory_total_kb": memory_total_kb,
            "memory_free_kb": memory_free_kb,
            "memory_available_kb": memory_available_kb,
            "load_avg_1m": load_1,
            "load_avg_5m": load_5,
            "load_avg_15m": load_15,
        })))
    }
}

fn read_proc_string(path: &str) -> Result<String> {
    std::fs::read_to_string(path).map_err(|e| NuraError::Tool {
        name: "system.info".into(),
        detail: format!("cannot read {}: {}", path, e),
    })
}

fn parse_uptime(raw: &str) -> f64 {
    raw.split_whitespace()
        .next()
        .and_then(|s| s.parse::<f64>().ok())
        .unwrap_or(0.0)
}

fn parse_meminfo(raw: &str) -> (u64, u64, u64) {
    let mut total = 0u64;
    let mut free = 0u64;
    let mut available = 0u64;
    for line in raw.lines() {
        let mut parts = line.split_whitespace();
        match parts.next() {
            Some("MemTotal:") => total = parts.next().and_then(|s| s.parse().ok()).unwrap_or(0),
            Some("MemFree:") => free = parts.next().and_then(|s| s.parse().ok()).unwrap_or(0),
            Some("MemAvailable:") => {
                available = parts.next().and_then(|s| s.parse().ok()).unwrap_or(0)
            }
            _ => {}
        }
    }
    (total, free, available)
}

fn parse_loadavg(raw: &str) -> (f64, f64, f64) {
    let mut parts = raw.split_whitespace();
    let l1 = parts.next().and_then(|s| s.parse().ok()).unwrap_or(0.0);
    let l5 = parts.next().and_then(|s| s.parse().ok()).unwrap_or(0.0);
    let l15 = parts.next().and_then(|s| s.parse().ok()).unwrap_or(0.0);
    (l1, l5, l15)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_uptime_from_proc() {
        assert!((parse_uptime("12345.67 89.10") - 12345.67).abs() < 0.01);
        assert_eq!(parse_uptime(""), 0.0);
    }

    #[test]
    fn parses_meminfo_fields() {
        let raw = "MemTotal:       16384000 kB\nMemFree:        8000000 kB\nMemAvailable:   12000000 kB\n";
        let (total, free, avail) = parse_meminfo(raw);
        assert_eq!(total, 16384000);
        assert_eq!(free, 8000000);
        assert_eq!(avail, 12000000);
    }

    #[test]
    fn parses_loadavg_fields() {
        let raw = "0.42 0.55 0.71 1/120 5432";
        let (l1, l5, l15) = parse_loadavg(raw);
        assert!((l1 - 0.42).abs() < 0.001);
        assert!((l5 - 0.55).abs() < 0.001);
        assert!((l15 - 0.71).abs() < 0.001);
    }
}
