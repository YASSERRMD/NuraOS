pub mod local;

#[cfg(feature = "remote-providers")]
pub mod anthropic;
#[cfg(feature = "remote-providers")]
pub mod openai;

#[cfg(feature = "llama-ffi")]
pub mod llama_ffi;

#[cfg(feature = "remote-providers")]
pub use anthropic::AnthropicProvider;
#[cfg(feature = "remote-providers")]
pub use openai::OpenAiCompatProvider;

#[cfg(feature = "llama-ffi")]
pub use llama_ffi::LlamaFfiProvider;
