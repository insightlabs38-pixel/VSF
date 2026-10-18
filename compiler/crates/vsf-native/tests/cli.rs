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
