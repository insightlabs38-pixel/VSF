use std::io::{Read, Write};
use std::process::ExitCode;

const USAGE: &str = "usage: vsf-fabric <emit-fixture --seed N | compile [FILE] | verify [FILE] | redact | pack-gen --seed N --nodes M | pack-analyze [FILE] | ir-validate [FILE] | pack-bench --seed N --nodes M --iterations K | serve | info>";

fn read_input(path: Option<&String>) -> std::io::Result<Vec<u8>> {
    read_input_limited(path, vsf_canon::MAX_INPUT_BYTES)
}

fn read_input_limited(path: Option<&String>, limit: usize) -> std::io::Result<Vec<u8>> {
    match path {
        Some(p) if p != "-" => std::fs::read(p),
        _ => {
            let mut buf = Vec::new();
            std::io::stdin()
                .take(limit as u64 + 1)
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

fn parse_flag(args: &[String], name: &str) -> Option<u64> {
    let i = args.iter().position(|a| a == name)?;
    args.get(i + 1)?.parse().ok()
}

/// Peak resident set (kB) of this process from /proc, or 0 where unavailable.
fn peak_rss_kb() -> u64 {
    std::fs::read_to_string("/proc/self/status")
        .ok()
        .and_then(|s| {
            s.lines()
                .find(|l| l.starts_with("VmHWM:"))
                .map(str::to_owned)
        })
        .and_then(|l| l.split_whitespace().nth(1).and_then(|v| v.parse().ok()))
        .unwrap_or(0)
}

fn report_rss() {
    if std::env::var_os("VSF_REPORT_RSS").is_some() {
        eprintln!("rss_kb={}", peak_rss_kb());
    }
}

/// Pure in-process compute lower bound for the analyze operation.
fn pack_bench(args: &[String]) -> ExitCode {
    let (Some(seed), Some(nodes), Some(iterations)) = (
        parse_flag(args, "--seed"),
        parse_flag(args, "--nodes"),
        parse_flag(args, "--iterations"),
    ) else {
        eprintln!("{USAGE}");
        return ExitCode::from(64);
    };
    let pack = vsf_fixtures::generate_pack(seed, nodes as u32);
    let mut micros: Vec<u64> = Vec::with_capacity(iterations as usize);
    for _ in 0..iterations {
        let start = std::time::Instant::now();
        if vsf_observation::analyze(&pack).is_err() {
            return ExitCode::from(2);
        }
        micros.push(start.elapsed().as_micros() as u64);
    }
    micros.sort_unstable();
    let rank = |p: usize| micros[((p * micros.len()).div_ceil(100)).clamp(1, micros.len()) - 1];
    println!(
        "{{\"iterations\":{iterations},\"pack_bytes\":{},\"p50_us\":{},\"p95_us\":{},\"p99_us\":{},\"max_us\":{},\"peak_rss_kb\":{}}}",
        pack.len(),
        rank(50),
        rank(95),
        rank(99),
        micros[micros.len() - 1],
        peak_rss_kb()
    );
    ExitCode::SUCCESS
}

/// Authorize request: u32-LE environment length, the environment as JSON, then the contract document.
fn authorize_frame(body: &[u8]) -> Result<Vec<u8>, &'static str> {
    let (len, rest) = body.split_at_checked(4).ok_or("authorize.truncated")?;
    let len = u32::from_le_bytes([len[0], len[1], len[2], len[3]]) as usize;
    let (env, doc) = rest.split_at_checked(len).ok_or("authorize.truncated")?;
    vsf_ir::api::authorize_report(doc, env)
}

/// Persistent worker: u32-LE length-prefixed request (op byte + payload), reply is status byte + u32-LE length + payload.
fn serve() -> ExitCode {
    let code = serve_loop();
    report_rss();
    code
}

fn serve_loop() -> ExitCode {
    let mut stdin = std::io::stdin().lock();
    let mut stdout = std::io::stdout().lock();
    loop {
        let mut len_buf = [0u8; 4];
        if stdin.read_exact(&mut len_buf).is_err() {
            return ExitCode::SUCCESS;
        }
        let len = u32::from_le_bytes(len_buf) as usize;
        if len == 0 || len > vsf_observation::MAX_PACK_BYTES + 1 {
            return ExitCode::from(65);
        }
        let mut frame = vec![0u8; len];
        if stdin.read_exact(&mut frame).is_err() {
            return ExitCode::from(66);
        }
        let result: Result<Vec<u8>, &'static str> = match frame[0] {
            b'A' => vsf_observation::analyze(&frame[1..]).map_err(|e| e.code()),
            b'C' => vsf_fabric::compile_envelope(&frame[1..]).map_err(|e| e.code()),
            b'V' => vsf_ir::api::validate_bytes(&frame[1..]),
            b'X' => authorize_frame(&frame[1..]),
            _ => Err("serve.unknown_op"),
        };
        let (status, payload) = match result {
            Ok(bytes) => (0u8, bytes),
            Err(code) => (2u8, code.as_bytes().to_vec()),
        };
        let wrote = stdout
            .write_all(&[status])
            .and_then(|()| stdout.write_all(&(payload.len() as u32).to_le_bytes()))
            .and_then(|()| stdout.write_all(&payload))
            .and_then(|()| stdout.flush());
        if wrote.is_err() {
            return ExitCode::from(74);
        }
    }
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
        Some("pack-gen") => match (parse_flag(&args, "--seed"), parse_flag(&args, "--nodes")) {
            (Some(seed), Some(nodes)) if nodes <= u64::from(vsf_observation::MAX_NODES) => {
                emit(&vsf_fixtures::generate_pack(seed, nodes as u32))
            }
            _ => {
                eprintln!("{USAGE}");
                ExitCode::from(64)
            }
        },
        Some("pack-analyze") => {
            let input = match read_input_limited(args.get(1), vsf_observation::MAX_PACK_BYTES) {
                Ok(bytes) => bytes,
                Err(err) => {
                    eprintln!("io: {err}");
                    return ExitCode::from(66);
                }
            };
            let result = vsf_observation::analyze(&input);
            report_rss();
            match result {
                Ok(bytes) => emit(&bytes),
                Err(err) => {
                    eprintln!("error: {}", err.code());
                    ExitCode::from(2)
                }
            }
        }
        Some("ir-validate") => {
            let input = match read_input(args.get(1)) {
                Ok(bytes) => bytes,
                Err(err) => {
                    eprintln!("io: {err}");
                    return ExitCode::from(66);
                }
            };
            match vsf_ir::api::validate_report(&input) {
                Ok((bytes, valid)) => {
                    let code = emit(&bytes);
                    if code == ExitCode::SUCCESS && !valid {
                        ExitCode::from(3)
                    } else {
                        code
                    }
                }
                Err(code) => {
                    eprintln!("error: {code}");
                    ExitCode::from(2)
                }
            }
        }
        Some("serve") => serve(),
        Some("pack-bench") => pack_bench(&args),
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
