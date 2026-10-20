//! Deterministic assembly of a Semantic IR document and its row data from one recognized observation.

mod build;
mod frame;
mod ids;
mod infer;
mod model;

use std::collections::BTreeMap;
use vsf_canon::{Value, to_canonical};
use vsf_recognizer::contract::derive;
use vsf_recognizer::engine::recognize;
use vsf_recognizer::ledger::build_ledger;
use vsf_recognizer::obs::Observation;
use vsf_recognizer::semantics::Rules;
use vsf_recognizer::witness::AcceptedWitnesses;

pub const SCHEMA: &str = "vsf-assembly-v1";
pub const MAX_ENTITIES: usize = 64;
pub const MAX_FIELDS: usize = 256;
pub const MAX_ROWS: usize = 20_000;
pub const MAX_ACTIONS: usize = 512;
pub const MAX_INPUT_BYTES: usize = 16 * 1024 * 1024;

/// Frame: u32 big-endian header length, header JSON, then the `vsf-observation-snapshot-v1` JSON.
pub fn assemble(input: &[u8]) -> Result<Vec<u8>, &'static str> {
    if input.len() > MAX_INPUT_BYTES {
        return Err("assemble.too_large");
    }
    let (params, snapshot) = frame::parse(input)?;
    let obs = Observation::from_json(snapshot).map_err(|e| e.code)?;
    let rec = recognize(&obs, Rules::V2);
    let ledger = build_ledger(&obs, &rec, params.observed_at);
    let contract = derive(&rec, &ledger, &AcceptedWitnesses::none(), &BTreeMap::new());
    let out: Value = build::run(&params, &obs, &rec, &contract)?;
    Ok(to_canonical(&out))
}

/// Builds a frame for callers that hold the pieces separately (tests and the native command line).
pub fn make_frame(header: &Value, snapshot: &[u8]) -> Vec<u8> {
    let head = to_canonical(header);
    let mut out = Vec::with_capacity(4 + head.len() + snapshot.len());
    out.extend_from_slice(&u32::try_from(head.len()).unwrap_or(u32::MAX).to_be_bytes());
    out.extend_from_slice(&head);
    out.extend_from_slice(snapshot);
    out
}
