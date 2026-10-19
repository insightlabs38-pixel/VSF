#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;

use std::collections::BTreeSet;
use std::path::PathBuf;
use vsf_ir::semantics::validate_version;
use vsf_registry::manifest::{Freshness, Trust};
use vsf_versioning::lineage::{Graph, manifest_id};
use vsf_versioning::promotion::verify;

fn dir() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../fixtures/versions")
}

fn files(s: &common::sample::Sample) -> Vec<(String, Vec<u8>)> {
    let mut out: Vec<(String, Vec<u8>)> = Vec::new();
    for (digest, bytes) in &s.blobs {
        out.push((format!("blobs/{}", digest.replace(':', "-")), bytes.clone()));
    }
    for (name, _, bytes) in &s.manifests {
        out.push((format!("manifests/{name}.json"), bytes.clone()));
    }
    let index: Vec<String> = s
        .manifests
        .iter()
        .map(|(n, id, _)| format!("  \"{n}\": \"{id}\""))
        .collect();
    out.push((
        "index.json".into(),
        format!("{{\n{}\n}}\n", index.join(",\n")).into_bytes(),
    ));
    out.push(("trust.json".into(), s.trust.clone()));
    out.push(("support.json".into(), s.support.clone()));
    out
}

#[test]
fn the_committed_version_fixtures_are_what_the_generator_writes() {
    let sample = common::sample::build();
    let want = files(&sample);
    if std::env::var_os("VSF_UPDATE_VERSION_FIXTURES").is_some() {
        let _ = std::fs::remove_dir_all(dir());
        for (rel, bytes) in &want {
            let path = dir().join(rel);
            std::fs::create_dir_all(path.parent().unwrap()).unwrap();
            std::fs::write(path, bytes).unwrap();
        }
    }
    for (rel, bytes) in &want {
        let got = std::fs::read(dir().join(rel))
            .unwrap_or_else(|_| panic!("missing {rel}; run with VSF_UPDATE_VERSION_FIXTURES=1"));
        assert_eq!(
            &got, bytes,
            "{rel} drifted; run with VSF_UPDATE_VERSION_FIXTURES=1"
        );
    }
}

#[test]
fn the_sample_history_is_valid_signed_and_forms_one_merged_lineage() {
    let sample = common::sample::build();
    let trust = Trust::decode(&vsf_canon::parse(&sample.trust).unwrap()).unwrap();
    let docs: Vec<(String, Vec<u8>)> = sample
        .manifests
        .iter()
        .map(|(_, id, b)| (id.clone(), b.clone()))
        .collect();
    for (name, id, bytes) in &sample.manifests {
        let (got, m) = manifest_id(bytes).unwrap();
        assert_eq!(&got, id, "{name}");
        assert!(
            validate_version(&m).is_empty(),
            "{name}: {:?}",
            validate_version(&m)
                .iter()
                .map(|v| v.code)
                .collect::<Vec<_>>()
        );
        if m.promotion.is_some() {
            let v = verify(
                &m,
                &trust,
                &Freshness {
                    now_ms: 1_792_368_500_000,
                    last: None,
                },
            )
            .unwrap_or_else(|e| panic!("{name}: {e}"));
            assert_eq!(v.key_id, "vsf-dev-1");
        }
    }
    let g = Graph::rebuild(&docs, &BTreeSet::new()).unwrap();
    assert_eq!(g.len(), 5);
    assert_eq!(g.heads(), vec![sample.manifests[4].1.clone()]);
    assert!(g.is_merge(&sample.manifests[3].1));
}
