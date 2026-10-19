use std::path::Path;
use std::process::ExitCode;
use vsf_registry::{dashboard, fixture, harness, import, json, manifest, model, policy};

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
        Some("dashboard") => {
            let root = Path::new(
                args.get(1)
                    .ok_or("usage: dashboard <repo-root> [--write|--check]")?,
            );
            let target = root.join(manifest::DASHBOARD);
            let dash = dashboard::Dashboard::build(
                &harness::load_package(root).map_err(|e| e.to_string())?,
            );
            let want = json::pretty(&dash.encode());
            match args.get(2).map(String::as_str) {
                Some("--write") => {
                    std::fs::write(&target, &want).map_err(|e| e.to_string())?;
                    Ok(dash.render())
                }
                Some("--check") => match std::fs::read_to_string(&target) {
                    Ok(have) if have == want => Ok(dash.render()),
                    _ => Err(format!(
                        "{} is stale; rerun with --write",
                        manifest::DASHBOARD
                    )),
                },
                _ => Ok(dash.render()),
            }
        }
        Some("manifest") => {
            let root = Path::new(args.get(2).ok_or(
                "usage: manifest <build|verify|trust> <repo-root> [--development | --trust FILE]",
            )?);
            match args.get(1).map(String::as_str) {
                Some("build") => {
                    let signer = match std::env::var("VSF_PACKAGE_SIGNING_SEED") {
                        Ok(seed) => manifest::Signer::from_seed_hex(
                            &std::env::var("VSF_PACKAGE_SIGNING_KEY_ID")
                                .map_err(|_| "VSF_PACKAGE_SIGNING_KEY_ID is required")?,
                            &seed,
                        ),
                        Err(_) if args.iter().any(|a| a == "--dev") => {
                            manifest::Signer::development()
                        }
                        Err(_) => return Err("set VSF_PACKAGE_SIGNING_SEED or pass --dev".into()),
                    }
                    .map_err(|e| e.to_string())?;
                    let pkg = harness::load_package(root).map_err(|e| e.to_string())?;
                    let m = manifest::build(&pkg, &signer).map_err(|e| e.to_string())?;
                    std::fs::write(root.join(manifest::MANIFEST), json::pretty(&m))
                        .map_err(|e| e.to_string())?;
                    Ok(format!(
                        "signed {} primitives with {}\n",
                        pkg.registry.entries.len(),
                        signer.key_id
                    ))
                }
                Some("trust") => {
                    let trust = manifest::Trust::development().map_err(|e| e.to_string())?;
                    std::fs::write(root.join(manifest::TRUST), json::pretty(&trust.encode()))
                        .map_err(|e| e.to_string())?;
                    Ok("wrote development trust\n".into())
                }
                Some("verify") => {
                    let development = args.iter().any(|a| a == "--development");
                    let trust_path = match args.iter().position(|a| a == "--trust") {
                        Some(i) => {
                            let given = Path::new(args.get(i + 1).ok_or("--trust needs a file")?);
                            let inside = given
                                .canonicalize()
                                .and_then(|g| root.canonicalize().map(|r| g.starts_with(r)))
                                .map_err(|e| e.to_string())?;
                            if inside && !development {
                                return Err("the trust set must come from outside the package being verified".into());
                            }
                            given.to_path_buf()
                        }
                        None if development => root.join(manifest::TRUST),
                        None => {
                            return Err(
                                "pass --trust FILE from outside the package, or --development to accept the committed development trust"
                                    .into(),
                            );
                        }
                    };
                    let trust = manifest::Trust::decode(
                        &model::load(&read(&trust_path.to_string_lossy())?).map_err(|e| e.to_string())?,
                    )
                    .map_err(|e| e.to_string())?;
                    let bytes = read(&root.join(manifest::MANIFEST).to_string_lossy())?;
                    let v = manifest::verify(root, &bytes, &trust).map_err(|e| e.to_string())?;
                    let v = v
                        .require_purpose(if development { "development" } else { "production" })
                        .map_err(|e| e.to_string())?;
                    Ok(format!(
                        "verified v{} key={} ({}) files={} primitives={} root={}\n",
                        v.version, v.key_id, v.purpose, v.files, v.primitives, v.primitive_root
                    ))
                }
                _ => Err("usage: manifest <build|verify|trust> <repo-root> [--development | --trust FILE]".into()),
            }
        }
        _ => Err("usage: vsf-registry <import|gen-fixtures|check|dashboard|manifest>".into()),
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
