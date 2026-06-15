use std::collections::{HashMap, HashSet};
use std::sync::mpsc::{self, RecvTimeoutError};
use std::sync::Arc;
use std::thread;
use std::time::Duration;

use serde_json::Value;
use tracing::{info, warn};

use crate::error::{NuraError, Result};

/// Structured result returned by a tool call.
#[derive(Debug, Clone)]
pub struct ToolResult {
    /// JSON-serialisable output from the tool.
    pub output: Value,
}

impl ToolResult {
    pub fn json(v: Value) -> Self {
        Self { output: v }
    }
}

/// A callable capability exposed to the model.
///
/// Each implementation must be `Send + Sync` so the registry can box it
/// and dispatch calls on a background thread for timeout enforcement.
pub trait Tool: Send + Sync {
    /// Stable, model-facing name (lowercase, no spaces). Used as the function
    /// name the model emits in a ToolCallRequest.
    fn name(&self) -> &str;

    /// One-sentence description included in the tool schema sent to the model.
    fn description(&self) -> &str;

    /// JSON Schema (draft-07 subset) describing the `args` object. Must have
    /// `type: "object"` at the root.
    fn schema(&self) -> &Value;

    /// True when the tool only reads state and never modifies it. The agent
    /// loop may call read-only tools without additional user confirmation.
    fn read_only(&self) -> bool;

    /// Execute the tool with validated arguments and return a structured result.
    ///
    /// Arguments have already been validated against `schema()` before this
    /// is called. Implementations should return `Err` only for runtime
    /// failures, not for schema mismatches.
    fn execute(&self, args: Value) -> Result<ToolResult>;
}

// ---------------------------------------------------------------------------
// Minimal JSON Schema validator (type + required + additionalProperties)
// ---------------------------------------------------------------------------

/// Validate `args` against a `schema` (draft-07 subset).
///
/// Supports: `type`, `properties` (with type checks), `required`,
/// `additionalProperties: false`.
///
/// Returns `Ok(())` if valid, or `Err` with a human-readable reason.
pub fn validate_args(schema: &Value, args: &Value) -> std::result::Result<(), String> {
    let root_type = schema.get("type").and_then(Value::as_str);

    if root_type != Some("object") {
        return Ok(());
    }

    let obj = args
        .as_object()
        .ok_or_else(|| "arguments must be a JSON object".to_string())?;

    if let Some(required) = schema.get("required").and_then(Value::as_array) {
        for req in required {
            let field = req
                .as_str()
                .ok_or_else(|| "required entry must be a string".to_string())?;
            if !obj.contains_key(field) {
                return Err(format!("required field '{}' is missing", field));
            }
        }
    }

    if let Some(props) = schema.get("properties").and_then(Value::as_object) {
        for (key, prop_schema) in props {
            if let Some(val) = obj.get(key) {
                if let Some(expected) = prop_schema.get("type").and_then(Value::as_str) {
                    if !check_type(val, expected) {
                        return Err(format!(
                            "field '{}' expected type '{}' but got {}",
                            key,
                            expected,
                            json_type_name(val)
                        ));
                    }
                }
            }
        }
    }

    if schema.get("additionalProperties") == Some(&Value::Bool(false)) {
        if let Some(props) = schema.get("properties").and_then(Value::as_object) {
            for key in obj.keys() {
                if !props.contains_key(key.as_str()) {
                    return Err(format!("unexpected property '{}'", key));
                }
            }
        }
    }

    Ok(())
}

fn check_type(val: &Value, expected: &str) -> bool {
    match expected {
        "string" => val.is_string(),
        "number" => val.is_number(),
        "integer" => val.is_i64() || val.is_u64(),
        "boolean" => val.is_boolean(),
        "object" => val.is_object(),
        "array" => val.is_array(),
        "null" => val.is_null(),
        _ => true,
    }
}

fn json_type_name(val: &Value) -> &'static str {
    match val {
        Value::Null => "null",
        Value::Bool(_) => "boolean",
        Value::Number(n) if n.is_f64() => "number",
        Value::Number(_) => "integer",
        Value::String(_) => "string",
        Value::Array(_) => "array",
        Value::Object(_) => "object",
    }
}

// ---------------------------------------------------------------------------
// ToolBudget -- per-turn call limit
// ---------------------------------------------------------------------------

/// Tracks how many tool calls remain in the current turn.
///
/// Call `consume()` before each tool dispatch; it returns
/// `Err(BudgetExceeded)` once the cap is reached so the agent loop can
/// close the turn cleanly.
#[derive(Debug, Clone)]
pub struct ToolBudget {
    max_calls: u32,
    calls_made: u32,
}

impl ToolBudget {
    pub fn new(max_calls: u32) -> Self {
        Self {
            max_calls,
            calls_made: 0,
        }
    }

    /// Deduct one call from the budget. Returns `Err` when exhausted.
    pub fn consume(&mut self) -> Result<()> {
        if self.calls_made >= self.max_calls {
            return Err(NuraError::BudgetExceeded(format!(
                "tool call budget of {} exceeded",
                self.max_calls
            )));
        }
        self.calls_made += 1;
        Ok(())
    }

    pub fn remaining(&self) -> u32 {
        self.max_calls.saturating_sub(self.calls_made)
    }

    pub fn calls_made(&self) -> u32 {
        self.calls_made
    }
}

// ---------------------------------------------------------------------------
// ToolRegistry -- registration and dispatch
// ---------------------------------------------------------------------------

/// Maintains a set of registered tools and an explicit allowlist.
///
/// Only tools on the allowlist can be called by the model. Tools that are
/// registered but not allowlisted exist only for internal use or testing.
pub struct ToolRegistry {
    tools: HashMap<String, Arc<dyn Tool>>,
    allowlist: HashSet<String>,
}

impl Default for ToolRegistry {
    fn default() -> Self {
        Self::new()
    }
}

impl ToolRegistry {
    pub fn new() -> Self {
        Self {
            tools: HashMap::new(),
            allowlist: HashSet::new(),
        }
    }

    /// Register a tool. A registered tool is not callable until it is also
    /// added to the allowlist.
    pub fn register(&mut self, tool: impl Tool + 'static) {
        let name = tool.name().to_string();
        info!(tool = %name, "tool registered");
        self.tools.insert(name, Arc::new(tool));
    }

    /// Add a previously registered tool to the allowlist.
    ///
    /// Panics in debug mode if the tool is not registered (misconfiguration).
    pub fn allowlist(&mut self, name: &str) {
        debug_assert!(
            self.tools.contains_key(name),
            "allowlisted tool '{}' is not registered",
            name
        );
        self.allowlist.insert(name.to_string());
        info!(tool = %name, "tool allowlisted");
    }

    pub fn is_allowed(&self, name: &str) -> bool {
        self.allowlist.contains(name)
    }

    /// Look up an allowlisted tool by name.
    pub fn get_allowed(&self, name: &str) -> Option<Arc<dyn Tool>> {
        if self.allowlist.contains(name) {
            self.tools.get(name).cloned()
        } else {
            None
        }
    }

    /// Dispatch a tool call:
    ///   1. Check allowlist (reject if not listed).
    ///   2. Validate args against the tool's JSON Schema.
    ///   3. Consume one call from `budget` (reject if exhausted).
    ///   4. Execute on a background thread with `call_timeout`.
    ///
    /// All rejections are logged at WARN before returning the error.
    pub fn call(
        &self,
        name: &str,
        args: Value,
        call_timeout: Duration,
        budget: &mut ToolBudget,
    ) -> Result<ToolResult> {
        let tool = match self.get_allowed(name) {
            Some(t) => t,
            None => {
                warn!(tool = %name, "tool call rejected: not allowlisted");
                return Err(NuraError::Tool {
                    name: name.to_string(),
                    detail: "tool is not on the allowlist".to_string(),
                });
            }
        };

        if let Err(reason) = validate_args(tool.schema(), &args) {
            warn!(tool = %name, reason = %reason, "tool call rejected: schema validation failed");
            return Err(NuraError::Tool {
                name: name.to_string(),
                detail: format!("schema validation failed: {}", reason),
            });
        }

        budget.consume()?;

        info!(
            tool = %name,
            read_only = tool.read_only(),
            remaining_budget = budget.remaining(),
            "dispatching tool call"
        );

        let (tx, rx) = mpsc::channel::<Result<ToolResult>>();
        thread::spawn(move || {
            let _ = tx.send(tool.execute(args));
        });

        match rx.recv_timeout(call_timeout) {
            Ok(result) => {
                if let Ok(ref r) = result {
                    info!(tool = %name, output = %r.output, "tool call completed");
                }
                result
            }
            Err(RecvTimeoutError::Timeout) => {
                warn!(tool = %name, timeout = ?call_timeout, "tool call timed out");
                Err(NuraError::BudgetExceeded(format!(
                    "tool '{}' exceeded call timeout {:?}",
                    name, call_timeout
                )))
            }
            Err(RecvTimeoutError::Disconnected) => {
                warn!(tool = %name, "tool thread panicked or disconnected");
                Err(NuraError::Internal(format!(
                    "tool '{}' thread terminated unexpectedly",
                    name
                )))
            }
        }
    }

    pub fn registered_names(&self) -> impl Iterator<Item = &str> {
        self.tools.keys().map(String::as_str)
    }

    pub fn allowed_names(&self) -> impl Iterator<Item = &str> {
        self.allowlist.iter().map(String::as_str)
    }
}

// ---------------------------------------------------------------------------
// EchoTool -- reference implementation for tests and docs
// ---------------------------------------------------------------------------

/// A trivial read-only tool that echoes its `message` argument back.
/// Used in tests and as a documentation example.
pub struct EchoTool;

impl Tool for EchoTool {
    fn name(&self) -> &str {
        "echo"
    }

    fn description(&self) -> &str {
        "Returns the provided message unchanged. Used for testing."
    }

    fn schema(&self) -> &Value {
        static SCHEMA: std::sync::OnceLock<Value> = std::sync::OnceLock::new();
        SCHEMA.get_or_init(|| {
            serde_json::json!({
                "type": "object",
                "required": ["message"],
                "additionalProperties": false,
                "properties": {
                    "message": { "type": "string" }
                }
            })
        })
    }

    fn read_only(&self) -> bool {
        true
    }

    fn execute(&self, args: Value) -> Result<ToolResult> {
        let msg = args["message"].as_str().ok_or_else(|| NuraError::Tool {
            name: self.name().to_string(),
            detail: "message field missing".to_string(),
        })?;
        Ok(ToolResult::json(serde_json::json!({ "echo": msg })))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn echo_registry() -> ToolRegistry {
        let mut r = ToolRegistry::new();
        r.register(EchoTool);
        r.allowlist("echo");
        r
    }

    fn short_timeout() -> Duration {
        Duration::from_secs(5)
    }

    // ---- validate_args ----

    #[test]
    fn schema_accepts_valid_args() {
        let schema = json!({
            "type": "object",
            "required": ["x"],
            "properties": { "x": { "type": "integer" } }
        });
        assert!(validate_args(&schema, &json!({"x": 42})).is_ok());
    }

    #[test]
    fn schema_rejects_missing_required() {
        let schema = json!({
            "type": "object",
            "required": ["x"],
            "properties": { "x": { "type": "string" } }
        });
        let err = validate_args(&schema, &json!({})).unwrap_err();
        assert!(err.contains("required field 'x'"), "got: {}", err);
    }

    #[test]
    fn schema_rejects_wrong_type() {
        let schema = json!({
            "type": "object",
            "properties": { "n": { "type": "integer" } }
        });
        let err = validate_args(&schema, &json!({"n": "not-a-number"})).unwrap_err();
        assert!(err.contains("type"), "got: {}", err);
    }

    #[test]
    fn schema_rejects_additional_properties() {
        let schema = json!({
            "type": "object",
            "additionalProperties": false,
            "properties": { "a": { "type": "string" } }
        });
        let err = validate_args(&schema, &json!({"a": "ok", "b": "extra"})).unwrap_err();
        assert!(err.contains("unexpected property"), "got: {}", err);
    }

    #[test]
    fn schema_passes_non_object_root_through() {
        let schema = json!({"type": "array"});
        assert!(validate_args(&schema, &json!(["anything"])).is_ok());
    }

    // ---- ToolBudget ----

    #[test]
    fn budget_enforces_max_calls() {
        let mut b = ToolBudget::new(2);
        assert!(b.consume().is_ok());
        assert!(b.consume().is_ok());
        assert!(b.consume().is_err(), "third call should exceed budget");
        assert_eq!(b.remaining(), 0);
        assert_eq!(b.calls_made(), 2);
    }

    #[test]
    fn budget_remaining_counts_correctly() {
        let mut b = ToolBudget::new(5);
        b.consume().unwrap();
        assert_eq!(b.remaining(), 4);
        assert_eq!(b.calls_made(), 1);
    }

    // ---- ToolRegistry ----

    #[test]
    fn registry_rejects_non_allowlisted_tool() {
        let mut r = ToolRegistry::new();
        r.register(EchoTool);
        let mut b = ToolBudget::new(10);
        let result = r.call("echo", json!({"message": "hi"}), short_timeout(), &mut b);
        assert!(result.is_err());
        if let Err(NuraError::Tool { detail, .. }) = result {
            assert!(detail.contains("allowlist"), "got: {}", detail);
        } else {
            panic!("expected Tool error");
        }
    }

    #[test]
    fn registry_rejects_schema_mismatch() {
        let r = echo_registry();
        let mut b = ToolBudget::new(10);
        let result = r.call("echo", json!({"message": 42}), short_timeout(), &mut b);
        assert!(result.is_err());
        if let Err(NuraError::Tool { detail, .. }) = result {
            assert!(detail.contains("schema"), "got: {}", detail);
        } else {
            panic!("expected Tool error");
        }
    }

    #[test]
    fn registry_rejects_missing_required_field() {
        let r = echo_registry();
        let mut b = ToolBudget::new(10);
        let result = r.call("echo", json!({}), short_timeout(), &mut b);
        assert!(result.is_err());
    }

    #[test]
    fn registry_executes_valid_call() {
        let r = echo_registry();
        let mut b = ToolBudget::new(10);
        let result = r
            .call("echo", json!({"message": "hello"}), short_timeout(), &mut b)
            .unwrap();
        assert_eq!(result.output["echo"], "hello");
        assert_eq!(b.calls_made(), 1);
    }

    #[test]
    fn registry_enforces_budget_across_calls() {
        let r = echo_registry();
        let mut b = ToolBudget::new(1);
        r.call("echo", json!({"message": "first"}), short_timeout(), &mut b)
            .unwrap();
        let result = r.call(
            "echo",
            json!({"message": "second"}),
            short_timeout(),
            &mut b,
        );
        assert!(result.is_err(), "second call should exhaust budget");
    }
}
