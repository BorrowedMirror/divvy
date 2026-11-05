use std::time::Duration;

use serde::Serialize;

#[derive(Debug, thiserror::Error)]
pub enum TradeError {
    #[error("rate limited; retry after {retry_after:?}")]
    RateLimited { retry_after: Duration },
    #[error("http {status}: {message}")]
    Http { status: u16, message: String },
    #[error("transport error: {0}")]
    Transport(#[from] reqwest::Error),
    #[error("invalid response: {0}")]
    Decode(String),
    #[error("item not supported by price source `{0}`")]
    Unsupported(&'static str),
    #[error("invalid configuration: {0}")]
    Config(String),
}

/// IPC-friendly flattened error.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, specta::Type)]
pub struct TradeErrorInfo {
    pub kind: String,
    pub message: String,
    pub retry_after_ms: Option<u64>,
}

impl From<&TradeError> for TradeErrorInfo {
    fn from(e: &TradeError) -> Self {
        let (kind, retry) = match e {
            TradeError::RateLimited { retry_after } => ("rate_limited", Some(retry_after.as_millis() as u64)),
            TradeError::Http { .. } => ("http", None),
            TradeError::Transport(_) => ("transport", None),
            TradeError::Decode(_) => ("decode", None),
            TradeError::Unsupported(_) => ("unsupported", None),
            TradeError::Config(_) => ("config", None),
        };
        Self { kind: kind.into(), message: e.to_string(), retry_after_ms: retry }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn flattens_every_variant() {
        let cases = [
            (TradeError::RateLimited { retry_after: Duration::from_millis(1500) }, "rate_limited", Some(1500)),
            (TradeError::Http { status: 502, message: "bad".into() }, "http", None),
            (TradeError::Decode("eof".into()), "decode", None),
            (TradeError::Unsupported("poe.ninja"), "unsupported", None),
            (TradeError::Config("nope".into()), "config", None),
        ];
        for (err, kind, retry) in cases {
            let info = TradeErrorInfo::from(&err);
            assert_eq!(info.kind, kind);
            assert_eq!(info.retry_after_ms, retry);
            assert_eq!(info.message, err.to_string());
        }
        assert_eq!(TradeError::Http { status: 502, message: "bad".into() }.to_string(), "http 502: bad");
    }

    #[test]
    fn default_user_agent_has_version_and_contact() {
        let ua = crate::default_user_agent();
        assert!(ua.starts_with(&format!("rkl/{}", env!("CARGO_PKG_VERSION"))));
        assert!(ua.contains("github.com/nightah/rust-king-ledger"));
    }
}
