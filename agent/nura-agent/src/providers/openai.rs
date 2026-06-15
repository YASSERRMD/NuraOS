use std::io::{BufRead, BufReader};
use std::thread;
use std::time::Duration;

use nura_core::error::{NuraError, Result};
use nura_core::provider::message::{Message, Role, StopReason, Usage};
use nura_core::provider::traits::{CancelToken, Capabilities, Provider, SamplingParams};
use nura_core::provider::StreamEvent;
use tracing::{debug, warn};

pub const DEFAULT_OPENAI_BASE_URL: &str = "https://api.openai.com";
pub const DEFAULT_MODEL: &str = "gpt-4o-mini";
const MAX_RETRIES: u32 = 3;

/// OpenAI-compatible chat completions provider.
///
/// Works with OpenAI, vLLM, LiteLLM, and any endpoint that implements the
/// `/v1/chat/completions` API. Base URL, model, and API key are all
/// configurable so the same struct covers many backends.
pub struct OpenAiCompatProvider {
    api_key: Option<String>,
    base_url: String,
    model: String,
}

impl OpenAiCompatProvider {
    pub fn new(
        api_key: impl Into<Option<String>>,
        base_url: impl Into<String>,
        model: impl Into<String>,
    ) -> Self {
        Self {
            api_key: api_key.into(),
            base_url: base_url.into(),
            model: model.into(),
        }
    }

    /// Build from environment secrets. Returns `None` if no key is configured.
    pub fn from_secrets() -> Option<Self> {
        use nura_core::secrets::Secrets;
        let secrets = Secrets::load().ok()?;
        let key = secrets.openai_api_key?.expose().to_string();
        Some(Self::new(Some(key), DEFAULT_OPENAI_BASE_URL, DEFAULT_MODEL))
    }

    fn completions_url(&self) -> String {
        format!(
            "{}/v1/chat/completions",
            self.base_url.trim_end_matches('/')
        )
    }

    fn send_with_retry(
        &self,
        body: &str,
        cancel: &CancelToken,
        retries_left: u32,
    ) -> Result<ureq::Response> {
        let mut attempt = 0u32;
        loop {
            if cancel.is_cancelled() {
                return Err(NuraError::Provider {
                    provider: self.model.clone(),
                    detail: "cancelled before send".into(),
                });
            }

            let mut req = ureq::post(&self.completions_url())
                .set("Content-Type", "application/json")
                .timeout(Duration::from_secs(120));

            if let Some(key) = &self.api_key {
                req = req.set("Authorization", &format!("Bearer {}", key));
            }

            match req.send_string(body) {
                Ok(resp) => return Ok(resp),
                Err(ureq::Error::Status(429, resp)) => {
                    if attempt >= retries_left {
                        return Err(NuraError::Provider {
                            provider: self.model.clone(),
                            detail: format!("rate limit after {} retries", retries_left),
                        });
                    }
                    let retry_after = resp
                        .header("retry-after")
                        .and_then(|v| v.parse::<u64>().ok())
                        .unwrap_or(2u64.pow(attempt + 1));
                    warn!("openai-compat: rate limited; retrying in {}s", retry_after);
                    thread::sleep(Duration::from_secs(retry_after));
                }
                Err(ureq::Error::Status(status, _)) if status >= 500 => {
                    if attempt >= retries_left {
                        return Err(NuraError::Provider {
                            provider: self.model.clone(),
                            detail: format!(
                                "server error {} after {} retries",
                                status, retries_left
                            ),
                        });
                    }
                    let delay = 2u64.pow(attempt + 1);
                    warn!(
                        "openai-compat: server error {}; retrying in {}s",
                        status, delay
                    );
                    thread::sleep(Duration::from_secs(delay));
                }
                Err(ureq::Error::Status(status, _)) => {
                    return Err(NuraError::Provider {
                        provider: self.model.clone(),
                        detail: format!("HTTP {}", status),
                    });
                }
                Err(e) => {
                    return Err(NuraError::Provider {
                        provider: self.model.clone(),
                        detail: format!("request error: {}", e),
                    });
                }
            }
            attempt += 1;
        }
    }
}

impl Provider for OpenAiCompatProvider {
    fn name(&self) -> &str {
        &self.model
    }

    fn capabilities(&self) -> Capabilities {
        Capabilities {
            streaming: true,
            tool_calling: true,
            system_messages: true,
            max_context_tokens: 128_000,
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

        let body = build_request_body(&self.model, messages, params);
        debug!(model = %self.model, "openai-compat: sending request");

        match self.send_with_retry(&body, cancel, MAX_RETRIES) {
            Err(e) => Box::new(std::iter::once(Err(e))),
            Ok(resp) => {
                let reader = BufReader::new(resp.into_reader());
                Box::new(OpenAiStream {
                    reader,
                    cancel: cancel.clone(),
                    done: false,
                    pending_tool_calls: std::collections::HashMap::new(),
                })
            }
        }
    }
}

// ---- request building ----

fn build_request_body(model: &str, messages: &[Message], params: &SamplingParams) -> String {
    let api_msgs: Vec<serde_json::Value> = messages.iter().map(message_to_api_value).collect();

    let mut obj = serde_json::json!({
        "model": model,
        "max_tokens": params.max_tokens,
        "temperature": params.temperature,
        "top_p": params.top_p,
        "stream": true,
        "stream_options": {"include_usage": true},
        "messages": api_msgs,
    });

    if !params.stop.is_empty() {
        obj["stop"] = serde_json::Value::Array(
            params
                .stop
                .iter()
                .map(|s| serde_json::Value::String(s.clone()))
                .collect(),
        );
    }

    obj.to_string()
}

fn message_to_api_value(msg: &Message) -> serde_json::Value {
    use nura_core::provider::message::ContentPart;

    let role = match msg.role {
        Role::System => "system",
        Role::User => "user",
        Role::Assistant => "assistant",
        Role::Tool => "tool",
    };

    // Flatten text parts to a single string for simple messages.
    let has_tool_parts = msg
        .parts
        .iter()
        .any(|p| !matches!(p, ContentPart::Text { .. }));

    if !has_tool_parts {
        let text = msg
            .parts
            .iter()
            .filter_map(|p| match p {
                ContentPart::Text { text } => Some(text.as_str()),
                _ => None,
            })
            .collect::<Vec<_>>()
            .join("\n");
        return serde_json::json!({"role": role, "content": text});
    }

    let content: Vec<serde_json::Value> = msg
        .parts
        .iter()
        .map(|part| match part {
            ContentPart::Text { text } => serde_json::json!({"type": "text", "text": text}),
            ContentPart::ToolCallRequest {
                id,
                name,
                arguments,
            } => {
                serde_json::json!({
                    "type": "function",
                    "id": id,
                    "function": {"name": name, "arguments": arguments.to_string()},
                })
            }
            ContentPart::ToolCallResult {
                call_id,
                output,
                error,
            } => {
                let content_str = if let Some(err) = error {
                    format!("error: {}", err)
                } else {
                    output.to_string()
                };
                serde_json::json!({
                    "role": "tool",
                    "tool_call_id": call_id,
                    "content": content_str,
                })
            }
        })
        .collect();

    serde_json::json!({"role": role, "content": content})
}

// ---- streaming ----

struct OpenAiStream {
    reader: BufReader<Box<dyn std::io::Read + Send + Sync + 'static>>,
    cancel: CancelToken,
    done: bool,
    /// Accumulates partial tool call data keyed by index.
    pending_tool_calls: std::collections::HashMap<u64, PartialToolCall>,
}

#[derive(Default)]
struct PartialToolCall {
    id: String,
    name: String,
    arguments: String,
}

impl Iterator for OpenAiStream {
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
                        provider: "openai-compat".into(),
                        detail: format!("stream read error: {}", e),
                    }));
                }
            }

            let trimmed = line.trim();
            if trimmed == "data: [DONE]" {
                self.done = true;
                return Some(Ok(StreamEvent::done(StopReason::EndOfTurn)));
            }

            let json_str = match trimmed.strip_prefix("data: ") {
                Some(s) if !s.is_empty() => s,
                _ => continue,
            };

            let v: serde_json::Value = match serde_json::from_str(json_str) {
                Ok(v) => v,
                Err(e) => {
                    warn!("openai-compat: bad SSE JSON: {}", e);
                    continue;
                }
            };

            if let Some(err) = v.get("error") {
                let msg = err["message"]
                    .as_str()
                    .unwrap_or("unknown error")
                    .to_string();
                self.done = true;
                return Some(Err(NuraError::Provider {
                    provider: "openai-compat".into(),
                    detail: msg,
                }));
            }

            // Usage chunk (sent when stream_options.include_usage is true).
            if let Some(usage) = v.get("usage").filter(|u| !u.is_null()) {
                let ev = StreamEvent::Usage(Usage {
                    prompt_tokens: usage["prompt_tokens"].as_u64().unwrap_or(0) as u32,
                    completion_tokens: usage["completion_tokens"].as_u64().unwrap_or(0) as u32,
                });
                return Some(Ok(ev));
            }

            let choices = match v["choices"].as_array() {
                Some(c) if !c.is_empty() => c,
                _ => continue,
            };

            let choice = &choices[0];
            let finish_reason = choice["finish_reason"].as_str();

            if let Some(reason) = finish_reason {
                if !reason.is_empty() {
                    let stop_reason = match reason {
                        "length" => StopReason::MaxTokens,
                        "tool_calls" => StopReason::ToolCall,
                        "stop" | _ => StopReason::EndOfTurn,
                    };
                    self.done = true;
                    return Some(Ok(StreamEvent::done(stop_reason)));
                }
            }

            let delta = &choice["delta"];

            if let Some(text) = delta["content"].as_str() {
                if !text.is_empty() {
                    return Some(Ok(StreamEvent::token(text)));
                }
            }

            if let Some(tool_calls) = delta["tool_calls"].as_array() {
                for tc in tool_calls {
                    let idx = tc["index"].as_u64().unwrap_or(0);
                    let entry = self.pending_tool_calls.entry(idx).or_default();

                    if let Some(id) = tc["id"].as_str() {
                        entry.id = id.to_string();
                    }
                    if let Some(name) = tc["function"]["name"].as_str() {
                        entry.name = name.to_string();
                    }
                    if let Some(args) = tc["function"]["arguments"].as_str() {
                        entry.arguments.push_str(args);

                        let first_name = if entry.arguments == args && !entry.name.is_empty() {
                            Some(entry.name.clone())
                        } else {
                            None
                        };

                        return Some(Ok(StreamEvent::ToolCallDelta {
                            id: entry.id.clone(),
                            name: first_name,
                            arguments_chunk: args.to_string(),
                        }));
                    }
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn build_body_stream_true() {
        let body = build_request_body("gpt-4o-mini", &[], &SamplingParams::default());
        let v: serde_json::Value = serde_json::from_str(&body).unwrap();
        assert_eq!(v["stream"], true);
        assert_eq!(v["model"], "gpt-4o-mini");
    }

    #[test]
    fn simple_message_is_string_content() {
        let msg = Message::user("hello");
        let v = message_to_api_value(&msg);
        assert_eq!(v["role"], "user");
        assert_eq!(v["content"], "hello");
    }

    #[test]
    fn cancel_before_complete() {
        let provider = OpenAiCompatProvider::new(None, DEFAULT_OPENAI_BASE_URL, DEFAULT_MODEL);
        let cancel = CancelToken::new();
        cancel.cancel();
        let events: Vec<_> = provider
            .complete(&[], &SamplingParams::default(), &cancel)
            .collect();
        assert_eq!(events.len(), 1);
        match events[0].as_ref().unwrap() {
            StreamEvent::Done { stop_reason } => assert_eq!(*stop_reason, StopReason::Cancel),
            _ => panic!("expected Done(Cancel)"),
        }
    }

    #[test]
    fn done_sentinel_parsed() {
        // Simulate the [DONE] line coming through the stream iterator directly.
        // We verify that a `data: [DONE]` terminates gracefully via the SSE reader.
        let cursor = std::io::Cursor::new(b"data: [DONE]\n\n".to_vec());
        let reader =
            BufReader::new(Box::new(cursor) as Box<dyn std::io::Read + Send + Sync + 'static>);
        let mut stream = OpenAiStream {
            reader,
            cancel: CancelToken::new(),
            done: false,
            pending_tool_calls: Default::default(),
        };
        let ev = stream.next().unwrap().unwrap();
        match ev {
            StreamEvent::Done { stop_reason } => assert_eq!(stop_reason, StopReason::EndOfTurn),
            _ => panic!("expected Done"),
        }
        assert!(stream.next().is_none(), "stream should be exhausted");
    }

    #[test]
    fn token_delta_parsed() {
        let line = r#"data: {"id":"x","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}"#;
        let cursor = std::io::Cursor::new(format!("{}\n", line).into_bytes());
        let reader =
            BufReader::new(Box::new(cursor) as Box<dyn std::io::Read + Send + Sync + 'static>);
        let mut stream = OpenAiStream {
            reader,
            cancel: CancelToken::new(),
            done: false,
            pending_tool_calls: Default::default(),
        };
        let ev = stream.next().unwrap().unwrap();
        match ev {
            StreamEvent::TokenDelta { text } => assert_eq!(text, "hello"),
            _ => panic!("expected TokenDelta"),
        }
    }

    /// Integration test: requires OPENAI_API_KEY or OPENAI_BASE_URL env var.
    #[test]
    #[ignore]
    fn integration_streams_completion() {
        if std::env::var("OPENAI_API_KEY").is_err() && std::env::var("OPENAI_BASE_URL").is_err() {
            return;
        }
        let provider = if let Some(p) = OpenAiCompatProvider::from_secrets() {
            p
        } else {
            let base =
                std::env::var("OPENAI_BASE_URL").unwrap_or_else(|_| DEFAULT_OPENAI_BASE_URL.into());
            OpenAiCompatProvider::new(None, base, DEFAULT_MODEL)
        };
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
        assert!(!tokens.is_empty(), "should receive at least one token");
    }
}
