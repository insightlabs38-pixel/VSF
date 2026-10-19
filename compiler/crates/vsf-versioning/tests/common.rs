#![allow(dead_code, clippy::unwrap_used, clippy::expect_used, clippy::panic)]

use vsf_canon::parse;
use vsf_ir::doc::{decode_document, digest_at, encode_document};
use vsf_ir::{Body, Channel, Digest, VersionManifest};
use vsf_versioning::lineage::manifest_id;

pub fn read_fixture(name: &str, version: (u32, u32, u32)) -> Body {
    let path = format!(
        "{}/../../../schemas/ir/fixtures/valid/{name}.json",
        env!("CARGO_MANIFEST_DIR")
    );
    let digest = digest_at("version", version).unwrap();
    let text = std::fs::read_to_string(path)
        .unwrap()
        .replace("$DIGEST", digest);
    decode_document(&vsf_canon::to_canonical(&parse(text.as_bytes()).unwrap())).unwrap()
}

pub fn base_manifest() -> VersionManifest {
    let Body::Version(m) = read_fixture("version-manifest", (1, 0, 0)) else {
        panic!("not a version manifest")
    };
    m
}

/// A preview manifest whose lineage is `parents`; `tag` keeps otherwise equal manifests distinct.
pub fn manifest(parents: &[&str], tag: i64) -> VersionManifest {
    let mut m = base_manifest();
    m.channel = Channel::Preview;
    m.promotion = None;
    m.created_at = vsf_ir::Instant(1_792_368_400_000 + tag);
    m.lineage = parents.iter().map(|p| Digest::new(p).unwrap()).collect();
    m
}

pub fn bytes(m: &VersionManifest) -> Vec<u8> {
    encode_document(&Body::Version(m.clone()))
}

pub fn made(parents: &[&str], tag: i64) -> (String, Vec<u8>) {
    let b = bytes(&manifest(parents, tag));
    (manifest_id(&b).unwrap().0, b)
}
