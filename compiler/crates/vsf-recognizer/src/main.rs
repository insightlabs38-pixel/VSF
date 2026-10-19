use std::collections::BTreeSet;
use std::process::ExitCode;
use vsf_recognizer::contract::derive;
use vsf_recognizer::engine::{discovery_queries, recognize};
use vsf_recognizer::index::{FeatureIndex, scan};
use vsf_recognizer::ledger::build_ledger;
use vsf_recognizer::obs::Observation;
use vsf_recognizer::reconcile::{Protocol, reconcile};
use vsf_recognizer::report::recognition_value;
use vsf_recognizer::semantics::Rules;
use vsf_recognizer::val::{count, list, n, obj, pretty, s};

const USAGE: &str = "usage: vsf-recognizer <recognize|ledger|contract|bench-index> <snapshot.json> [--rules 1|2] [--captured-at N] [--protocol FILE]... [--witness ID,ID]";

fn flag<'a>(args: &'a [String], name: &str) -> Option<&'a str> {
    let at = args.iter().position(|a| a == name)?;
    args.get(at + 1).map(String::as_str)
}

fn flags<'a>(args: &'a [String], name: &str) -> Vec<&'a str> {
    args.windows(2)
        .filter(|w| w[0] == name)
        .map(|w| w[1].as_str())
        .collect()
}

fn run(args: &[String]) -> Result<String, String> {
    let (cmd, path) = match args {
        [c, p, ..] => (c.as_str(), p.as_str()),
        _ => return Err(USAGE.into()),
    };
    let bytes = std::fs::read(path).map_err(|e| format!("read {path}: {e}"))?;
    let obs = Observation::from_json(&bytes).map_err(|e| format!("{e:?}"))?;
    let rules = match flag(args, "--rules") {
        None | Some("2") => Rules::V2,
        Some("1") => Rules::V1,
        Some(other) => return Err(format!("unknown rules version {other}")),
    };
    let captured_at: i64 = flag(args, "--captured-at")
        .map_or(Ok(0), str::parse)
        .map_err(|_| "bad --captured-at".to_string())?;
    let rec = recognize(&obs, rules);
    match cmd {
        "recognize" => Ok(pretty(&recognition_value(&rec))),
        "ledger" => {
            let mut ledger = build_ledger(&obs, &rec, captured_at);
            for f in flags(args, "--protocol") {
                let raw = std::fs::read(f).map_err(|e| format!("read {f}: {e}"))?;
                let p = Protocol::from_json(&raw)?;
                reconcile(&mut ledger, &obs, &rec, &p, 30 * 24 * 3600 * 1000);
            }
            Ok(pretty(&ledger.to_value()))
        }
        "contract" => {
            let ledger = build_ledger(&obs, &rec, captured_at);
            let witnessed: BTreeSet<u32> = flag(args, "--witness")
                .unwrap_or("")
                .split(',')
                .filter_map(|x| x.parse().ok())
                .collect();
            Ok(pretty(&derive(&rec, &ledger, &witnessed).to_value()))
        }
        "bench-index" => {
            let index = FeatureIndex::build(&obs);
            let stats = index.stats();
            let mut rows = Vec::new();
            for (kind, q) in discovery_queries() {
                let (hits, st) = index.query(&q);
                let reference = scan(&obs, &q);
                rows.push(obj(vec![
                    ("candidates_examined", count(st.candidates)),
                    ("equal_to_scan", vsf_canon::Value::Bool(reference == hits)),
                    ("kind", s(kind.as_str())),
                    ("results", count(hits.len())),
                    ("scan_examined", count(obs.len())),
                ]));
            }
            Ok(pretty(&obj(vec![
                ("index_bytes", count(stats.bytes())),
                ("nodes", count(obs.len())),
                ("queries", list(rows)),
                ("schema", s("vsf-index-bench-v1")),
                ("version", n(1)),
            ])))
        }
        other => Err(format!("unknown command {other}\n{USAGE}")),
    }
}

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    match run(&args) {
        Ok(out) => {
            print!("{out}");
            ExitCode::SUCCESS
        }
        Err(e) => {
            eprintln!("{e}");
            ExitCode::from(2)
        }
    }
}
