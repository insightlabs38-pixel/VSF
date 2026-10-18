use std::path::Path;
use std::process::ExitCode;
use vsf_registry::{fixture, harness, import, json, model, policy};

fn read(path: &str) -> Result<Vec<u8>, String> {
    std::fs::read(path).map_err(|e| format!("{path}: {e}"))
}

fn run(args: &[String]) -> Result<String, String> {
    match args.first().map(String::as_str) {
        Some("import") => {
            let (Some(catalog), Some(pol)) = (args.get(1), args.get(2)) else {
                return Err("usage: import <catalog> <policy>".into());
            };
            let policy =
                policy::Policy::decode(&model::load(&read(pol)?).map_err(|e| e.to_string())?)
                    .map_err(|e| e.to_string())?;
            let reg = import::import(&read(catalog)?, &policy).map_err(|e| e.to_string())?;
            Ok(json::pretty(&reg.encode()))
        }
        Some("gen-fixtures") => {
            let dir = args.get(1).ok_or("usage: gen-fixtures <dir>")?;
            let mut n = 0;
            for (rel, text) in fixture::seeded_files() {
                let path = Path::new(dir).join(rel);
                std::fs::create_dir_all(path.parent().ok_or("no parent")?)
                    .map_err(|e| e.to_string())?;
                std::fs::write(&path, text).map_err(|e| format!("{}: {e}", path.display()))?;
                n += 1;
            }
            Ok(format!("wrote {n} fixtures\n"))
        }
        Some("check") => {
            let root = args.get(1).ok_or("usage: check <repo-root>")?;
            let pkg = harness::load_package(Path::new(root)).map_err(|e| e.to_string())?;
            Ok(format!(
                "ok: {} entries, {} registered, {} fixtures\n",
                pkg.registry.entries.len(),
                pkg.registered.len(),
                pkg.fixtures.len()
            ))
        }
        _ => Err("usage: vsf-registry <import|gen-fixtures|check>".into()),
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
            eprintln!("error: {e}");
            ExitCode::from(2)
        }
    }
}
