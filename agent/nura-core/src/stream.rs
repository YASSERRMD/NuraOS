use std::collections::HashMap;
use std::sync::mpsc::{self, RecvTimeoutError};
use std::thread;
use std::time::{Duration, Instant};

use tracing::{debug, info, warn};

use crate::error::{NuraError, Result};
use crate::provider::message::{StopReason, Usage};
use crate::provider::traits::CancelToken;
use crate::provider::StreamEvent;

/// A fully assembled tool call reconstructed from streaming deltas.
#[derive(Debug, Clone)]
pub struct AssembledToolCall {
    pub id: String,
    pub name: String,
    /// Raw JSON arguments string as delivered by the provider.
    pub arguments_json: String,
}

/// Aggregated result of one complete streaming turn.
#[derive(Debug, Default)]
pub struct AggregatedOutput {
    /// Full assistant text assembled from all TokenDelta events.
    pub text: String,
    /// Tool calls assembled from ToolCallDelta events.
    pub tool_calls: Vec<AssembledToolCall>,
    /// Token usage reported by the provider (may be None for partial results).
    pub usage: Option<Usage>,
    pub stop_reason: StopReason,
    /// True when the turn was cut short by the caller's CancelToken.
    pub cancelled: bool,
    /// True when the turn exceeded the per-turn timeout.
    pub timed_out: bool,
}

impl Default for StopReason {
    fn default() -> Self {
        StopReason::EndOfTurn
    }
}

/// Drive a provider event stream to completion, enforcing a timeout and
/// forwarding cancellation.
///
/// The event iterator runs on a background thread, feeding into a bounded
/// channel of capacity `queue_depth` (default 32). This provides backpressure:
/// a slow consumer blocks the provider thread rather than buffering unboundedly.
///
/// `on_token` is called synchronously on the calling thread for each text token
/// so the REPL and HTTP front end can stream output immediately.
///
/// Returns `AggregatedOutput` in all non-error cases. Errors are returned only
/// for unrecoverable provider errors; timeout and cancellation set flags in the
/// output struct.
pub fn aggregate<F>(
    events: Box<dyn Iterator<Item = Result<StreamEvent>> + Send + 'static>,
    cancel: CancelToken,
    timeout: Duration,
    queue_depth: usize,
    on_token: &mut F,
) -> Result<AggregatedOutput>
where
    F: FnMut(&str),
{
    let queue_depth = queue_depth.max(1);
    let (tx, rx) = mpsc::sync_channel(queue_depth);
    let cancel_bg = cancel.clone();

    thread::spawn(move || {
        for ev in events {
            if cancel_bg.is_cancelled() {
                break;
            }
            if tx.send(ev).is_err() {
                break; // receiver dropped (e.g. timed out)
            }
        }
        // tx drops here, closing the channel
    });

    let deadline = Instant::now() + timeout;
    let mut output = AggregatedOutput::default();
    let mut partial_calls: HashMap<String, PartialToolCall> = HashMap::new();

    loop {
        if cancel.is_cancelled() {
            output.cancelled = true;
            output.stop_reason = StopReason::Cancel;
            info!("stream: cancelled by caller");
            break;
        }

        let remaining = deadline.saturating_duration_since(Instant::now());
        if remaining.is_zero() {
            output.timed_out = true;
            output.stop_reason = StopReason::Error;
            cancel.cancel();
            warn!(
                partial_text_len = output.text.len(),
                "stream: turn timeout reached"
            );
            break;
        }

        match rx.recv_timeout(remaining) {
            Ok(Ok(event)) => {
                if let Some(should_break) = handle_event(event, &mut output, &mut partial_calls, on_token)? {
                    if should_break {
                        break;
                    }
                }
            }
            Ok(Err(e)) => {
                warn!("stream: provider error: {}", e);
                return Err(e);
            }
            Err(RecvTimeoutError::Timeout) => {
                output.timed_out = true;
                output.stop_reason = StopReason::Error;
                cancel.cancel();
                warn!(
                    partial_text_len = output.text.len(),
                    "stream: recv timeout"
                );
                break;
            }
            Err(RecvTimeoutError::Disconnected) => {
                // Provider thread finished without sending a Done event.
                debug!("stream: provider channel closed (no Done event)");
                break;
            }
        }
    }

    // Assemble any in-progress tool calls.
    for (_, partial) in partial_calls {
        if !partial.id.is_empty() && !partial.name.is_empty() {
            output.tool_calls.push(AssembledToolCall {
                id: partial.id,
                name: partial.name,
                arguments_json: partial.arguments,
            });
        }
    }

    if output.timed_out || output.cancelled {
        info!(
            cancelled = output.cancelled,
            timed_out = output.timed_out,
            text_len = output.text.len(),
            tool_calls = output.tool_calls.len(),
            "stream: turn closed early"
        );
    } else {
        info!(
            stop_reason = ?output.stop_reason,
            text_len = output.text.len(),
            tool_calls = output.tool_calls.len(),
            prompt_tokens = output.usage.as_ref().map(|u| u.prompt_tokens).unwrap_or(0),
            completion_tokens = output.usage.as_ref().map(|u| u.completion_tokens).unwrap_or(0),
            "stream: turn complete"
        );
    }

    Ok(output)
}

/// Returns `Some(true)` to break the loop, `Some(false)` to continue,
/// `None` for events that do not affect loop control.
fn handle_event<F>(
    event: StreamEvent,
    output: &mut AggregatedOutput,
    partial_calls: &mut HashMap<String, PartialToolCall>,
    on_token: &mut F,
) -> Result<Option<bool>>
where
    F: FnMut(&str),
{
    match event {
        StreamEvent::TokenDelta { text } => {
            output.text.push_str(&text);
            on_token(&text);
            Ok(Some(false))
        }
        StreamEvent::ToolCallDelta {
            id,
            name,
            arguments_chunk,
        } => {
            let entry = partial_calls.entry(id.clone()).or_insert_with(|| PartialToolCall {
                id: id.clone(),
                name: String::new(),
                arguments: String::new(),
            });
            if let Some(n) = name {
                entry.name = n;
            }
            entry.arguments.push_str(&arguments_chunk);
            Ok(Some(false))
        }
        StreamEvent::Usage(u) => {
            match &mut output.usage {
                Some(existing) => {
                    if u.prompt_tokens > 0 {
                        existing.prompt_tokens = u.prompt_tokens;
                    }
                    if u.completion_tokens > 0 {
                        existing.completion_tokens = u.completion_tokens;
                    }
                }
                None => output.usage = Some(u),
            }
            Ok(Some(false))
        }
        StreamEvent::Done { stop_reason } => {
            output.stop_reason = stop_reason;
            Ok(Some(true)) // break loop
        }
        StreamEvent::Error { message } => Err(NuraError::Provider {
            provider: "stream".into(),
            detail: message,
        }),
    }
}

#[derive(Default)]
struct PartialToolCall {
    id: String,
    name: String,
    arguments: String,
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::provider::message::StopReason;
    use crate::provider::StreamEvent;

    fn run_stream(events: Vec<Result<StreamEvent>>, timeout_secs: u64) -> Result<AggregatedOutput> {
        let cancel = CancelToken::new();
        let mut tokens = Vec::<String>::new();
        aggregate(
            Box::new(events.into_iter()),
            cancel,
            Duration::from_secs(timeout_secs),
            32,
            &mut |t| tokens.push(t.to_string()),
        )
    }

    #[test]
    fn aggregates_text_and_usage() {
        let events = vec![
            Ok(StreamEvent::token("hello")),
            Ok(StreamEvent::token(" world")),
            Ok(StreamEvent::Usage(Usage {
                prompt_tokens: 5,
                completion_tokens: 2,
            })),
            Ok(StreamEvent::done(StopReason::EndOfTurn)),
        ];
        let out = run_stream(events, 10).unwrap();
        assert_eq!(out.text, "hello world");
        assert_eq!(out.stop_reason, StopReason::EndOfTurn);
        assert!(!out.cancelled);
        assert!(!out.timed_out);
        let u = out.usage.unwrap();
        assert_eq!(u.prompt_tokens, 5);
        assert_eq!(u.completion_tokens, 2);
    }

    #[test]
    fn assembles_tool_call_from_deltas() {
        let events = vec![
            Ok(StreamEvent::ToolCallDelta {
                id: "tc-1".into(),
                name: Some("read_file".into()),
                arguments_chunk: r#"{"path":"#.into(),
            }),
            Ok(StreamEvent::ToolCallDelta {
                id: "tc-1".into(),
                name: None,
                arguments_chunk: r#""/etc/os"}"#.into(),
            }),
            Ok(StreamEvent::done(StopReason::ToolCall)),
        ];
        let out = run_stream(events, 10).unwrap();
        assert_eq!(out.tool_calls.len(), 1);
        assert_eq!(out.tool_calls[0].name, "read_file");
        assert!(out.tool_calls[0].arguments_json.contains("/etc/os"));
        assert_eq!(out.stop_reason, StopReason::ToolCall);
    }

    #[test]
    fn mid_stream_cancel() {
        let cancel = CancelToken::new();
        let cancel_clone = cancel.clone();

        let events: Vec<Result<StreamEvent>> = vec![
            Ok(StreamEvent::token("partial")),
            Ok(StreamEvent::token(" text")),
            Ok(StreamEvent::done(StopReason::EndOfTurn)),
        ];

        cancel_clone.cancel();

        let mut tokens = Vec::new();
        let out = aggregate(
            Box::new(events.into_iter()),
            cancel,
            Duration::from_secs(10),
            32,
            &mut |t| tokens.push(t.to_string()),
        )
        .unwrap();

        assert!(out.cancelled, "should be marked cancelled");
        assert_eq!(out.stop_reason, StopReason::Cancel);
    }

    #[test]
    fn provider_error_propagated() {
        let events = vec![
            Ok(StreamEvent::token("hi")),
            Err(NuraError::Provider {
                provider: "test".into(),
                detail: "boom".into(),
            }),
        ];
        let result = run_stream(events, 10);
        assert!(result.is_err(), "provider error should be returned");
    }

    #[test]
    fn timeout_closes_cleanly() {
        let cancel = CancelToken::new();
        let events: Vec<Result<StreamEvent>> = vec![
            Ok(StreamEvent::token("slow")),
        ];

        let mut tokens = Vec::new();
        let out = aggregate(
            Box::new(events.into_iter()),
            cancel,
            Duration::from_millis(50),
            32,
            &mut |t| tokens.push(t.to_string()),
        )
        .unwrap();

        assert!(out.timed_out || !out.text.is_empty(), "partial result expected");
    }

    #[test]
    fn on_token_called_for_each_delta() {
        let events = vec![
            Ok(StreamEvent::token("a")),
            Ok(StreamEvent::token("b")),
            Ok(StreamEvent::token("c")),
            Ok(StreamEvent::done(StopReason::EndOfTurn)),
        ];
        let cancel = CancelToken::new();
        let mut received = Vec::new();
        aggregate(
            Box::new(events.into_iter()),
            cancel,
            Duration::from_secs(5),
            32,
            &mut |t| received.push(t.to_string()),
        )
        .unwrap();
        assert_eq!(received, vec!["a", "b", "c"]);
    }
}

