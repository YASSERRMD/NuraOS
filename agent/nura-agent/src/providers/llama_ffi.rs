/// LlamaFfiProvider: direct-link backend for llama.cpp via FFI.
///
/// This module defines the interface for embedding llama.cpp as a library
/// rather than spawning llama-server as a subprocess. The FFI backend
/// eliminates the HTTP round-trip overhead (~1 ms per token) and the
/// inter-process startup latency.
///
/// BUILD REQUIREMENTS
/// ------------------
/// Set LLAMA_LIB_DIR to a directory containing libllama.so (or libllama.a)
/// compiled from llama.cpp. The C API headers must be available at compile
/// time via LLAMA_INCLUDE_DIR. A build.rs script (not yet written) would
/// wire these via rustc-link-search / rustc-link-lib directives.
///
/// CURRENT STATUS
/// --------------
/// The provider struct and trait impl are defined here but the underlying
/// calls are unimplemented. Enable the `llama-ffi` feature flag only when
/// you have a compiled libllama and intend to implement the C FFI bindings.
///
/// TRADE-OFFS vs HTTP backend
/// --------------------------
/// See docs/adr/0004-llama-http-not-ffi.md for the full analysis.
/// Summary: FFI is faster but harder to debug; the HTTP backend is the
/// supported path.
use nura_core::error::{NuraError, Result};
use nura_core::provider::message::{Message, StopReason};
use nura_core::provider::traits::{CancelToken, Capabilities, Provider, SamplingParams};
use nura_core::provider::StreamEvent;

/// Path to the GGUF model file, passed to llama_load_model_from_file.
/// Defaults to /data/models/model.gguf if not overridden in config.
pub const DEFAULT_MODEL_PATH: &str = "/data/models/model.gguf";

/// Context window size in tokens for the embedded model instance.
pub const DEFAULT_N_CTX: u32 = 2048;

/// LlamaFfiProvider wraps an embedded llama.cpp context loaded at startup.
///
/// One instance of this provider holds the model in memory. Because llama.cpp
/// is not Sync, this provider must be used from a single thread or guarded by
/// an external mutex.
#[allow(dead_code)]
pub struct LlamaFfiProvider {
    model_path: String,
    n_ctx: u32,
    // Future: llama_model *model and llama_context *ctx pointers
    // stored here as raw pointers behind a Mutex<()> guard.
}

impl LlamaFfiProvider {
    /// Create a new provider. Does NOT load the model; call `init()` first.
    pub fn new(model_path: impl Into<String>, n_ctx: u32) -> Self {
        Self {
            model_path: model_path.into(),
            n_ctx,
        }
    }

    /// Load the model into memory. Must be called before the first `complete()`.
    #[allow(dead_code)]
    ///
    /// This maps to `llama_load_model_from_file` + `llama_new_context_with_model`.
    /// Returns an error if the model file is missing or the context cannot be
    /// allocated (e.g. not enough RAM for the given n_ctx).
    pub fn init(&self) -> Result<()> {
        // TODO: call llama_backend_init(false) and load the model via FFI.
        Err(NuraError::Provider {
            provider: "llama-ffi".into(),
            detail: format!(
                "FFI backend not yet wired (model_path={}, n_ctx={}); \
                 implement llama_load_model_from_file binding",
                self.model_path, self.n_ctx
            ),
        })
    }
}

impl Provider for LlamaFfiProvider {
    fn name(&self) -> &str {
        "llama-ffi"
    }

    fn capabilities(&self) -> Capabilities {
        Capabilities {
            streaming: true,
            tool_calling: false,
            system_messages: true,
            max_context_tokens: self.n_ctx,
        }
    }

    fn complete(
        &self,
        _messages: &[Message],
        _params: &SamplingParams,
        cancel: &CancelToken,
    ) -> Box<dyn Iterator<Item = Result<StreamEvent>> + Send + '_> {
        if cancel.is_cancelled() {
            return Box::new(std::iter::once(Ok(StreamEvent::done(StopReason::Cancel))));
        }

        // TODO: tokenise with llama_tokenize, run llama_decode in a loop,
        // yield StreamEvent::token(detokenised_text) for each new token,
        // emit StreamEvent::done when EOS token is produced or max_tokens hit.
        Box::new(std::iter::once(Err(NuraError::Provider {
            provider: "llama-ffi".into(),
            detail: "LlamaFfiProvider.complete() not yet implemented".into(),
        })))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use nura_core::provider::traits::SamplingParams;

    #[test]
    fn ffi_provider_name() {
        let p = LlamaFfiProvider::new("/data/models/model.gguf", 2048);
        assert_eq!(p.name(), "llama-ffi");
    }

    #[test]
    fn ffi_provider_capabilities() {
        let p = LlamaFfiProvider::new("/data/models/model.gguf", 2048);
        let caps = p.capabilities();
        assert!(caps.streaming, "FFI backend must advertise streaming");
        assert_eq!(caps.max_context_tokens, 2048);
    }

    #[test]
    fn ffi_init_returns_error_until_wired() {
        let p = LlamaFfiProvider::new("/nonexistent.gguf", 512);
        assert!(p.init().is_err(), "init must fail before FFI is wired");
    }

    #[test]
    fn ffi_complete_returns_error_until_wired() {
        use nura_core::provider::message::Message;
        let p = LlamaFfiProvider::new("/nonexistent.gguf", 512);
        let cancel = CancelToken::new();
        let msgs = vec![Message::user("hello")];
        let params = SamplingParams::default();
        let events: Vec<_> = p.complete(&msgs, &params, &cancel).collect();
        assert_eq!(events.len(), 1);
        assert!(events[0].is_err(), "complete must return error until wired");
    }

    #[test]
    fn ffi_complete_respects_cancel() {
        use nura_core::provider::message::Message;
        let p = LlamaFfiProvider::new("/nonexistent.gguf", 512);
        let cancel = CancelToken::new();
        cancel.cancel();
        let msgs = vec![Message::user("hello")];
        let params = SamplingParams::default();
        let events: Vec<_> = p.complete(&msgs, &params, &cancel).collect();
        assert_eq!(events.len(), 1);
        match events[0].as_ref().unwrap() {
            StreamEvent::Done { stop_reason } => assert_eq!(*stop_reason, StopReason::Cancel),
            other => panic!("expected Done(Cancel), got {:?}", other),
        }
    }
}
