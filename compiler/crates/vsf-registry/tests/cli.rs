#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

use std::path::PathBuf;
use std::process::{Command, Output};

fn repo() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../../..")
        .canonicalize()
        .unwrap()
}

fn verify(extra: &[&str]) -> Output {
    Command::new(env!("CARGO_BIN_EXE_vsf-registry"))
        .args(["manifest", "verify"])
        .arg(repo())
        .args(extra)
        .output()
        .unwrap()
}

fn stderr(o: &Output) -> String {
    String::from_utf8_lossy(&o.stderr).into_owned()
}

#[test]
fn a_trust_set_inside_the_package_is_refused_for_production() {
    let inside = repo().join("registry/primitives/trust.v1.json");
    let o = verify(&["--trust", inside.to_str().unwrap()]);
    assert!(!o.status.success());
    assert!(stderr(&o).contains("outside the package"), "{}", stderr(&o));
    let none = verify(&[]);
    assert!(!none.status.success());
}

#[test]
fn the_watermark_advances_and_then_refuses_older_or_different_manifests() {
    let dir = std::env::temp_dir().join(format!("vsf-wm-{}", std::process::id()));
    std::fs::create_dir_all(&dir).unwrap();
    let mark = dir.join("mark.json");
    let m = mark.to_str().unwrap();
    let first = verify(&["--development", "--watermark", m]);
    assert!(first.status.success(), "{}", stderr(&first));
    assert!(String::from_utf8_lossy(&first.stdout).contains("seq=1"));
    let again = verify(&["--development", "--watermark", m]);
    assert!(
        again.status.success(),
        "the accepted manifest verifies again"
    );
    std::fs::write(
        &mark,
        r#"{"digest":"3f","schema":"vsf-package-watermark-v1","sequence":1}"#,
    )
    .unwrap();
    let conflict = verify(&["--development", "--watermark", m]);
    assert!(
        stderr(&conflict).contains("sequence_conflict"),
        "{}",
        stderr(&conflict)
    );
    std::fs::write(
        &mark,
        r#"{"digest":"3f","schema":"vsf-package-watermark-v1","sequence":2}"#,
    )
    .unwrap();
    let rollback = verify(&["--development", "--watermark", m]);
    assert!(
        stderr(&rollback).contains("rollback"),
        "{}",
        stderr(&rollback)
    );
    let expired = verify(&["--development", "--now", "4102444800000"]);
    assert!(stderr(&expired).contains("expired"), "{}", stderr(&expired));
}
