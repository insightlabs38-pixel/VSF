//! Signed, versioned package manifest: file digests, per-primitive digests and an Ed25519 signature.

use crate::graph;
use crate::harness::{CATALOG, FIXTURES, POLICY, Package, REGISTRY, STATUS, load_package};
use crate::json::{Error, Obj, Res, array, err, object, s};
use crate::model::load;
use crate::status::encode_registration;
use ed25519_compact::{KeyPair, PublicKey, Seed, Signature};
use std::collections::{BTreeMap, BTreeSet};
use std::path::Path;
use vsf_canon::{Value, sha256_hex, to_canonical};

pub const SCHEMA: &str = "vsf-primitive-package-v1";
pub const MANIFEST: &str = "registry/primitives/package.manifest.v1.json";
pub const TRUST: &str = "registry/primitives/trust.v1.json";
pub const DASHBOARD: &str = "registry/primitives/dashboard.v1.json";
pub const PACKAGE_VERSION: &str = "0.1.0";
pub const DEV_KEY_ID: &str = "vsf-dev-1";
const DOMAIN: &[u8] = b"vsf-primitive-package-v1\n";

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

fn unhex(x: &str) -> Res<Vec<u8>> {
    if !x.len().is_multiple_of(2)
        || !x
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
    {
        return Err(err(
            "manifest.bad_hex",
            x.chars().take(16).collect::<String>(),
        ));
    }
    (0..x.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&x[i..i + 2], 16).map_err(|_| err("manifest.bad_hex", x)))
        .collect()
}

fn n(x: usize) -> Value {
    Value::Int(i64::try_from(x).unwrap_or(i64::MAX))
}

pub struct Signer {
    pub key_id: String,
    pair: KeyPair,
}

impl Signer {
    pub fn from_seed_hex(key_id: &str, seed: &str) -> Res<Signer> {
        let raw: [u8; 32] = unhex(seed)?
            .try_into()
            .map_err(|_| err("manifest.bad_seed", "seed must be 32 bytes"))?;
        Ok(Signer {
            key_id: key_id.to_string(),
            pair: KeyPair::from_seed(Seed::new(raw)),
        })
    }

    /// Derived, public and development-only: it proves reproducibility, not publisher identity.
    pub fn development() -> Res<Signer> {
        let seed = sha256_hex(b"vsf-primitive-dev-signing-seed-v1");
        Signer::from_seed_hex(DEV_KEY_ID, &seed)
    }

    pub fn public_hex(&self) -> String {
        hex(&self.pair.pk[..])
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TrustedKey {
    pub id: String,
    pub public_key: String,
    pub purpose: String,
    pub active: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Trust {
    pub keys: Vec<TrustedKey>,
}

impl Trust {
    pub fn decode(v: &Value) -> Res<Trust> {
        let mut o = Obj::new(v, "")?;
        if o.str("schema")? != "vsf-package-trust-v1" {
            return Err(err("manifest.bad_schema", "trust"));
        }
        let mut keys = Vec::new();
        for (i, x) in array(o.req("keys")?, "/keys")?.iter().enumerate() {
            let mut k = Obj::new(x, &format!("/keys/{i}"))?;
            if k.str("alg")? != "ed25519" {
                return Err(err("manifest.bad_alg", format!("/keys/{i}")));
            }
            let status = k.str("status")?;
            keys.push(TrustedKey {
                id: k.str("id")?,
                public_key: k.str("public_key")?,
                purpose: k.str("purpose")?,
                active: status == "active",
            });
            k.finish()?;
        }
        o.finish()?;
        Ok(Trust { keys })
    }

    pub fn encode(&self) -> Value {
        let keys = self
            .keys
            .iter()
            .map(|k| {
                object(vec![
                    ("alg", s("ed25519")),
                    ("id", s(&k.id)),
                    ("public_key", s(&k.public_key)),
                    ("purpose", s(&k.purpose)),
                    ("status", s(if k.active { "active" } else { "revoked" })),
                ])
            })
            .collect();
        object(vec![
            ("keys", Value::Array(keys)),
            ("schema", s("vsf-package-trust-v1")),
        ])
    }

    pub fn development() -> Res<Trust> {
        Ok(Trust {
            keys: vec![TrustedKey {
                id: DEV_KEY_ID.to_string(),
                public_key: Signer::development()?.public_hex(),
                purpose: "development".to_string(),
                active: true,
            }],
        })
    }
}

fn read(root: &Path, rel: &str) -> Res<Vec<u8>> {
    std::fs::read(root.join(rel)).map_err(|e| err("manifest.file_missing", format!("{rel}: {e}")))
}

fn fixture_files(root: &Path) -> Res<Vec<String>> {
    fn walk(base: &Path, dir: &Path, out: &mut Vec<String>) -> Res<()> {
        let rd = std::fs::read_dir(dir)
            .map_err(|e| err("manifest.io", format!("{}: {e}", dir.display())))?;
        for entry in rd {
            let path = entry.map_err(|e| err("manifest.io", e.to_string()))?.path();
            if path.is_dir() {
                walk(base, &path, out)?;
            } else if path.extension().is_some_and(|x| x == "json")
                && let Ok(rel) = path.strip_prefix(base)
            {
                out.push(rel.to_string_lossy().replace('\\', "/"));
            }
        }
        Ok(())
    }
    let mut out = Vec::new();
    walk(root, &root.join(FIXTURES), &mut out)?;
    out.sort();
    Ok(out)
}

/// Every file the package's behavior or claims depend on.
pub fn file_set(pkg: &Package) -> Res<BTreeSet<String>> {
    let mut set: BTreeSet<String> = [REGISTRY, POLICY, STATUS, CATALOG, DASHBOARD]
        .into_iter()
        .map(String::from)
        .collect();
    set.extend(fixture_files(&pkg.root)?);
    for r in pkg.registered.values() {
        set.extend(r.implementation.clone());
        set.extend(
            r.evidence
                .iter()
                .filter(|e| e.kind == "spec-document")
                .map(|e| e.id.clone()),
        );
    }
    set.extend(
        pkg.status
            .compositions
            .iter()
            .map(|c| c.implementation.clone()),
    );
    Ok(set)
}

/// Each digest also folds in its dependencies' digests, so changing a leaf changes every dependent.
pub fn primitive_digests(pkg: &Package) -> Res<BTreeMap<String, String>> {
    let order = graph::verify(&pkg.registry).map_err(|e| err(e[0].code(), e[0].describe()))?;
    let index = pkg.registry.index();
    let fixtures: BTreeMap<&str, String> = pkg
        .fixtures
        .iter()
        .map(|f| (f.id.as_str(), sha256_hex(&to_canonical(&f.encode()))))
        .collect();
    let mut digests: BTreeMap<String, String> = BTreeMap::new();
    for id in order {
        let entry = index
            .get(id.as_str())
            .ok_or_else(|| err("manifest.unknown_id", id.clone()))?;
        let mut witnesses = Vec::new();
        let (mut registration, mut implementation, mut specs) =
            (Value::Null, Value::Null, Vec::new());
        if let Some(r) = pkg.registered.get(&id) {
            registration = encode_registration(r);
            for e in &r.evidence {
                match e.kind.as_str() {
                    "fixture" => {
                        let d = fixtures
                            .get(e.id.as_str())
                            .ok_or_else(|| err("manifest.unknown_fixture", e.id.clone()))?;
                        witnesses.push(object(vec![("digest", s(d)), ("id", s(&e.id))]));
                    }
                    "spec-document" => {
                        let d = sha256_hex(&read(&pkg.root, &e.id)?);
                        specs.push(object(vec![("digest", s(&d)), ("path", s(&e.id))]));
                    }
                    _ => {}
                }
            }
            if let Some(path) = &r.implementation {
                implementation = s(&sha256_hex(&read(&pkg.root, path)?));
            }
        }
        let deps: Vec<Value> = entry
            .requires
            .iter()
            .map(|d| {
                object(vec![
                    ("digest", s(digests.get(d).map_or("", String::as_str))),
                    ("id", s(d)),
                ])
            })
            .collect();
        let body = object(vec![
            ("entry", entry.encode()),
            ("implementation", implementation),
            ("registration", registration),
            ("requires", Value::Array(deps)),
            ("specs", Value::Array(specs)),
            ("witnesses", Value::Array(witnesses)),
        ]);
        digests.insert(id, sha256_hex(&to_canonical(&body)));
    }
    Ok(digests)
}

pub fn primitive_root(digests: &BTreeMap<String, String>) -> String {
    let rows = digests
        .iter()
        .map(|(id, d)| object(vec![("digest", s(d)), ("id", s(id))]))
        .collect();
    sha256_hex(&to_canonical(&Value::Array(rows)))
}

fn signing_bytes(manifest: &Value) -> Res<Vec<u8>> {
    let Value::Object(entries) = manifest else {
        return Err(err("manifest.not_object", ""));
    };
    let body = Value::Object(
        entries
            .iter()
            .filter(|(k, _)| k != "signature")
            .cloned()
            .collect(),
    );
    let mut bytes = DOMAIN.to_vec();
    bytes.extend(to_canonical(&body));
    Ok(bytes)
}

/// Replaces any signature on `manifest` with one over its current body.
pub fn sign(manifest: Value, signer: &Signer) -> Res<Value> {
    let Value::Object(entries) = manifest else {
        return Err(err("manifest.not_object", ""));
    };
    let mut m = Value::Object(
        entries
            .into_iter()
            .filter(|(k, _)| k != "signature")
            .collect(),
    );
    let sig = signer.pair.sk.sign(signing_bytes(&m)?, None);
    if let Value::Object(entries) = &mut m {
        entries.push((
            "signature".to_string(),
            object(vec![
                ("alg", s("ed25519")),
                ("key_id", s(&signer.key_id)),
                ("value", s(&hex(&sig[..]))),
            ]),
        ));
    }
    Ok(m)
}

pub fn build(pkg: &Package, signer: &Signer) -> Res<Value> {
    let files = file_set(pkg)?
        .into_iter()
        .map(|rel| {
            let bytes = read(&pkg.root, &rel)?;
            Ok(object(vec![
                ("bytes", n(bytes.len())),
                ("path", s(&rel)),
                ("sha256", s(&sha256_hex(&bytes))),
            ]))
        })
        .collect::<Res<Vec<_>>>()?;
    let digests = primitive_digests(pkg)?;
    let prims = digests
        .iter()
        .map(|(id, d)| object(vec![("digest", s(d)), ("id", s(id))]))
        .collect();
    let body = object(vec![
        ("catalog_sha256", s(&pkg.registry.catalog_sha256)),
        ("files", Value::Array(files)),
        ("ir_version", s(vsf_ir::generated::IR_VERSION_TEXT)),
        ("package", s("vsf-primitives")),
        ("primitive_root", s(&primitive_root(&digests))),
        ("primitives", Value::Array(prims)),
        ("schema", s(SCHEMA)),
        ("version", s(PACKAGE_VERSION)),
    ]);
    sign(body, signer)
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Verified {
    pub version: String,
    pub key_id: String,
    pub purpose: String,
    pub files: usize,
    pub primitives: usize,
    pub primitive_root: String,
}

fn field_strs(v: &Value, path: &str, keys: [&str; 2]) -> Res<Vec<(String, String)>> {
    let mut out = Vec::new();
    for (i, x) in array(v, path)?.iter().enumerate() {
        let mut o = Obj::new(x, &format!("{path}/{i}"))?;
        let a = o.str(keys[0])?;
        let b = o.str(keys[1])?;
        if keys[0] == "path" {
            o.int("bytes")?;
        }
        o.finish()?;
        out.push((a, b));
    }
    Ok(out)
}

fn check_signature(manifest: &Value, trust: &Trust) -> Res<(String, String)> {
    let sig = manifest
        .get("signature")
        .ok_or_else(|| err("manifest.unsigned", "signature missing"))?;
    let mut o = Obj::new(sig, "/signature")?;
    if o.str("alg")? != "ed25519" {
        return Err(err("manifest.bad_alg", "signature"));
    }
    let key_id = o.str("key_id")?;
    let value = o.str("value")?;
    o.finish()?;
    let key = trust
        .keys
        .iter()
        .find(|k| k.id == key_id && k.active)
        .ok_or_else(|| err("manifest.untrusted_key", key_id.clone()))?;
    let pk = PublicKey::from_slice(&unhex(&key.public_key)?)
        .map_err(|_| err("manifest.bad_key", key_id.clone()))?;
    let sig = Signature::from_slice(&unhex(&value)?)
        .map_err(|_| err("manifest.bad_signature", "length"))?;
    pk.verify(signing_bytes(manifest)?, &sig)
        .map_err(|_| err("manifest.bad_signature", "does not verify"))?;
    Ok((key_id, key.purpose.clone()))
}

/// Any mismatch is a hard error: signature first, then files, then claims and primitive digests.
pub fn verify(root: &Path, manifest_bytes: &[u8], trust: &Trust) -> Result<Verified, Error> {
    let manifest = load(manifest_bytes)?;
    if manifest.get("schema").and_then(Value::as_str) != Some(SCHEMA) {
        return Err(err("manifest.bad_schema", "package"));
    }
    let (key_id, purpose) = check_signature(&manifest, trust)?;
    let mut o = Obj::new(&manifest, "")?;
    let catalog = o.str("catalog_sha256")?;
    let files = field_strs(o.req("files")?, "/files", ["path", "sha256"])?;
    let ir = o.str("ir_version")?;
    let _ = o.str("package")?;
    let claimed_root = o.str("primitive_root")?;
    let prims = field_strs(o.req("primitives")?, "/primitives", ["id", "digest"])?;
    let _ = o.str("schema")?;
    let _ = o.opt("signature");
    let version = o.str("version")?;
    o.finish()?;
    if ir != vsf_ir::generated::IR_VERSION_TEXT {
        return Err(err("manifest.ir_mismatch", ir));
    }
    for (path, want) in &files {
        let got = sha256_hex(&read(root, path)?);
        if &got != want {
            return Err(err("manifest.file_digest_mismatch", path.clone()));
        }
    }
    let pkg = load_package(root)?;
    let listed: BTreeSet<&str> = files.iter().map(|(p, _)| p.as_str()).collect();
    if let Some(extra) = file_set(&pkg)?
        .iter()
        .find(|p| !listed.contains(p.as_str()))
    {
        return Err(err("manifest.unlisted_file", extra.clone()));
    }
    if pkg.registry.catalog_sha256 != catalog {
        return Err(err("manifest.catalog_mismatch", catalog));
    }
    let digests = primitive_digests(&pkg)?;
    let claimed: BTreeMap<&str, &str> = prims
        .iter()
        .map(|(i, d)| (i.as_str(), d.as_str()))
        .collect();
    if claimed.len() != prims.len() || claimed.len() != digests.len() {
        return Err(err(
            "manifest.primitive_set_mismatch",
            format!("{} vs {}", claimed.len(), digests.len()),
        ));
    }
    for (id, d) in &digests {
        if claimed.get(id.as_str()) != Some(&d.as_str()) {
            return Err(err("manifest.primitive_digest_mismatch", id.clone()));
        }
    }
    if primitive_root(&digests) != claimed_root {
        return Err(err("manifest.root_mismatch", claimed_root));
    }
    Ok(Verified {
        version,
        key_id,
        purpose,
        files: files.len(),
        primitives: digests.len(),
        primitive_root: claimed_root,
    })
}
