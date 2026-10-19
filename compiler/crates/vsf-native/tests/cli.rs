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

fn ir_fixture(name: &str, edit: impl Fn(&str) -> String) -> Vec<u8> {
    let path = format!(
        "{}/../../../schemas/ir/fixtures/valid/{name}.json",
        env!("CARGO_MANIFEST_DIR")
    );
    let text = std::fs::read_to_string(path).unwrap();
    let (family, version) = {
        let rest = text.split("\"schema\": \"vsf.").nth(1).unwrap();
        let (family, rest) = rest.split_once('/').unwrap();
        let ver = rest.split('"').next().unwrap();
        let mut it = ver.split('.').map(|x| x.parse::<u32>().unwrap());
        (
            family.to_string(),
            (it.next().unwrap(), it.next().unwrap(), it.next().unwrap()),
        )
    };
    let text = edit(&text.replace("$DIGEST", vsf_ir::doc::digest_at(&family, version).unwrap()));
    vsf_canon::to_canonical(&vsf_canon::parse(text.as_bytes()).unwrap())
}

#[test]
fn ir_validate_reports_valid_invalid_and_uncanonical_documents() {
    let ok = ir_fixture("operation-update-stage", str::to_string);
    let (code, out, _) = run(&["ir-validate", "-"], &ok);
    assert_eq!(code, 0);
    let text = String::from_utf8(out).unwrap();
    assert!(
        text.contains("\"valid\":true") && text.contains("\"family\":\"operation\""),
        "{text}"
    );
    assert_eq!(
        text.as_bytes(),
        vsf_ir::api::validate_bytes(&ok).unwrap().as_slice()
    );

    let understated = ir_fixture("operation-update-stage", |t| {
        t.replace(
            "\"class\": \"conditional_write\"",
            "\"class\": \"idempotent_write\"",
        )
        .replace(
            "\"idempotency\": \"conditional\"",
            "\"idempotency\": \"idempotent\"",
        )
    });
    let (code, out, _) = run(&["ir-validate", "-"], &understated);
    assert_eq!(code, 3);
    assert!(
        String::from_utf8(out)
            .unwrap()
            .contains("contract.effect_understated")
    );

    let (code, out, _) = run(
        &["ir-validate", "-"],
        br#"{"schema":"vsf.operation/2.0.0","schema_digest":"x","body":{}}"#,
    );
    assert_eq!(code, 3);
    assert!(
        String::from_utf8(out)
            .unwrap()
            .contains("\"code\":\"schema.incompatible_major\"")
    );

    let (code, out, err) = run(&["ir-validate", "-"], b"{\"a\":1.5}");
    assert_eq!((code, out.len()), (2, 0));
    assert_eq!(err.trim(), "error: canon.non_integer");
}

#[test]
fn serve_validates_ir_documents() {
    let ok = ir_fixture("presentation-orders", str::to_string);
    let mut input = Vec::new();
    input.extend_from_slice(&(ok.len() as u32 + 1).to_le_bytes());
    input.push(b'V');
    input.extend_from_slice(&ok);
    let (code, out, _) = run(&["serve"], &input);
    assert_eq!(code, 0);
    let expected = vsf_ir::api::validate_bytes(&ok).unwrap();
    assert_eq!(out[0], 0);
    assert_eq!(&out[5..], expected.as_slice());
}
