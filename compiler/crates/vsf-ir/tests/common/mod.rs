#![allow(dead_code, clippy::unwrap_used, clippy::expect_used, clippy::panic)]

use std::path::PathBuf;
use vsf_canon::{Value, parse, to_canonical};
use vsf_ir::doc::family_digest;

pub fn fixtures() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../schemas/ir/fixtures")
}

/// Loads a valid fixture, substituting the family digest placeholder, and returns canonical bytes.
pub fn valid_bytes(name: &str) -> Vec<u8> {
    let text =
        std::fs::read_to_string(fixtures().join("valid").join(format!("{name}.json"))).unwrap();
    let family = text
        .split("\"schema\": \"vsf.")
        .nth(1)
        .unwrap()
        .split('/')
        .next()
        .unwrap()
        .to_string();
    let text = text.replace("$DIGEST", family_digest(&family).unwrap());
    to_canonical(&parse(text.as_bytes()).unwrap())
}

pub fn valid_names() -> Vec<String> {
    let mut names: Vec<String> = std::fs::read_dir(fixtures().join("valid"))
        .unwrap()
        .map(|e| {
            e.unwrap()
                .file_name()
                .to_string_lossy()
                .trim_end_matches(".json")
                .to_string()
        })
        .collect();
    names.sort();
    names
}

pub fn pointer_set(root: &mut Value, path: &str, value: Option<Value>) {
    let segs: Vec<&str> = path.split('/').skip(1).collect();
    let (last, head) = segs.split_last().unwrap();
    let mut cur = root;
    for seg in head {
        cur = match cur {
            Value::Object(entries) => &mut entries.iter_mut().find(|(k, _)| k == seg).unwrap().1,
            Value::Array(items) => &mut items[seg.parse::<usize>().unwrap()],
            _ => panic!("bad path {path}"),
        };
    }
    match cur {
        Value::Object(entries) => {
            entries.retain(|(k, _)| k != last);
            if let Some(v) = value {
                entries.push(((*last).to_string(), v));
            }
        }
        Value::Array(items) => {
            let i = last.parse::<usize>().unwrap();
            match value {
                Some(v) => items[i] = v,
                None => {
                    items.remove(i);
                }
            }
        }
        _ => panic!("bad path {path}"),
    }
}
