#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

use std::io::Write;
use std::process::{Command, Stdio};

fn run(args: &[&str], stdin: &[u8]) -> (i32, Vec<u8>, String) {
    let mut child = Command::new(env!("CARGO_BIN_EXE_vsf-fabric"))
        .args(args)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    child.stdin.take().unwrap().write_all(stdin).unwrap();
    let out = child.wait_with_output().unwrap();
    (
        out.status.code().unwrap(),
        out.stdout,
        String::from_utf8(out.stderr).unwrap(),
    )
}

#[test]
fn emit_then_compile_roundtrip_matches_library() {
    let (code, fixture, _) = run(&["emit-fixture", "--seed", "42"], b"");
    assert_eq!(code, 0);
    assert_eq!(fixture, vsf_fixtures::generate_envelope_bytes(42));
    let (code, compiled, _) = run(&["compile", "-"], &fixture);
    assert_eq!(code, 0);
    assert_eq!(compiled, vsf_fabric::compile_envelope(&fixture).unwrap());
}

#[test]
fn incompatible_schema_exits_2_with_code() {
    let bad =
        br#"{"schema":"vsf.smoke.envelope/2.0.0","schema_digest":"x","seq":1,"payload":null}"#;
    let (code, out, err) = run(&["verify", "-"], bad);
    assert_eq!((code, out.len()), (2, 0));
    assert_eq!(err.trim(), "error: schema.incompatible_major");
}

#[test]
fn bad_usage_exits_64() {
    assert_eq!(run(&["emit-fixture"], b"").0, 64);
    assert_eq!(run(&["nope"], b"").0, 64);
}

#[test]
fn pack_generate_analyze_matches_library_and_rejects_truncation() {
    let (code, pack, _) = run(&["pack-gen", "--seed", "9", "--nodes", "300"], b"");
    assert_eq!(code, 0);
    assert_eq!(pack, vsf_fixtures::generate_pack(9, 300));
    let (code, summary, _) = run(&["pack-analyze", "-"], &pack);
    assert_eq!(code, 0);
    assert_eq!(summary, vsf_observation::analyze(&pack).unwrap());
    let (code, out, err) = run(&["pack-analyze", "-"], &pack[..pack.len() - 1]);
    assert_eq!((code, out.len()), (2, 0));
    assert_eq!(err.trim(), "error: pack.truncated");
    assert_eq!(
        run(&["pack-analyze", "-"], b"").2.trim(),
        "error: pack.empty"
    );
    assert_eq!(run(&["pack-gen", "--seed", "1"], b"").0, 64);
}

#[test]
fn serve_handles_frames_errors_and_unknown_ops() {
    let pack = vsf_fixtures::generate_pack(2, 50);
    let mut input = Vec::new();
    let mut frame = |op: u8, payload: &[u8]| {
        input.extend_from_slice(&(payload.len() as u32 + 1).to_le_bytes());
        input.push(op);
        input.extend_from_slice(payload);
    };
    frame(b'A', &pack);
    frame(b'A', &pack[..10]);
    frame(b'Z', b"x");
    let (code, out, _) = run(&["serve"], &input);
    assert_eq!(code, 0);
    let expected_ok = vsf_observation::analyze(&pack).unwrap();
    assert_eq!(out[0], 0);
    assert_eq!(
        u32::from_le_bytes(out[1..5].try_into().unwrap()) as usize,
        expected_ok.len()
    );
    assert_eq!(&out[5..5 + expected_ok.len()], expected_ok.as_slice());
    let rest = &out[5 + expected_ok.len()..];
    assert_eq!(rest[0], 2);
    assert!(rest.windows(14).any(|w| w == b"pack.truncated"));
    assert!(rest.windows(16).any(|w| w == b"serve.unknown_op"));
    let (code, _, _) = run(&["serve"], &0u32.to_le_bytes());
    assert_eq!(code, 65);
}
