use std::process::ExitCode;
use vsf_canon::{Value, parse};
use vsf_ir::doc::decode_document;
use vsf_registry::json::pretty;
use vsf_versioning::diff::diff;
use vsf_versioning::impact::{DepGraph, TestSpec, select};
use vsf_versioning::report::to_value;

const USAGE: &str =
    "usage: vsf-versioning diff FROM.json TO.json [--catalog TESTS.json] [--context OTHER.json]...";

fn read(path: &str) -> Result<Vec<u8>, String> {
    std::fs::read(path).map_err(|e| format!("{path}: {e}"))
}

fn catalog(path: &str) -> Result<Vec<TestSpec>, String> {
    let v = parse(&read(path)?).map_err(|e| e.code().to_string())?;
    let Value::Array(items) = v else {
        return Err("a catalog is a list".into());
    };
    items
        .iter()
        .map(|t| {
            let covers = match t.get("covers") {
                Some(Value::Array(c)) => c
                    .iter()
                    .filter_map(|x| x.as_str().map(String::from))
                    .collect(),
                _ => Vec::new(),
            };
            Ok(TestSpec {
                id: t
                    .get("id")
                    .and_then(Value::as_str)
                    .ok_or("a test needs an id")?
                    .to_string(),
                covers,
                always: t.get("always") == Some(&Value::Bool(true)),
            })
        })
        .collect()
}

fn run(args: &[String]) -> Result<String, String> {
    let [cmd, from, to, rest @ ..] = args else {
        return Err(USAGE.into());
    };
    if cmd != "diff" {
        return Err(USAGE.into());
    }
    let load = |p: &str| decode_document(&read(p)?).map_err(|e| format!("{p}: {}", e.code));
    let (a, b) = (load(from)?, load(to)?);
    let d = diff(&a, &b).map_err(|e| e.to_string())?;
    let mut tests = None;
    let mut context = vec![a.clone(), b.clone()];
    let mut it = rest.iter();
    while let Some(flag) = it.next() {
        let value = it.next().ok_or(USAGE)?;
        match flag.as_str() {
            "--catalog" => tests = Some(catalog(value)?),
            "--context" => context.push(load(value)?),
            _ => return Err(USAGE.into()),
        }
    }
    let selection = tests.map(|t| {
        let refs: Vec<&vsf_ir::Body> = context.iter().collect();
        let operation = match &a {
            vsf_ir::Body::Operation(c) => Some(c.operation_id.as_str().to_string()),
            _ => None,
        };
        select(&d, &DepGraph::from_bodies(&refs), &t, operation.as_deref())
    });
    Ok(pretty(&to_value(&d, selection.as_ref())))
}

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    match run(&args) {
        Ok(out) => {
            println!("{out}");
            ExitCode::SUCCESS
        }
        Err(e) => {
            eprintln!("error: {e}");
            ExitCode::from(2)
        }
    }
}
