use std::collections::BTreeMap;
use std::process::ExitCode;
use vsf_recognizer::contract::derive;
use vsf_recognizer::engine::{discovery_queries, recognize};
use vsf_recognizer::index::{FeatureIndex, scan};
use vsf_recognizer::ledger::{Export, build_ledger, build_ledger_in};
use vsf_recognizer::obs::Observation;
use vsf_recognizer::protocol::{ProtocolPolicy, Standing, assess};
use vsf_recognizer::reconcile::{Protocol, reconcile, reconcile_contradictions};
use vsf_recognizer::report::recognition_value;
use vsf_recognizer::semantics::Rules;
use vsf_recognizer::val::{count, list, n, obj, pretty, s};
use vsf_recognizer::witness::{AcceptedWitnesses, WitnessPolicy};

const USAGE: &str = "usage: vsf-recognizer <recognize|ledger|contract|bench-index> <snapshot.json> [--rules 1|2|3] [--captured-at N] [--protocol FILE]... [--witness FILE]... [--witness-policy FILE] [--policy FILE] [--shared]";

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
        None | Some("3") => Rules::V3,
        Some("2") => Rules::V2,
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
            let policy = match flag(args, "--policy") {
                None => None,
                Some(f) => {
                    let raw = std::fs::read(f).map_err(|e| format!("read {f}: {e}"))?;
                    let v =
                        vsf_canon::parse(&raw).map_err(|e| format!("policy.parse:{}", e.code()))?;
                    Some(ProtocolPolicy::from_value(&v)?)
                }
            };
            let ctx = policy.as_ref().map(|p| &p.trust);
            let mut ledger = build_ledger_in(&obs, &rec, captured_at, ctx);
            let mut docs = Vec::new();
            for f in flags(args, "--protocol") {
                let raw = std::fs::read(f).map_err(|e| format!("read {f}: {e}"))?;
                let p = Protocol::from_json(&raw)?;
                let standing = policy
                    .as_ref()
                    .map_or(Standing::Advisory("no_policy"), |pol| assess(&p, pol));
                let max_age = policy
                    .as_ref()
                    .map_or(30 * 24 * 3600 * 1000, |pol| pol.max_age_ms);
                reconcile(&mut ledger, &obs, &rec, &p, standing, ctx, max_age);
                docs.push((p, standing));
            }
            let refs: Vec<_> = docs.iter().map(|(p, s)| (p, *s)).collect();
            reconcile_contradictions(&mut ledger, &obs, &refs);
            let export = if args.iter().any(|a| a == "--shared") {
                Export::Shared
            } else {
                Export::Local
            };
            Ok(pretty(&ledger.export(export)))
        }
        "contract" => {
            if flag(args, "--witness").is_some_and(|w| w.parse::<u32>().is_ok() || w.contains(','))
            {
                return Err("bare node ids are not witnesses; pass witness documents".into());
            }
            let ledger = build_ledger(&obs, &rec, captured_at);
            let docs = flags(args, "--witness")
                .into_iter()
                .map(|f| {
                    let raw = std::fs::read(f).map_err(|e| format!("read {f}: {e}"))?;
                    vsf_canon::parse(&raw).map_err(|e| format!("witness.parse:{}", e.code()))
                })
                .collect::<Result<Vec<_>, String>>()?;
            let accepted = match flag(args, "--witness-policy") {
                None if docs.is_empty() => AcceptedWitnesses::none(),
                None => return Err("witnesses need --witness-policy".into()),
                Some(f) => {
                    let raw = std::fs::read(f).map_err(|e| format!("read {f}: {e}"))?;
                    let v =
                        vsf_canon::parse(&raw).map_err(|e| format!("policy.parse:{}", e.code()))?;
                    let pol = WitnessPolicy::from_value(&v)?;
                    AcceptedWitnesses::collect(&docs, &pol, &obs)
                }
            };
            let mut out = derive(&rec, &ledger, &accepted, &BTreeMap::new()).to_value();
            if let vsf_canon::Value::Object(entries) = &mut out {
                let rejected = accepted
                    .rejected
                    .iter()
                    .map(|r| obj(vec![("id", s(&r.id)), ("reason", s(r.reason))]))
                    .collect();
                entries.push(("rejected_witnesses".to_string(), list(rejected)));
            }
            Ok(pretty(&out))
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
