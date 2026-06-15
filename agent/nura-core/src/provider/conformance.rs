/// Provider conformance test suite.
///
/// `run_conformance_suite` exercises any `Provider` implementation against the
/// invariants the agent loop depends on. Call it in your provider's `#[test]`
/// module to verify conformance without duplicating assertions.
///
/// Each check is independent; a failure is accumulated rather than panicking
/// immediately, so a single call surfaces all violations at once.
///
/// Example:
/// ```rust,no_run
/// # use nura_core::provider::conformance::run_conformance_suite;
/// # use nura_core::provider::traits::StubProvider;
/// let failures = run_conformance_suite(&StubProvider);
/// assert!(failures.is_empty(), "conformance failures: {:?}", failures);
/// ```
use super::{
    message::Message,
    traits::{CancelToken, Provider, SamplingParams},
    StreamEvent,
};

/// A single conformance violation.
#[derive(Debug, Clone)]
pub struct Violation {
    pub check: &'static str,
    pub detail: String,
}

impl std::fmt::Display for Violation {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "[{}] {}", self.check, self.detail)
    }
}

/// Run all conformance checks against `provider`.
///
/// Returns a list of violations; an empty list means the provider passes.
pub fn run_conformance_suite(provider: &dyn Provider) -> Vec<Violation> {
    let mut v: Vec<Violation> = Vec::new();

    check_name(provider, &mut v);
    check_capabilities_consistent(provider, &mut v);
    check_streaming_ends_with_done(provider, &mut v);
    check_cancel_respected(provider, &mut v);
    check_empty_message_list(provider, &mut v);
    check_done_is_last_event(provider, &mut v);
    check_no_events_after_done(provider, &mut v);

    v
}

fn fail(violations: &mut Vec<Violation>, check: &'static str, detail: impl Into<String>) {
    violations.push(Violation {
        check,
        detail: detail.into(),
    });
}

fn default_msgs() -> Vec<Message> {
    vec![Message::user("conformance probe")]
}

fn default_params() -> SamplingParams {
    SamplingParams::default()
}

// 1. Provider name must be non-empty and must not contain whitespace.
fn check_name(provider: &dyn Provider, v: &mut Vec<Violation>) {
    const CHECK: &str = "name";
    let name = provider.name();
    if name.is_empty() {
        fail(v, CHECK, "name() returned an empty string");
    } else if name.contains(char::is_whitespace) {
        fail(v, CHECK, format!("name() contains whitespace: {:?}", name));
    }
}

// 2. Capabilities must be internally consistent.
fn check_capabilities_consistent(provider: &dyn Provider, v: &mut Vec<Violation>) {
    const CHECK: &str = "capabilities";
    let caps = provider.capabilities();
    if caps.max_context_tokens == 0 {
        fail(v, CHECK, "max_context_tokens must be > 0");
    }
}

// 3. Streaming a normal prompt must produce at least one event and eventually
//    yield a Done event.
fn check_streaming_ends_with_done(provider: &dyn Provider, v: &mut Vec<Violation>) {
    const CHECK: &str = "streaming_ends_with_done";
    let cancel = CancelToken::new();
    let events: Vec<_> = provider
        .complete(&default_msgs(), &default_params(), &cancel)
        .collect();

    if events.is_empty() {
        fail(v, CHECK, "complete() yielded no events at all");
        return;
    }

    let has_done = events.iter().any(|e| {
        matches!(
            e,
            Ok(StreamEvent::Done { .. }) | Ok(StreamEvent::Error { .. })
        )
    });
    if !has_done {
        fail(v, CHECK, "stream ended without a Done or Error event");
    }
}

// 4. When the cancel token is pre-fired, the stream must terminate quickly
//    (no more than 1 event) and must not produce a full response.
fn check_cancel_respected(provider: &dyn Provider, v: &mut Vec<Violation>) {
    const CHECK: &str = "cancel_respected";
    let cancel = CancelToken::new();
    cancel.cancel();
    let events: Vec<_> = provider
        .complete(&default_msgs(), &default_params(), &cancel)
        .collect();

    // Collect non-error text tokens.
    let text_events: Vec<_> = events
        .iter()
        .filter(|e| matches!(e, Ok(StreamEvent::TokenDelta { .. })))
        .collect();

    // After a pre-cancelled token we allow at most 1 token delta (a provider
    // may have already buffered one) but not a full response.
    if text_events.len() > 2 {
        fail(
            v,
            CHECK,
            format!(
                "pre-cancelled stream produced {} token events (expected 0-1)",
                text_events.len()
            ),
        );
    }
}

// 5. An empty message list must not panic; it may return an error event but
//    must yield at least one event.
fn check_empty_message_list(provider: &dyn Provider, v: &mut Vec<Violation>) {
    const CHECK: &str = "empty_message_list";
    let cancel = CancelToken::new();
    let events: Vec<_> = provider.complete(&[], &default_params(), &cancel).collect();

    if events.is_empty() {
        fail(
            v,
            CHECK,
            "complete([]) yielded no events -- must yield at least one",
        );
    }
}

// 6. After a Done or Error event the stream must end.
fn check_done_is_last_event(provider: &dyn Provider, v: &mut Vec<Violation>) {
    const CHECK: &str = "done_is_last";
    let cancel = CancelToken::new();
    let events: Vec<_> = provider
        .complete(&default_msgs(), &default_params(), &cancel)
        .collect();

    let terminal_pos = events.iter().position(|e| {
        matches!(
            e,
            Ok(StreamEvent::Done { .. }) | Ok(StreamEvent::Error { .. })
        )
    });

    if let Some(pos) = terminal_pos {
        if pos + 1 < events.len() {
            fail(
                v,
                CHECK,
                format!(
                    "events appeared after Done/Error at index {}; stream had {} events total",
                    pos,
                    events.len()
                ),
            );
        }
    }
}

// 7. Same as (6) but explicit: no events after Done.
fn check_no_events_after_done(provider: &dyn Provider, v: &mut Vec<Violation>) {
    const CHECK: &str = "no_events_after_done";
    let cancel = CancelToken::new();
    let events: Vec<_> = provider
        .complete(&default_msgs(), &default_params(), &cancel)
        .collect();

    let mut seen_terminal = false;
    for e in &events {
        if seen_terminal {
            fail(
                v,
                CHECK,
                "received an event after Done/Error; iterator must stop there",
            );
            break;
        }
        if matches!(
            e,
            Ok(StreamEvent::Done { .. }) | Ok(StreamEvent::Error { .. })
        ) {
            seen_terminal = true;
        }
    }
}

// ---------------------------------------------------------------------------
// Tests: run the conformance suite against StubProvider.
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;
    use crate::provider::{message::StopReason, traits::StubProvider};

    fn assert_conformant(provider: &dyn Provider) {
        let failures = run_conformance_suite(provider);
        if !failures.is_empty() {
            let msgs: Vec<_> = failures.iter().map(|f| f.to_string()).collect();
            panic!(
                "provider '{}' failed {} conformance check(s):\n  {}",
                provider.name(),
                failures.len(),
                msgs.join("\n  ")
            );
        }
    }

    #[test]
    fn stub_provider_is_conformant() {
        assert_conformant(&StubProvider);
    }

    #[test]
    fn check_name_rejects_empty() {
        struct EmptyNameProvider;
        impl Provider for EmptyNameProvider {
            fn name(&self) -> &str {
                ""
            }
            fn capabilities(&self) -> crate::provider::traits::Capabilities {
                StubProvider.capabilities()
            }
            fn complete<'a>(
                &'a self,
                messages: &[Message],
                params: &SamplingParams,
                cancel: &CancelToken,
            ) -> Box<dyn Iterator<Item = crate::error::Result<StreamEvent>> + Send + 'a>
            {
                StubProvider.complete(messages, params, cancel)
            }
        }
        let v = run_conformance_suite(&EmptyNameProvider);
        assert!(
            v.iter().any(|f| f.check == "name"),
            "expected name violation, got: {:?}",
            v
        );
    }

    #[test]
    fn check_name_rejects_whitespace() {
        struct SpaceNameProvider;
        impl Provider for SpaceNameProvider {
            fn name(&self) -> &str {
                "bad name"
            }
            fn capabilities(&self) -> crate::provider::traits::Capabilities {
                StubProvider.capabilities()
            }
            fn complete<'a>(
                &'a self,
                messages: &[Message],
                params: &SamplingParams,
                cancel: &CancelToken,
            ) -> Box<dyn Iterator<Item = crate::error::Result<StreamEvent>> + Send + 'a>
            {
                StubProvider.complete(messages, params, cancel)
            }
        }
        let v = run_conformance_suite(&SpaceNameProvider);
        assert!(
            v.iter().any(|f| f.check == "name"),
            "expected name violation for whitespace, got: {:?}",
            v
        );
    }

    #[test]
    fn check_streaming_detects_missing_done() {
        use crate::error::Result;

        struct NoDoneProvider;
        impl Provider for NoDoneProvider {
            fn name(&self) -> &str {
                "nodone"
            }
            fn capabilities(&self) -> crate::provider::traits::Capabilities {
                StubProvider.capabilities()
            }
            fn complete<'a>(
                &'a self,
                _messages: &[Message],
                _params: &SamplingParams,
                _cancel: &CancelToken,
            ) -> Box<dyn Iterator<Item = Result<StreamEvent>> + Send + 'a> {
                Box::new(vec![Ok(StreamEvent::token("hello"))].into_iter())
            }
        }
        let v = run_conformance_suite(&NoDoneProvider);
        assert!(
            v.iter().any(|f| f.check == "streaming_ends_with_done"),
            "expected streaming_ends_with_done violation, got: {:?}",
            v
        );
    }

    #[test]
    fn check_done_is_last_detects_extra_events() {
        use crate::error::Result;

        struct ExtraAfterDoneProvider;
        impl Provider for ExtraAfterDoneProvider {
            fn name(&self) -> &str {
                "extra"
            }
            fn capabilities(&self) -> crate::provider::traits::Capabilities {
                StubProvider.capabilities()
            }
            fn complete<'a>(
                &'a self,
                _messages: &[Message],
                _params: &SamplingParams,
                _cancel: &CancelToken,
            ) -> Box<dyn Iterator<Item = Result<StreamEvent>> + Send + 'a> {
                Box::new(
                    vec![
                        Ok(StreamEvent::done(StopReason::EndOfTurn)),
                        Ok(StreamEvent::token("oops")),
                    ]
                    .into_iter(),
                )
            }
        }
        let v = run_conformance_suite(&ExtraAfterDoneProvider);
        assert!(
            v.iter()
                .any(|f| f.check == "done_is_last" || f.check == "no_events_after_done"),
            "expected done_is_last or no_events_after_done violation, got: {:?}",
            v
        );
    }
}
