#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

use std::path::{Path, PathBuf};
use vsf_canon::{Value, parse, to_canonical};
use vsf_registry::harness::load_package;
use vsf_registry::json::pretty;
use vsf_registry::manifest::{
    MANIFEST, Signer, TRUST, Trust, TrustedKey, build, file_set, primitive_digests, primitive_root,
    sign, verify,
};

fn repo() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../../..")
        .canonicalize()
        .unwrap()
}

fn trust() -> Trust {
    Trust::decode(&parse(&std::fs::read(repo().join(TRUST)).unwrap()).unwrap()).unwrap()
}

fn committed() -> Vec<u8> {
    std::fs::read(repo().join(MANIFEST)).unwrap()
}

/// A copy of every file the package covers so a test can tamper without touching the tree.
fn scratch(name: &str) -> PathBuf {
    let dir = std::env::temp_dir().join(format!("vsf-manifest-{name}-{}", std::process::id()));
    let _ = std::fs::remove_dir_all(&dir);
    let pkg = load_package(&repo()).unwrap();
    for rel in file_set(&pkg).unwrap() {
        let to = dir.join(&rel);
        std::fs::create_dir_all(to.parent().unwrap()).unwrap();
        std::fs::copy(repo().join(&rel), &to).unwrap();
    }
    dir
}

fn code(root: &Path, bytes: &[u8], t: &Trust) -> &'static str {
    verify(root, bytes, t).unwrap_err().code
}

fn edit(v: &Value, f: impl FnOnce(&mut Vec<(String, Value)>)) -> Value {
    let Value::Object(entries) = v else { panic!() };
    let mut entries = entries.clone();
    f(&mut entries);
    Value::Object(entries)
}

fn field<'a>(entries: &'a mut [(String, Value)], key: &str) -> &'a mut Value {
    &mut entries.iter_mut().find(|(k, _)| k == key).unwrap().1
}

fn text(v: &Value) -> Vec<u8> {
    pretty(v).into_bytes()
}

#[test]
fn the_committed_manifest_verifies_and_rebuilds_byte_for_byte() {
    let v = verify(&repo(), &committed(), &trust()).unwrap();
    assert_eq!(
        (v.primitives, v.key_id.as_str(), v.purpose.as_str()),
        (924, "vsf-dev-1", "development")
    );
    let pkg = load_package(&repo()).unwrap();
    let signer = Signer::development().unwrap();
    let again = build(&pkg, &signer).unwrap();
    assert_eq!(pretty(&again).into_bytes(), committed());
    assert_eq!(pretty(&build(&pkg, &signer).unwrap()), pretty(&again));
}

#[test]
fn a_flipped_byte_in_any_covered_file_is_a_hard_failure() {
    let pkg = load_package(&repo()).unwrap();
    for rel in file_set(&pkg).unwrap() {
        let dir = scratch("flip");
        let path = dir.join(&rel);
        let mut bytes = std::fs::read(&path).unwrap();
        let last = bytes.len() - 1;
        bytes[last] ^= 1;
        std::fs::write(&path, bytes).unwrap();
        assert_eq!(
            code(&dir, &committed(), &trust()),
            "manifest.file_digest_mismatch",
            "{rel}"
        );
        std::fs::remove_dir_all(dir).unwrap();
    }
}

#[test]
fn missing_and_unlisted_files_fail() {
    let dir = scratch("files");
    std::fs::remove_file(
        dir.join("fixtures/primitives/collection/html-table/table.html.nested.json"),
    )
    .unwrap();
    assert_eq!(code(&dir, &committed(), &trust()), "manifest.file_missing");
    let dir = scratch("unlisted");
    let src = repo().join("fixtures/primitives/collection/html-table/table.html.nested.json");
    let copy = std::fs::read_to_string(src)
        .unwrap()
        .replace("\"table.html.nested\"", "\"table.html.copy\"");
    std::fs::write(
        dir.join("fixtures/primitives/collection/html-table/table.html.copy.json"),
        copy,
    )
    .unwrap();
    assert_eq!(code(&dir, &committed(), &trust()), "manifest.unlisted_file");
}

#[test]
fn signature_tampering_and_key_misuse_fail_before_content_is_trusted() {
    let m = parse(&committed()).unwrap();
    let dir = repo();
    let t = trust();
    let sig = |m: &Value| match m.get("signature").unwrap() {
        Value::Object(e) => e.clone(),
        _ => panic!(),
    };
    let flipped = edit(&m, |e| {
        let mut s = sig(&Value::Object(e.clone()));
        let v = field(&mut s, "value");
        let Value::Str(x) = v else { panic!() };
        let c = if x.starts_with('0') { '1' } else { '0' };
        x.replace_range(0..1, &c.to_string());
        *field(e, "signature") = Value::Object(s);
    });
    assert_eq!(code(&dir, &text(&flipped), &t), "manifest.bad_signature");
    let unsigned = edit(&m, |e| e.retain(|(k, _)| k != "signature"));
    assert_eq!(code(&dir, &text(&unsigned), &t), "manifest.unsigned");
    let body_change = edit(&m, |e| *field(e, "version") = Value::Str("9.9.9".into()));
    assert_eq!(
        code(&dir, &text(&body_change), &t),
        "manifest.bad_signature"
    );
    let other = Signer::from_seed_hex("vsf-dev-1", &"07".repeat(32)).unwrap();
    let forged = sign(m.clone(), &other).unwrap();
    assert_eq!(code(&dir, &text(&forged), &t), "manifest.bad_signature");
    let unknown = Signer::from_seed_hex("attacker", &"07".repeat(32)).unwrap();
    assert_eq!(
        code(&dir, &text(&sign(m.clone(), &unknown).unwrap()), &t),
        "manifest.untrusted_key"
    );
    let mut revoked = t.clone();
    revoked.keys[0].active = false;
    assert_eq!(code(&dir, &committed(), &revoked), "manifest.untrusted_key");
    let mut wrong_pk = t.clone();
    wrong_pk.keys[0] = TrustedKey {
        public_key: other.public_hex(),
        ..wrong_pk.keys[0].clone()
    };
    assert_eq!(
        code(&dir, &committed(), &wrong_pk),
        "manifest.bad_signature"
    );
}

#[test]
fn a_validly_signed_but_lying_manifest_is_caught_by_recomputation() {
    let m = parse(&committed()).unwrap();
    let dev = Signer::development().unwrap();
    let dir = repo();
    let t = trust();
    let bad_digest = edit(&m, |e| {
        let Value::Array(rows) = field(e, "primitives") else {
            panic!()
        };
        let Value::Object(first) = &mut rows[0] else {
            panic!()
        };
        *field(first, "digest") = Value::Str("0".repeat(64));
    });
    assert_eq!(
        code(&dir, &text(&sign(bad_digest, &dev).unwrap()), &t),
        "manifest.primitive_digest_mismatch"
    );
    let dropped = edit(&m, |e| {
        let Value::Array(rows) = field(e, "primitives") else {
            panic!()
        };
        rows.pop();
    });
    assert_eq!(
        code(&dir, &text(&sign(dropped, &dev).unwrap()), &t),
        "manifest.primitive_set_mismatch"
    );
    let bad_root = edit(&m, |e| {
        *field(e, "primitive_root") = Value::Str("0".repeat(64))
    });
    assert_eq!(
        code(&dir, &text(&sign(bad_root, &dev).unwrap()), &t),
        "manifest.root_mismatch"
    );
    let old_ir = edit(&m, |e| *field(e, "ir_version") = Value::Str("0.9.0".into()));
    assert_eq!(
        code(&dir, &text(&sign(old_ir, &dev).unwrap()), &t),
        "manifest.ir_mismatch"
    );
    let fewer_files = edit(&m, |e| {
        let Value::Array(rows) = field(e, "files") else {
            panic!()
        };
        rows.retain(|r| {
            r.get("path").and_then(Value::as_str) != Some("registry/primitives/policy.v1.json")
        });
    });
    assert_eq!(
        code(&dir, &text(&sign(fewer_files, &dev).unwrap()), &t),
        "manifest.unlisted_file"
    );
    let junk = edit(&m, |e| e.push(("extra".into(), Value::Null)));
    assert!(verify(&dir, &text(&sign(junk, &dev).unwrap()), &t).is_err());
}

#[test]
fn a_substituted_trust_anchor_is_not_accepted_from_the_package_itself() {
    let pkg = load_package(&repo()).unwrap();
    let attacker = Signer::from_seed_hex("attacker", &"09".repeat(32)).unwrap();
    let forged = build(&pkg, &attacker).unwrap();
    assert_eq!(
        code(&repo(), &text(&forged), &trust()),
        "manifest.untrusted_key"
    );
}

#[test]
fn changing_a_dependency_changes_every_dependent_digest_and_the_root() {
    let before = primitive_digests(&load_package(&repo()).unwrap()).unwrap();
    let dir = scratch("deps");
    let path = dir.join("registry/primitives/registry.v1.json");
    let mut doc = parse(&std::fs::read(&path).unwrap()).unwrap();
    let Value::Object(top) = &mut doc else {
        panic!()
    };
    let Value::Array(entries) = field(top, "entries") else {
        panic!()
    };
    let leaf = entries
        .iter()
        .find_map(|e| match e.get("requires") {
            Some(Value::Array(r)) if !r.is_empty() => r[0].as_str().map(String::from),
            _ => None,
        })
        .unwrap();
    for e in entries.iter_mut() {
        if e.get("id").and_then(Value::as_str) == Some(leaf.as_str()) {
            let Value::Object(f) = e else { panic!() };
            *field(f, "rule") = Value::Str("changed rule".into());
        }
    }
    std::fs::write(&path, to_canonical(&doc)).unwrap();
    let after = primitive_digests(&load_package(&dir).unwrap()).unwrap();
    assert_ne!(before[&leaf], after[&leaf]);
    let dependents: Vec<&String> = before.keys().filter(|k| before[*k] != after[*k]).collect();
    assert!(dependents.len() >= 2, "{dependents:?}");
    assert_ne!(primitive_root(&before), primitive_root(&after));
}

#[test]
fn the_development_key_is_labelled_and_deterministic() {
    let a = Signer::development().unwrap();
    let b = Signer::development().unwrap();
    assert_eq!(a.public_hex(), b.public_hex());
    assert_eq!(trust().keys[0].purpose, "development");
    assert!(Signer::from_seed_hex("k", "zz").is_err());
    assert!(Signer::from_seed_hex("k", "00").is_err());
}
