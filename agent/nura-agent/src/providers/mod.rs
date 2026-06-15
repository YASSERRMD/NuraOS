pub mod local;

#[cfg(feature = "remote-providers")]
pub mod anthropic;
#[cfg(feature = "remote-providers")]
pub mod openai;

pub use local::LocalProvider;

#[cfg(feature = "remote-providers")]
pub use anthropic::AnthropicProvider;
#[cfg(feature = "remote-providers")]
pub use openai::OpenAiCompatProvider;
