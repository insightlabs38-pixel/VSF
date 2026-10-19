#![allow(dead_code, clippy::unwrap_used, clippy::expect_used, clippy::panic)]

pub mod sample;

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

use vsf_canon::Value;

pub enum Edit<'a> {
    Set(&'a str, Value),
    Remove(&'a str),
    Push(&'a str, Value),
}

fn step<'a>(v: &'a mut Value, seg: &str) -> &'a mut Value {
    match v {
        Value::Object(e) => {
            &mut e
                .iter_mut()
                .find(|(k, _)| k == seg)
                .unwrap_or_else(|| panic!("no key {seg}"))
                .1
        }
        Value::Array(a) => &mut a[seg.parse::<usize>().unwrap()],
        _ => panic!("cannot descend into {seg}"),
    }
}

fn apply(root: &mut Value, edit: &Edit) {
    let (path, _) = match edit {
        Edit::Set(p, v) | Edit::Push(p, v) => (*p, Some(v)),
        Edit::Remove(p) => (*p, None),
    };
    let segs: Vec<&str> = path.trim_start_matches('/').split('/').collect();
    let (last, parents) = segs.split_last().unwrap();
    let mut cur = &mut *root;
    for s in parents {
        cur = step(cur, s);
    }
    match (edit, cur) {
        (Edit::Set(_, v), Value::Object(e)) => match e.iter_mut().find(|(k, _)| k == last) {
            Some(slot) => slot.1 = v.clone(),
            None => e.push((last.to_string(), v.clone())),
        },
        (Edit::Set(_, v), Value::Array(a)) => a[last.parse::<usize>().unwrap()] = v.clone(),
        (Edit::Remove(_), Value::Object(e)) => e.retain(|(k, _)| k != last),
        (Edit::Remove(_), Value::Array(a)) => {
            a.remove(last.parse::<usize>().unwrap());
        }
        (Edit::Push(_, v), cur) => {
            let Value::Array(a) = step(cur, last) else {
                panic!("not a list")
            };
            a.push(v.clone());
        }
        _ => panic!("bad edit"),
    }
}

/// A valid fixture with edits applied under the document's own version.
pub fn edited(name: &str, version: (u32, u32, u32), edits: &[Edit]) -> Body {
    let path = format!(
        "{}/../../../schemas/ir/fixtures/valid/{name}.json",
        env!("CARGO_MANIFEST_DIR")
    );
    let digest = digest_at(family_of_name(name), version).unwrap();
    let text = std::fs::read_to_string(path)
        .unwrap()
        .replace("$DIGEST", digest);
    let mut v = parse(text.as_bytes()).unwrap();
    for e in edits {
        apply(&mut v, e);
    }
    decode_document(&vsf_canon::to_canonical(&v)).unwrap_or_else(|e| panic!("{name}: {e:?}"))
}

fn family_of(prefix: &str) -> &'static str {
    match prefix {
        "operation" => "operation",
        "semantic" => "semantic",
        "presentation" => "presentation",
        "observation" => "observation",
        "environment" => "environment",
        _ => "version",
    }
}

fn family_of_name(name: &str) -> &'static str {
    family_of(name.split('-').next().unwrap_or(""))
}

pub fn s(x: &str) -> Value {
    Value::Str(x.to_string())
}
