use std::io::{BufRead, BufReader};
use std::thread;
use std::time::Duration;

use nura_core::error::{NuraError, Result};
use nura_core::provider::message::{Message, Role, StopReason, Usage};
use nura_core::provider::traits::{CancelToken, Capabilities, Provider, SamplingParams};
use nura_core::provider::StreamEvent;
use tracing::{debug, warn};

pub const DEFAULT_BASE_URL: &str = "https://api.anthropic.com";
pub const DEFAULT_MODEL: &str = "claude-haiku-4-5-20251001";
const API_VERSION: &str = "2023-06-01";
const MAX_RETRIES: u32 = 3;

/// AnthropicProvider calls the Anthropic Messages API with streaming.
///
/// API key is taken from `Secrets::load()` or `ANTHROPIC_API_KEY` env var.
/// It is never stored in logs or debug output.
pub struct AnthropicProvider {
    api_key: String,
    base_url: String,
    model: String,
}

impl AnthropicProvider {
    /// Create from an explicit key, base URL, and model.
    pub fn new(
        api_key: impl Into<String>,
        base_url: impl Into<String>,
        model: impl Into<String>,
    ) -> Self {
        Self {
            api_key: api_key.into(),
            base_url: base_url.into(),
            model: model.into(),
        }
    }

    /// Try to build from the loaded secrets. Returns `None` if no key is available.
    pub fn from_secrets() -> Option<Self> {
        use nura_core::secrets::Secrets;
        let secrets = Secrets::load().ok()?;
        let key = secrets.anthropic_api_key?.expose().to_string();
        Some(Self::new(key, DEFAULT_BASE_URL, DEFAULT_MODEL))
    }

    fn messages_url(&self) -> String {
        format!("{}/v1/messages", self.base_url.trim_end_matches('/'))
    }
}

impl Provider for AnthropicProvider {
    fn name(&self) -> &str {
        "anthropic"
    }

    fn capabilities(&self) -> Capabilities {
        Capabilities {
            streaming: true,
            tool_calling: true,
            system_messages: true,
            max_context_tokens: 200_000,
        }
    }

    fn complete(
        &self,
        messages: &[Message],
        params: &SamplingParams,
        cancel: &CancelToken,
    ) -> Box<dyn Iterator<Item = Result<StreamEvent>> + Send + '_> {
        if cancel.is_cancelled() {
            return Box::new(std::iter::once(Ok(StreamEvent::done(StopReason::Cancel))));
        }

        let (system, api_messages) = split_system(messages);
        let body = build_request_body(&self.model, &system, &api_messages, params);

        debug!(model = %self.model, "anthropic provider: sending request");

        let response = self.send_with_retry(&body, cancel, MAX_RETRIES);
        match response {
            Err(e) => Box::new(std::iter::once(Err(e))),
            Ok(resp) => {
                let reader = BufReader::new(resp.into_reader());
                Box::new(AnthropicStream {
                    reader,
                    cancel: cancel.clone(),
                    done: false,
                    pending_tool_id: None,
                    pending_tool_name: None,
                })
            }
        }
    }
}

impl AnthropicProvider {
    fn send_with_retry(
        &self,
        body: &str,
        cancel: &CancelToken,
        retries_left: u32,
    ) -> std::result::Result<ureq::Response, NuraError> {
        let mut attempt = 0u32;
        loop {
            if cancel.is_cancelled() {
                return Err(NuraError::Provider {
                    provider: "anthropic".into(),
                    detail: "cancelled before send".into(),
                });
            }

            let result = ureq::post(&self.messages_url())
                .set("Content-Type", "application/json")
                .set("x-api-key", &self.api_key)
                .set("anthropic-version", API_VERSION)
                .timeout(Duration::from_secs(120))
                .send_string(body);

            match result {
                Ok(resp) => return Ok(resp),
                Err(ureq::Error::Status(429, resp)) => {
                    if attempt >= retries_left {
                        return Err(NuraError::Provider {
                            provider: "anthropic".into(),
                            detail: format!("rate limit after {} retries", retries_left),
                        });
                    }
                    let retry_after = resp
                        .header("retry-after")
                        .and_then(|v| v.parse::<u64>().ok())
                        .unwrap_or(2u64.pow(attempt + 1));
                    warn!("anthropic: rate limited; retrying in {}s", retry_after);
                    thread::sleep(Duration::from_secs(retry_after));
                }
                Err(ureq::Error::Status(status, _)) if status >= 500 => {
                    if attempt >= retries_left {
                        return Err(NuraError::Provider {
                            provider: "anthropic".into(),
                            detail: format!(
                                "server error {} after {} retries",
                                status, retries_left
                            ),
                        });
                    }
                    let delay = 2u64.pow(attempt + 1);
                    warn!("anthropic: server error {}; retrying in {}s", status, delay);
                    thread::sleep(Duration::from_secs(delay));
                }
                Err(ureq::Error::Status(status, _)) => {
                    return Err(NuraError::Provider {
                        provider: "anthropic".into(),
                        detail: format!("HTTP {}", status),
                    });
                }
                Err(e) => {
                    return Err(NuraError::Provider {
                        provider: "anthropic".into(),
                        detail: format!("request error: {}", e),
                    });
                }
            }
            attempt += 1;
        }
    }
}

// ---- request building ----

/// Split system messages from the rest; Anthropic puts system at the top level.
fn split_system(messages: &[Message]) -> (Option<String>, Vec<serde_json::Value>) {
    let mut system_text: Option<String> = None;
    let mut api_msgs: Vec<serde_json::Value> = Vec::new();

    for msg in messages {
        if msg.role == Role::System {
            let text = msg
                .parts
                .iter()
                .filter_map(|p| match p {
                    nura_core::provider::message::ContentPart::Text { text } => Some(text.as_str()),
                    _ => None,
                })
                .collect::<Vec<_>>()
                .join("\n");
            system_text = Some(text);
        } else {
            api_msgs.push(message_to_api_value(msg));
        }
    }

    (system_text, api_msgs)
}

fn message_to_api_value(msg: &Message) -> serde_json::Value {
    use nura_core::provider::message::ContentPart;

    let role = match msg.role {
        Role::User => "user",
        Role::Assistant => "assistant",
        Role::Tool => "user",
        Role::System => "user",
    };

    let content: Vec<serde_json::Value> = msg
        .parts
        .iter()
        .map(|part| match part {
            ContentPart::Text { text } => {
                serde_json::json!({"type": "text", "text": text})
            }
            ContentPart::ToolCallRequest {
                id,
                name,
                arguments,
            } => {
                serde_json::json!({
                    "type": "tool_use",
                    "id": id,
                    "name": name,
                    "input": arguments,
                })
            }
            ContentPart::ToolCallResult {
                call_id,
                output,
                error,
            } => {
                let content_val = if let Some(err) = error {
                    serde_json::json!([{"type":"text","text": format!("error: {}", err)}])
                } else {
                    serde_json::json!([{"type":"text","text": output.to_string()}])
                };
                serde_json::json!({
                    "type": "tool_result",
                    "tool_use_id": call_id,
                    "content": content_val,
                })
            }
        })
        .collect();

    serde_json::json!({"role": role, "content": content})
}

fn build_request_body(
    model: &str,
    system: &Option<String>,
    messages: &[serde_json::Value],
    params: &SamplingParams,
) -> String {
    let mut obj = serde_json::json!({
        "model": model,
        "max_tokens": params.max_tokens,
        "temperature": params.temperature,
        "top_p": params.top_p,
        "stream": true,
        "messages": messages,
    });

    if let Some(sys) = system {
        obj["system"] = serde_json::Value::String(sys.clone());
    }

    if !params.stop.is_empty() {
        obj["stop_sequences"] = serde_json::Value::Array(
            params
                .stop
                .iter()
                .map(|s| serde_json::Value::String(s.clone()))
                .collect(),
        );
    }

    obj.to_string()
}

// ---- streaming ----

struct AnthropicStream {
    reader: BufReader<Box<dyn std::io::Read + Send + Sync + 'static>>,
    cancel: CancelToken,
    done: bool,
    /// ID of the tool_use block currently being streamed.
    pending_tool_id: Option<String>,
    /// Name of the tool_use block currently being streamed.
    pending_tool_name: Option<String>,
}

impl Iterator for AnthropicStream {
    type Item = Result<StreamEvent>;

    fn next(&mut self) -> Option<Self::Item> {
        if self.done {
            return None;
        }

        loop {
            if self.cancel.is_cancelled() {
                self.done = true;
                return Some(Ok(StreamEvent::done(StopReason::Cancel)));
            }

            let mut event_type = String::new();
            let mut data_line = String::new();

            // Read event type line ("event: ...").
            loop {
                let mut line = String::new();
                match self.reader.read_line(&mut line) {
                    Ok(0) => {
                        self.done = true;
                        return Some(Ok(StreamEvent::done(StopReason::EndOfTurn)));
                    }
                    Ok(_) => {}
                    Err(e) => {
                        self.done = true;
                        return Some(Err(NuraError::Provider {
                            provider: "anthropic".into(),
                            detail: format!("stream read error: {}", e),
                        }));
                    }
                }
                let trimmed = line.trim().to_string();
                if trimmed.is_empty() {
                    if !data_line.is_empty() {
                        break; // blank line after data = dispatch the event
                    }
                    continue;
                }
                if let Some(rest) = trimmed.strip_prefix("event: ") {
                    event_type = rest.to_string();
                } else if let Some(rest) = trimmed.strip_prefix("data: ") {
                    data_line = rest.to_string();
                }
            }

            if data_line.is_empty() {
                continue;
            }

            match parse_event(
                &event_type,
                &data_line,
                &mut self.pending_tool_id,
                &mut self.pending_tool_name,
            ) {
                ParseResult::Continue => continue,
                ParseResult::Emit(ev) => {
                    if matches!(ev, StreamEvent::Done { .. }) {
                        self.done = true;
                    }
                    return Some(Ok(ev));
                }
                ParseResult::Error(e) => {
                    self.done = true;
                    return Some(Err(e));
                }
            }
        }
    }
}

enum ParseResult {
    Continue,
    Emit(StreamEvent),
    Error(NuraError),
}

fn parse_event(
    event_type: &str,
    data: &str,
    pending_tool_id: &mut Option<String>,
    pending_tool_name: &mut Option<String>,
) -> ParseResult {
    let v: serde_json::Value = match serde_json::from_str(data) {
        Ok(v) => v,
        Err(e) => {
            warn!("anthropic: bad SSE JSON: {}", e);
            return ParseResult::Continue;
        }
    };

    match event_type {
        "content_block_start" => {
            let block_type = v["content_block"]["type"].as_str().unwrap_or("");
            if block_type == "tool_use" {
                *pending_tool_id = v["content_block"]["id"].as_str().map(str::to_string);
                *pending_tool_name = v["content_block"]["name"].as_str().map(str::to_string);
            }
            ParseResult::Continue
        }
        "content_block_delta" => {
            let delta_type = v["delta"]["type"].as_str().unwrap_or("");
            match delta_type {
                "text_delta" => {
                    let text = v["delta"]["text"].as_str().unwrap_or("").to_string();
                    if text.is_empty() {
                        ParseResult::Continue
                    } else {
                        ParseResult::Emit(StreamEvent::token(text))
                    }
                }
                "input_json_delta" => {
                    let chunk = v["delta"]["partial_json"]
                        .as_str()
                        .unwrap_or("")
                        .to_string();
                    ParseResult::Emit(StreamEvent::ToolCallDelta {
                        id: pending_tool_id.clone().unwrap_or_default(),
                        name: pending_tool_name.take(),
                        arguments_chunk: chunk,
                    })
                }
                _ => ParseResult::Continue,
            }
        }
        "content_block_stop" => {
            *pending_tool_id = None;
            *pending_tool_name = None;
            ParseResult::Continue
        }
        "message_delta" => {
            let output_tokens = v["usage"]["output_tokens"].as_u64().unwrap_or(0) as u32;
            ParseResult::Emit(StreamEvent::Usage(Usage {
                prompt_tokens: 0,
                completion_tokens: output_tokens,
            }))
        }
        "message_start" => {
            let input_tokens = v["message"]["usage"]["input_tokens"].as_u64().unwrap_or(0) as u32;
            if input_tokens > 0 {
                ParseResult::Emit(StreamEvent::Usage(Usage {
                    prompt_tokens: input_tokens,
                    completion_tokens: 0,
                }))
            } else {
                ParseResult::Continue
            }
        }
        "message_stop" => {
            let stop_reason = match v["message_stop"]["stop_reason"]
                .as_str()
                .unwrap_or("end_turn")
            {
                "max_tokens" => StopReason::MaxTokens,
                "tool_use" => StopReason::ToolCall,
                _ => StopReason::EndOfTurn,
            };
            ParseResult::Emit(StreamEvent::done(stop_reason))
        }
        "error" => {
            let msg = v["error"]["message"]
                .as_str()
                .unwrap_or("unknown error")
                .to_string();
            ParseResult::Error(NuraError::Provider {
                provider: "anthropic".into(),
                detail: msg,
            })
        }
        _ => ParseResult::Continue,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use nura_core::provider::message::Message;

    #[test]
    fn split_system_separates_correctly() {
        let msgs = vec![Message::system("You are helpful."), Message::user("hello")];
        let (sys, api) = split_system(&msgs);
        assert_eq!(sys.as_deref(), Some("You are helpful."));
        assert_eq!(api.len(), 1);
        assert_eq!(api[0]["role"], "user");
    }

    #[test]
    fn build_body_has_stream() {
        let body = build_request_body(
            "claude-haiku-4-5-20251001",
            &None,
            &[],
            &SamplingParams::default(),
        );
        let v: serde_json::Value = serde_json::from_str(&body).unwrap();
        assert_eq!(v["stream"], true);
        assert_eq!(v["model"], "claude-haiku-4-5-20251001");
    }

    #[test]
    fn build_body_includes_system() {
        let body = build_request_body(
            "model",
            &Some("sys prompt".into()),
            &[],
            &SamplingParams::default(),
        );
        let v: serde_json::Value = serde_json::from_str(&body).unwrap();
        assert_eq!(v["system"], "sys prompt");
    }

    #[test]
    fn parse_content_block_delta_text() {
        let mut id: Option<String> = None;
        let mut name: Option<String> = None;
        let data =
            r#"{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}"#;
        let result = parse_event("content_block_delta", data, &mut id, &mut name);
        match result {
            ParseResult::Emit(StreamEvent::TokenDelta { text }) => assert_eq!(text, "hi"),
            _ => panic!("expected TokenDelta"),
        }
    }

    #[test]
    fn parse_message_stop_emits_done() {
        let mut id: Option<String> = None;
        let mut name: Option<String> = None;
        let data = r#"{"type":"message_stop"}"#;
        let result = parse_event("message_stop", data, &mut id, &mut name);
        match result {
            ParseResult::Emit(StreamEvent::Done { stop_reason }) => {
                assert_eq!(stop_reason, StopReason::EndOfTurn)
            }
            _ => panic!("expected Done"),
        }
    }

    #[test]
    fn cancel_before_complete() {
        let provider = AnthropicProvider::new("key", DEFAULT_BASE_URL, DEFAULT_MODEL);
        let msgs = vec![Message::user("ping")];
        let cancel = CancelToken::new();
        cancel.cancel();
        let events: Vec<_> = provider
            .complete(&msgs, &SamplingParams::default(), &cancel)
            .collect();
        assert_eq!(events.len(), 1);
        match events[0].as_ref().unwrap() {
            StreamEvent::Done { stop_reason } => assert_eq!(*stop_reason, StopReason::Cancel),
            _ => panic!("expected Done(Cancel)"),
        }
    }

    /// Integration test: requires ANTHROPIC_API_KEY env var.
    /// Run with: cargo test -- --ignored
    #[test]
    #[ignore]
    fn integration_streams_completion() {
        if std::env::var("ANTHROPIC_API_KEY").is_err() {
            return;
        }
        let provider = AnthropicProvider::from_secrets().expect("API key required");
        let msgs = vec![Message::user("Say 'hello' and nothing else.")];
        let cancel = CancelToken::new();
        let events: Vec<_> = provider
            .complete(
                &msgs,
                &SamplingParams {
                    max_tokens: 20,
                    ..Default::default()
                },
                &cancel,
            )
            .collect();
        let tokens: Vec<_> = events
            .iter()
            .filter_map(|r| r.as_ref().ok())
            .filter(|e| matches!(e, StreamEvent::TokenDelta { .. }))
            .collect();
        assert!(!tokens.is_empty(), "should stream at least one token");
    }
}
