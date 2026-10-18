use std::io::{Read, Write};
use std::process::ExitCode;

const USAGE: &str =
    "usage: vsf-fabric <emit-fixture --seed N | compile [FILE] | verify [FILE] | info>";

fn read_input(path: Option<&String>) -> std::io::Result<Vec<u8>> {
    match path {
        Some(p) if p != "-" => std::fs::read(p),
        _ => {
            let mut buf = Vec::new();
            std::io::stdin()
                .take(vsf_canon::MAX_INPUT_BYTES as u64 + 1)
                .read_to_end(&mut buf)?;
            Ok(buf)
        }
    }
}

fn emit(bytes: &[u8]) -> ExitCode {
    let mut out = std::io::stdout().lock();
    if out.write_all(bytes).and_then(|()| out.flush()).is_err() {
        return ExitCode::from(74);
    }
    ExitCode::SUCCESS
}

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    match args.first().map(String::as_str) {
        Some("emit-fixture") => {
            let seed = match (
                args.get(1).map(String::as_str),
                args.get(2).map(|s| s.parse::<u64>()),
            ) {
                (Some("--seed"), Some(Ok(seed))) => seed,
                _ => {
                    eprintln!("{USAGE}");
                    return ExitCode::from(64);
                }
            };
            emit(&vsf_fixtures::generate_envelope_bytes(seed))
        }
        Some(cmd @ ("compile" | "verify")) => {
            let input = match read_input(args.get(1)) {
                Ok(bytes) => bytes,
                Err(err) => {
                    eprintln!("io: {err}");
                    return ExitCode::from(66);
                }
            };
            let result = if cmd == "compile" {
                vsf_fabric::compile_envelope(&input)
            } else {
                vsf_schema::verify_envelope_bytes(&input).map(|(_, canonical)| canonical)
            };
            match result {
                Ok(bytes) => emit(&bytes),
                Err(err) => {
                    eprintln!("error: {}", err.code());
                    ExitCode::from(2)
                }
            }
        }
        Some("redact") => match read_input(None).map(String::from_utf8) {
            Ok(Ok(text)) => emit(vsf_redact::redact(&text).as_bytes()),
            _ => ExitCode::from(66),
        },
        Some("info") => {
            println!(
                "{{\"compiler\":\"{}\",\"schema\":\"{}/{}\"}}",
                vsf_fabric::COMPILER_ID,
                vsf_schema::SCHEMA_NAME,
                vsf_schema::CURRENT_VERSION
            );
            ExitCode::SUCCESS
        }
        _ => {
            eprintln!("{USAGE}");
            ExitCode::from(64)
        }
    }
}
