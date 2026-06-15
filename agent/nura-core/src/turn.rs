use std::time::{Duration, Instant};

use tracing::{debug, info, warn};

use crate::error::{NuraError, Result};
use crate::provider::message::{ContentPart, Message, Role, StopReason, Usage};
use crate::provider::traits::{CancelToken, Provider, SamplingParams};
use crate::stream::{aggregate_sync, AggregatedOutput, AssembledToolCall};
use crate::tool::{ToolBudget, ToolRegistry};

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

/// Budget and timeout parameters for a single user-visible turn.
#[derive(Debug, Clone)]
pub struct TurnConfig {
    /// Maximum number of provider calls (1 + tool-call iterations) per turn.
    pub max_iterations: u32,
    /// Wall-clock budget for the entire turn, measured from `run_turn()` entry.
    pub turn_timeout: Duration,
    /// Per-tool-call wall-clock limit forwarded to `ToolRegistry::call`.
    pub call_timeout: Duration,
    /// Maximum total tool invocations across all iterations in the turn.
    pub tool_budget: u32,
}

impl Default for TurnConfig {
    fn default() -> Self {
        Self {
            max_iterations: 10,
            turn_timeout: Duration::from_secs(120),
            call_timeout: Duration::from_secs(30),
            tool_budget: 20,
        }
    }
}

// ---------------------------------------------------------------------------
// Transcript
// ---------------------------------------------------------------------------

/// One recorded step in a turn's provenance log.
#[derive(Debug, Clone)]
pub enum TurnEvent {
    /// A provider response (may include partial text before a tool call).
    ModelResponse {
        text: String,
        tool_calls: Vec<AssembledToolCall>,
        usage: Option<Usage>,
    },
    /// A tool call dispatched during this iteration.
    ToolCall {
        id: String,
        name: String,
        arguments_json: String,
    },
    /// The result (or error) returned by a tool.
    ToolResult {
        call_id: String,
        output: serde_json::Value,
        error: Option<String>,
    },
}

// ---------------------------------------------------------------------------
// Outcome
// ---------------------------------------------------------------------------

/// Complete result of one user-facing turn.
#[derive(Debug)]
pub struct TurnOutcome {
    /// Final assistant text (empty when the turn ended on a budget error).
    pub final_text: String,
    /// Ordered provenance log of model responses and tool calls.
    pub transcript: Vec<TurnEvent>,
    /// Reason inference stopped on the last provider call.
    pub stop_reason: StopReason,
    /// Accumulated token usage across all provider calls in the turn.
    pub total_usage: Usage,
    /// Number of provider calls made (1 for a plain answer, N for N-1 tool rounds).
    pub iterations: u32,
    /// Total elapsed wall-clock time.
    pub elapsed: Duration,
    /// True when the caller cancelled via `CancelToken`.
    pub cancelled: bool,
    /// True when the turn exceeded `TurnConfig::turn_timeout`.
    pub timed_out: bool,
}

// ---------------------------------------------------------------------------
// Turn runner
// ---------------------------------------------------------------------------

/// Orchestrate a multi-step tool-using turn.
///
/// Sends `messages` (mutable so tool results can be appended) to `provider`,
/// detects `ToolCall` stop reasons, dispatches tools via `registry`, feeds
/// results back, and repeats until a final answer or a budget is exhausted.
///
/// Tool calls within each iteration are executed **sequentially** and in the
/// order the model requested them. Concurrent dispatch is only safe when all
/// tools in a batch are read-only and independent; that optimisation is left
/// to a future phase.
///
/// `on_token` is called for each text token as it arrives so the caller can
/// stream output to the user incrementally.
pub fn run_turn<F>(
    messages: &mut Vec<Message>,
    provider: &dyn Provider,
    params: &SamplingParams,
    registry: &ToolRegistry,
    config: &TurnConfig,
    cancel: CancelToken,
    on_token: &mut F,
) -> Result<TurnOutcome>
where
    F: FnMut(&str),
{
    let turn_start = Instant::now();
    let deadline = turn_start + config.turn_timeout;
    let mut tool_budget = ToolBudget::new(config.tool_budget);
    let mut transcript: Vec<TurnEvent> = Vec::new();
    let mut total_usage = Usage::default();
    let mut iterations = 0u32;

    let outcome = loop {
        if cancel.is_cancelled() {
            break TurnOutcome {
                final_text: String::new(),
                transcript,
                stop_reason: StopReason::Cancel,
                total_usage,
                iterations,
                elapsed: turn_start.elapsed(),
                cancelled: true,
                timed_out: false,
            };
        }

        let now = Instant::now();
        if now >= deadline {
            warn!(iterations, "turn: turn_timeout exceeded");
            break TurnOutcome {
                final_text: String::new(),
                transcript,
                stop_reason: StopReason::Error,
                total_usage,
                iterations,
                elapsed: turn_start.elapsed(),
                cancelled: false,
                timed_out: true,
            };
        }

        if iterations >= config.max_iterations {
            warn!(iterations, max = config.max_iterations, "turn: max_iterations exceeded");
            return Err(NuraError::BudgetExceeded(format!(
                "turn exceeded max_iterations ({})",
                config.max_iterations
            )));
        }

        iterations += 1;
        debug!(iteration = iterations, "turn: calling provider");

        let events = provider.complete(messages, params, &cancel);
        let agg = aggregate_sync(events, &cancel, on_token)?;

        if let Some(ref u) = agg.usage {
            total_usage.prompt_tokens += u.prompt_tokens;
            total_usage.completion_tokens += u.completion_tokens;
        }

        transcript.push(TurnEvent::ModelResponse {
            text: agg.text.clone(),
            tool_calls: agg.tool_calls.clone(),
            usage: agg.usage.clone(),
        });

        if agg.cancelled {
            break TurnOutcome {
                final_text: agg.text,
                transcript,
                stop_reason: StopReason::Cancel,
                total_usage,
                iterations,
                elapsed: turn_start.elapsed(),
                cancelled: true,
                timed_out: false,
            };
        }

        if agg.stop_reason != StopReason::ToolCall || agg.tool_calls.is_empty() {
            info!(
                iterations,
                stop_reason = ?agg.stop_reason,
                text_len = agg.text.len(),
                "turn: complete"
            );
            // Append the final assistant reply to conversation history.
            if !agg.text.is_empty() {
                messages.push(Message::assistant(agg.text.clone()));
            }
            break TurnOutcome {
                final_text: agg.text,
                transcript,
                stop_reason: agg.stop_reason,
                total_usage,
                iterations,
                elapsed: turn_start.elapsed(),
                cancelled: false,
                timed_out: false,
            };
        }

        // Tool-call iteration: append assistant message and execute tools.
        append_assistant_message(messages, &agg);
        let tool_results = execute_tools(&agg.tool_calls, registry, config, &mut tool_budget, &mut transcript);
        append_tool_results(messages, tool_results);
    };

    Ok(outcome)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

fn append_assistant_message(messages: &mut Vec<Message>, agg: &AggregatedOutput) {
    let mut parts = Vec::new();
    if !agg.text.is_empty() {
        parts.push(ContentPart::text(agg.text.clone()));
    }
    for tc in &agg.tool_calls {
        let arguments = serde_json::from_str(&tc.arguments_json)
            .unwrap_or(serde_json::Value::Null);
        parts.push(ContentPart::ToolCallRequest {
            id: tc.id.clone(),
            name: tc.name.clone(),
            arguments,
        });
    }
    if !parts.is_empty() {
        messages.push(Message {
            role: Role::Assistant,
            parts,
        });
    }
}

/// Execute tool calls sequentially (preserving model-requested order).
fn execute_tools(
    calls: &[AssembledToolCall],
    registry: &ToolRegistry,
    config: &TurnConfig,
    budget: &mut ToolBudget,
    transcript: &mut Vec<TurnEvent>,
) -> Vec<(String, serde_json::Value, Option<String>)> {
    let mut results = Vec::new();

    for tc in calls {
        transcript.push(TurnEvent::ToolCall {
            id: tc.id.clone(),
            name: tc.name.clone(),
            arguments_json: tc.arguments_json.clone(),
        });

        let args: serde_json::Value = serde_json::from_str(&tc.arguments_json)
            .unwrap_or(serde_json::Value::Null);

        let (output, error) = match registry.call(&tc.name, args, config.call_timeout, budget) {
            Ok(r) => (r.output, None),
            Err(e) => {
                warn!(tool = %tc.name, error = %e, "turn: tool call failed");
                (serde_json::Value::Null, Some(e.to_string()))
            }
        };

        transcript.push(TurnEvent::ToolResult {
            call_id: tc.id.clone(),
            output: output.clone(),
            error: error.clone(),
        });

        results.push((tc.id.clone(), output, error));
    }

    results
}

fn append_tool_results(
    messages: &mut Vec<Message>,
    results: Vec<(String, serde_json::Value, Option<String>)>,
) {
    if results.is_empty() {
        return;
    }
    let parts: Vec<ContentPart> = results
        .into_iter()
        .map(|(call_id, output, error)| ContentPart::ToolCallResult {
            call_id,
            output,
            error,
        })
        .collect();
    messages.push(Message {
        role: Role::Tool,
        parts,
    });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;
    use crate::provider::message::StopReason;
    use crate::provider::StreamEvent;
    use crate::tool::EchoTool;

    // ---- Scripted stub provider ----

    struct ScriptedProvider {
        turns: std::sync::Mutex<Vec<Vec<crate::error::Result<StreamEvent>>>>,
    }

    impl ScriptedProvider {
        fn new(turns: Vec<Vec<crate::error::Result<StreamEvent>>>) -> Self {
            Self {
                turns: std::sync::Mutex::new(turns),
            }
        }
    }

    impl Provider for ScriptedProvider {
        fn name(&self) -> &str {
            "scripted"
        }

        fn capabilities(&self) -> crate::provider::traits::Capabilities {
            crate::provider::traits::Capabilities {
                streaming: true,
                tool_calling: true,
                system_messages: false,
                max_context_tokens: 4096,
            }
        }

        fn complete(
            &self,
            _messages: &[Message],
            _params: &SamplingParams,
            _cancel: &CancelToken,
        ) -> Box<dyn Iterator<Item = crate::error::Result<StreamEvent>> + Send + '_> {
            let mut lock = self.turns.lock().unwrap();
            let events = if lock.is_empty() {
                vec![Ok(StreamEvent::done(StopReason::EndOfTurn))]
            } else {
                lock.remove(0)
            };
            Box::new(events.into_iter())
        }
    }

    fn echo_registry() -> ToolRegistry {
        let mut r = ToolRegistry::new();
        r.register(EchoTool);
        r.allowlist("echo");
        r
    }

    fn fast_config() -> TurnConfig {
        TurnConfig {
            max_iterations: 5,
            turn_timeout: Duration::from_secs(30),
            call_timeout: Duration::from_secs(5),
            tool_budget: 10,
        }
    }

    // ---- Tests ----

    #[test]
    fn single_iteration_plain_answer() {
        let provider = ScriptedProvider::new(vec![vec![
            Ok(StreamEvent::token("hello world")),
            Ok(StreamEvent::done(StopReason::EndOfTurn)),
        ]]);
        let mut msgs = vec![Message::user("hi")];
        let registry = echo_registry();
        let cancel = CancelToken::new();
        let mut tokens = Vec::new();

        let outcome = run_turn(
            &mut msgs,
            &provider,
            &SamplingParams::default(),
            &registry,
            &fast_config(),
            cancel,
            &mut |t| tokens.push(t.to_string()),
        )
        .unwrap();

        assert_eq!(outcome.final_text, "hello world");
        assert_eq!(outcome.iterations, 1);
        assert!(!outcome.cancelled);
        assert!(!outcome.timed_out);
        assert_eq!(tokens, vec!["hello world"]);
    }

    #[test]
    fn tool_call_then_final_answer() {
        let provider = ScriptedProvider::new(vec![
            // First provider call: requests echo tool
            vec![
                Ok(StreamEvent::ToolCallDelta {
                    id: "tc1".into(),
                    name: Some("echo".into()),
                    arguments_chunk: r#"{"message":"ping"}"#.into(),
                }),
                Ok(StreamEvent::done(StopReason::ToolCall)),
            ],
            // Second provider call: final answer
            vec![
                Ok(StreamEvent::token("pong")),
                Ok(StreamEvent::done(StopReason::EndOfTurn)),
            ],
        ]);

        let mut msgs = vec![Message::user("use echo")];
        let registry = echo_registry();
        let cancel = CancelToken::new();

        let outcome = run_turn(
            &mut msgs,
            &provider,
            &SamplingParams::default(),
            &registry,
            &fast_config(),
            cancel,
            &mut |_| {},
        )
        .unwrap();

        assert_eq!(outcome.final_text, "pong");
        assert_eq!(outcome.iterations, 2);

        // Transcript: ModelResponse (tool call) + ToolCall + ToolResult + ModelResponse (final)
        assert_eq!(outcome.transcript.len(), 4);

        // Check tool result was added to messages
        assert_eq!(msgs.len(), 4); // user + assistant + tool-results + final (only provider calls add)
    }

    #[test]
    fn max_iterations_budget_exceeded() {
        // Provider always requests a tool call -- never gives a final answer.
        let turns: Vec<Vec<_>> = (0..10)
            .map(|i| {
                vec![
                    Ok(StreamEvent::ToolCallDelta {
                        id: format!("tc{}", i),
                        name: Some("echo".into()),
                        arguments_chunk: r#"{"message":"loop"}"#.into(),
                    }),
                    Ok(StreamEvent::done(StopReason::ToolCall)),
                ]
            })
            .collect();

        let provider = ScriptedProvider::new(turns);
        let mut msgs = vec![Message::user("loop forever")];
        let registry = echo_registry();
        let cancel = CancelToken::new();

        let config = TurnConfig {
            max_iterations: 3,
            ..fast_config()
        };

        let result = run_turn(
            &mut msgs,
            &provider,
            &SamplingParams::default(),
            &registry,
            &config,
            cancel,
            &mut |_| {},
        );

        assert!(result.is_err(), "should return BudgetExceeded");
        if let Err(NuraError::BudgetExceeded(msg)) = result {
            assert!(msg.contains("max_iterations"), "got: {}", msg);
        } else {
            panic!("expected BudgetExceeded error");
        }
    }

    #[test]
    fn cancellation_stops_cleanly() {
        let cancel = CancelToken::new();
        cancel.cancel(); // cancel before the turn starts

        let provider = ScriptedProvider::new(vec![vec![
            Ok(StreamEvent::token("should not appear")),
            Ok(StreamEvent::done(StopReason::EndOfTurn)),
        ]]);

        let mut msgs = vec![Message::user("hi")];
        let registry = echo_registry();

        let outcome = run_turn(
            &mut msgs,
            &provider,
            &SamplingParams::default(),
            &registry,
            &fast_config(),
            cancel,
            &mut |_| {},
        )
        .unwrap();

        assert!(outcome.cancelled);
        assert_eq!(outcome.stop_reason, StopReason::Cancel);
    }

    #[test]
    fn tool_call_transcript_is_ordered() {
        let provider = ScriptedProvider::new(vec![
            vec![
                Ok(StreamEvent::ToolCallDelta {
                    id: "a".into(),
                    name: Some("echo".into()),
                    arguments_chunk: r#"{"message":"first"}"#.into(),
                }),
                Ok(StreamEvent::ToolCallDelta {
                    id: "b".into(),
                    name: Some("echo".into()),
                    arguments_chunk: r#"{"message":"second"}"#.into(),
                }),
                Ok(StreamEvent::done(StopReason::ToolCall)),
            ],
            vec![
                Ok(StreamEvent::token("done")),
                Ok(StreamEvent::done(StopReason::EndOfTurn)),
            ],
        ]);

        let mut msgs = vec![Message::user("multi-tool")];
        let registry = echo_registry();

        let outcome = run_turn(
            &mut msgs,
            &provider,
            &SamplingParams::default(),
            &registry,
            &fast_config(),
            CancelToken::new(),
            &mut |_| {},
        )
        .unwrap();

        let call_events: Vec<_> = outcome
            .transcript
            .iter()
            .filter_map(|e| match e {
                TurnEvent::ToolCall { id, .. } => Some(id.as_str()),
                _ => None,
            })
            .collect();

        assert_eq!(call_events, vec!["a", "b"], "tool calls must be in order");
    }
}
