use nura_core::error::{NuraError, Result};
use nura_core::tool::{Tool, ToolResult};
use serde_json::{json, Value};

/// Returns network interface names and whether a default route is present.
/// Reads /proc/net/dev (interfaces) and /proc/net/route (routing table).
pub struct NetStatusTool;

impl Tool for NetStatusTool {
    fn name(&self) -> &str {
        "net.status"
    }

    fn description(&self) -> &str {
        "Returns network interface names and default-route presence."
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
        let dev_raw = std::fs::read_to_string("/proc/net/dev").map_err(|e| NuraError::Tool {
            name: self.name().into(),
            detail: format!("cannot read /proc/net/dev: {}", e),
        })?;

        let route_raw =
            std::fs::read_to_string("/proc/net/route").map_err(|e| NuraError::Tool {
                name: self.name().into(),
                detail: format!("cannot read /proc/net/route: {}", e),
            })?;

        let interfaces = parse_interfaces(&dev_raw);
        let has_default_route = has_default_route(&route_raw);

        Ok(ToolResult::json(json!({
            "interfaces": interfaces,
            "has_default_route": has_default_route,
        })))
    }
}

/// Parse interface names from /proc/net/dev (skip header lines and loopback).
pub fn parse_interfaces(raw: &str) -> Vec<String> {
    raw.lines()
        .skip(2)
        .filter_map(|line| {
            let name = line.split(':').next()?.trim().to_string();
            if name.is_empty() {
                None
            } else {
                Some(name)
            }
        })
        .collect()
}

/// A default route has Destination == "00000000" in /proc/net/route.
pub fn has_default_route(raw: &str) -> bool {
    raw.lines().skip(1).any(|line| {
        let mut fields = line.split_whitespace();
        let _iface = fields.next();
        let dest = fields.next().unwrap_or("");
        dest == "00000000"
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_interface_names() {
        let raw = "Inter-|   Receive                                                \n \
                   face |bytes    packets errs\n \
                   lo:       0       0\n \
                   eth0:  12345     100\n";
        let ifaces = parse_interfaces(raw);
        assert!(ifaces.contains(&"lo".to_string()));
        assert!(ifaces.contains(&"eth0".to_string()));
    }

    #[test]
    fn detects_default_route_present() {
        let raw = "Iface Destination Gateway Flags\n\
                   eth0 00000000 0101A8C0 0003\n\
                   eth0 0001A8C0 00000000 0001\n";
        assert!(has_default_route(raw));
    }

    #[test]
    fn detects_default_route_absent() {
        let raw = "Iface Destination Gateway Flags\n\
                   eth0 0001A8C0 00000000 0001\n";
        assert!(!has_default_route(raw));
    }

    #[test]
    fn tool_is_read_only() {
        let t = NetStatusTool;
        assert!(t.read_only());
        assert_eq!(t.name(), "net.status");
    }
}
