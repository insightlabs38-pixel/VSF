#![cfg_attr(test, allow(clippy::unwrap_used, clippy::expect_used, clippy::panic))]
//! Version identity, lineage, promotion trust, semantic diff, impact selection and pin resolution.

pub mod diff;
pub mod impact;
pub mod lineage;
pub mod pin;
pub mod promotion;

/// A refusal with a stable code; callers branch on the code, never on the text.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Refusal {
    pub code: &'static str,
    pub detail: String,
}

impl std::fmt::Display for Refusal {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}: {}", self.code, self.detail)
    }
}

pub fn refuse(code: &'static str, detail: impl Into<String>) -> Refusal {
    Refusal {
        code,
        detail: detail.into(),
    }
}

pub type Res<T> = Result<T, Refusal>;
