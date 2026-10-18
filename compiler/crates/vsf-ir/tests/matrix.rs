#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

mod common;
use common::fixtures;
use vsf_canon::{Value, parse};
use vsf_ir::VersionAxes;
use vsf_ir::matrix::{AXES, Pins, Support, evaluate, overall, results_json};
use vsf_ir::rt::Ctx;

fn set(v: &mut Value, key: &str, new: Option<Value>) {
    let Value::Object(entries) = v else {
        panic!("object")
    };
    entries.retain(|(k, _)| k != key);
    if let Some(n) = new {
        entries.push((key.to_string(), n));
    }
}

fn load() -> Value {
    parse(&std::fs::read(fixtures().join("matrix/matrix.json")).unwrap()).unwrap()
}

fn entries(v: &Value) -> &[(String, Value)] {
    let Value::Object(e) = v else {
        panic!("object")
    };
    e
}

#[test]
fn every_matrix_case_yields_the_expected_status_and_reason() {
    let doc = load();
    let Some(Value::Array(cases)) = doc.get("cases") else {
        panic!("cases")
    };
    assert!(cases.len() >= 45);
    let ctx = Ctx { version: (1, 0, 0) };
    let base_axes = doc.get("base").unwrap();
    let base_host = doc.get("host").unwrap();
    let baseline = evaluate(
        &VersionAxes::decode(base_axes, "", &ctx).unwrap(),
        &Support::from_value(base_host).unwrap(),
        &Pins::new(),
    );
    assert!(baseline.iter().all(|r| r.reason == "ok"), "{baseline:?}");

    for case in cases {
        let name = case.get("name").unwrap().as_str().unwrap();
        let mut axes = base_axes.clone();
        for (k, v) in entries(case.get("set").unwrap()) {
            set(&mut axes, k, Some(v.clone()));
        }
        let mut host = base_host.clone();
        for (k, v) in entries(case.get("host").unwrap()) {
            set(
                &mut host,
                k,
                if *v == Value::Null {
                    None
                } else {
                    Some(v.clone())
                },
            );
        }
        let pins: Pins = entries(case.get("pins").unwrap())
            .iter()
            .map(|(k, v)| (k.clone(), v.as_str().unwrap().to_string()))
            .collect();
        let got = evaluate(
            &VersionAxes::decode(&axes, "", &ctx).unwrap(),
            &Support::from_value(&host).unwrap(),
            &pins,
        );
        assert_eq!(got.len(), AXES.len());

        let expect = entries(case.get("expect").unwrap());
        for (axis, want) in expect {
            if axis == "overall" {
                assert_eq!(
                    overall(&got).as_str(),
                    want.as_str().unwrap().split(':').next().unwrap(),
                    "{name}"
                );
                continue;
            }
            let r = got.iter().find(|r| r.axis == axis).unwrap();
            assert_eq!(
                format!("{}:{}", r.status.as_str(), r.reason),
                want.as_str().unwrap(),
                "{name}/{axis}"
            );
        }
        // Axes evolve independently: nothing outside the changed or pinned axes may move.
        let touched = |axis: &str| {
            entries(case.get("set").unwrap())
                .iter()
                .any(|(k, _)| k == axis)
                || entries(case.get("host").unwrap()).iter().any(|(k, _)| {
                    k.starts_with(axis)
                        || (k == "observed" && axis.starts_with("source"))
                        || (k == "runtime" && axis == "runtime")
                        || (k == "cache_format" && axis == "cache_format")
                })
                || entries(case.get("pins").unwrap())
                    .iter()
                    .any(|(k, _)| k == axis)
        };
        for (r, b) in got.iter().zip(&baseline) {
            if !touched(r.axis) {
                assert_eq!(r, b, "{name}: {} moved without being changed", r.axis);
            }
        }
        assert_eq!(results_json(&got), results_json(&got));
    }
}

#[test]
fn evaluation_json_is_canonical_and_stable() {
    let doc = load();
    let ctx = Ctx { version: (1, 0, 0) };
    let r = evaluate(
        &VersionAxes::decode(doc.get("base").unwrap(), "", &ctx).unwrap(),
        &Support::from_value(doc.get("host").unwrap()).unwrap(),
        &Pins::new(),
    );
    let text = String::from_utf8(results_json(&r)).unwrap();
    assert!(
        text.starts_with(r#"{"axes":[{"axis":"source_app","reason":"ok","status":"supported"}"#),
        "{text}"
    );
    assert!(text.ends_with(r#"],"overall":"supported"}"#), "{text}");
    assert_eq!(
        vsf_canon::to_canonical(&parse(text.as_bytes()).unwrap()),
        text.as_bytes()
    );
}

#[test]
fn malformed_host_descriptions_are_rejected() {
    let doc = load();
    let host = doc.get("host").unwrap();
    for key in [
        "compiler",
        "operations",
        "min_policy",
        "runtime",
        "primitive_sets",
    ] {
        let mut h = host.clone();
        set(&mut h, key, None);
        assert!(Support::from_value(&h).is_err(), "{key}");
    }
    let mut h = host.clone();
    set(&mut h, "runtime", Some(Value::Str("not-a-version".into())));
    assert!(Support::from_value(&h).is_err());
}
