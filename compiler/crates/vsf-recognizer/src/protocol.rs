//! Protocol evidence: strict validation of the whole document, and a verdict on how much it may be trusted.

use crate::trust::{TrustContext, app_id_ok, digest_ok, origin_ok, principal_ok};
use crate::val::{obj, s};
use std::collections::{BTreeMap, BTreeSet};
use vsf_canon::{Value, sha256_hex, to_canonical};

pub const SCHEMA: &str = "vsf-protocol-evidence-v1";
const MAX_FIELDS: usize = 256;
const MAX_SAFE: i64 = 9_007_199_254_740_991;
const SKEW_MS: i64 = 5 * 60 * 1000;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProtocolField {
    pub name: String,
    pub ty: String,
    pub required: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Authorization {
    pub basis: String,
    pub principal_ref: Option<String>,
    pub scope: Option<String>,
    pub granted_at: Option<i64>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Promotion {
    pub decision_id: String,
    pub reviewed_by: String,
    pub documentation_reference: String,
    pub witnesses: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Protocol {
    pub id: String,
    /// Content digest of the canonical document: the identity evidence cites, since ids are author-chosen.
    pub digest: String,
    pub source_app_id: String,
    pub origin: String,
    pub kind: String,
    pub stability: String,
    pub support: String,
    pub method: String,
    pub recorded_at: i64,
    pub authorization: Authorization,
    pub operation: String,
    pub request: Vec<ProtocolField>,
    pub response: Vec<ProtocolField>,
    pub promotion: Option<Promotion>,
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord)]
pub struct Problem {
    pub code: &'static str,
    pub path: String,
}

#[derive(Default)]
struct Checker {
    problems: Vec<Problem>,
}

type Fields<'a> = &'a [(String, Value)];

impl Checker {
    fn bad(&mut self, code: &'static str, path: &str) {
        self.problems.push(Problem {
            code,
            path: path.to_string(),
        });
    }

    fn object<'a>(
        &mut self,
        v: Option<&'a Value>,
        path: &str,
        required: &[&str],
        allowed: &[&str],
    ) -> Option<Fields<'a>> {
        let v = v?;
        let Value::Object(entries) = v else {
            self.bad("schema.type", path);
            return None;
        };
        for (k, _) in entries {
            if !allowed.contains(&k.as_str()) {
                self.bad("schema.unknown_field", &format!("{path}/{k}"));
            }
        }
        for k in required {
            if !entries.iter().any(|(x, _)| x == k) {
                self.bad("schema.required", &format!("{path}/{k}"));
            }
        }
        Some(entries)
    }

    fn string<'a>(
        &mut self,
        v: Option<&'a Value>,
        path: &str,
        ok: Option<fn(&str) -> bool>,
        max: usize,
    ) -> Option<&'a str> {
        let v = v?;
        let Value::Str(x) = v else {
            self.bad("schema.type", path);
            return None;
        };
        if x.is_empty() || x.chars().count() > max || ok.is_some_and(|f| !f(x)) {
            self.bad("schema.pattern", path);
            return None;
        }
        Some(x)
    }

    fn one_of<'a>(&mut self, v: Option<&'a Value>, path: &str, set: &[&str]) -> Option<&'a str> {
        let v = v?;
        match v.as_str() {
            Some(x) if set.contains(&x) => Some(x),
            _ => {
                self.bad("schema.enum", path);
                None
            }
        }
    }

    fn safe_int(&mut self, v: Option<&Value>, path: &str) -> Option<i64> {
        let v = v?;
        match v.as_int() {
            Some(t) if (0..=MAX_SAFE).contains(&t) => Some(t),
            _ => {
                self.bad("schema.type", path);
                None
            }
        }
    }
}

fn get<'a>(f: Fields<'a>, k: &str) -> Option<&'a Value> {
    f.iter().find(|(x, _)| x == k).map(|(_, v)| v)
}

fn scope_ok(x: &str) -> bool {
    let b = x.as_bytes();
    !b.is_empty()
        && b.len() <= 96
        && b[0].is_ascii_lowercase()
        && b[1..]
            .iter()
            .all(|&c| c.is_ascii_lowercase() || c.is_ascii_digit() || b"_.:-".contains(&c))
}

fn field_name_ok(x: &str) -> bool {
    let b = x.as_bytes();
    b.len() <= 64
        && (b[0].is_ascii_alphabetic() || b[0] == b'_')
        && b[1..]
            .iter()
            .all(|&c| c.is_ascii_alphanumeric() || b"_.-".contains(&c))
}

fn hex_ok(x: &str, min: usize, max: usize) -> bool {
    (min..=max).contains(&x.len())
        && x.bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}

fn identifier_ok(x: &str) -> bool {
    let b = x.as_bytes();
    b.len() <= 64
        && (b[0].is_ascii_alphabetic() || b[0] == b'_')
        && b[1..]
            .iter()
            .all(|&c| c.is_ascii_alphanumeric() || b"_.".contains(&c))
}

fn check_fields(c: &mut Checker, v: Option<&Value>, path: &str) {
    let Some(Value::Array(items)) = v else {
        c.bad("schema.type", path);
        return;
    };
    if items.len() > MAX_FIELDS {
        c.bad("schema.too_many", path);
    }
    let mut seen = BTreeSet::new();
    for (i, it) in items.iter().enumerate() {
        let p = format!("{path}/{i}");
        let Some(o) = c.object(
            Some(it),
            &p,
            &["name", "type", "required"],
            &["name", "type", "required"],
        ) else {
            continue;
        };
        if let Some(n) = c.string(
            get(o, "name"),
            &format!("{p}/name"),
            Some(field_name_ok),
            64,
        ) && !seen.insert(n.to_string())
        {
            c.bad("schema.duplicate", &p);
        }
        c.one_of(
            get(o, "type"),
            &format!("{p}/type"),
            &[
                "string", "integer", "number", "boolean", "object", "array", "null", "unknown",
            ],
        );
        if !matches!(get(o, "required"), Some(Value::Bool(_))) {
            c.bad("schema.type", &format!("{p}/required"));
        }
    }
}

fn is_template_segment(seg: &str) -> bool {
    seg.strip_prefix('{')
        .and_then(|r| r.strip_suffix('}'))
        .is_some_and(|name| {
            let b = name.as_bytes();
            !b.is_empty()
                && (b[0].is_ascii_alphabetic() || b[0] == b'_')
                && b.iter().all(|&c| c.is_ascii_alphanumeric() || c == b'_')
        })
}

fn concrete_segment(seg: &str) -> bool {
    let all_digits = !seg.is_empty() && seg.bytes().all(|b| b.is_ascii_digit());
    let long_hex = seg.len() >= 8 && seg.bytes().all(|b| b.is_ascii_hexdigit());
    let uuid = seg.len() == 36
        && seg.split('-').map(str::len).eq([8, 4, 4, 4, 12])
        && seg.bytes().all(|b| b == b'-' || b.is_ascii_hexdigit());
    all_digits || long_hex || uuid
}

fn check_path(c: &mut Checker, value: &str, path: &str) {
    for seg in value.split('/').skip(1) {
        if seg.contains(['{', '}']) && !is_template_segment(seg) {
            c.bad("evidence.bad_template", path);
        } else if !seg.starts_with('{') && (concrete_segment(seg) || seg.len() > 40) {
            c.bad("evidence.concrete_identifier", path);
        }
    }
}

fn https_reference(c: &mut Checker, value: &str, path: &str) {
    let Some(rest) = value.strip_prefix("https://") else {
        c.bad("evidence.bad_reference", path);
        return;
    };
    let authority = rest.split(['/', '?', '#']).next().unwrap_or("");
    if authority.is_empty() {
        c.bad("evidence.bad_reference", path);
    }
    if authority.contains('@') || rest.contains('?') {
        c.bad("evidence.credentials_in_url", path);
    }
}

fn secret_shaped(c: &mut Checker, v: &Value, path: &str) {
    match v {
        Value::Str(x) => {
            if vsf_redact::redact(x) != *x {
                c.bad("evidence.secret_shaped", path);
            }
        }
        Value::Array(items) => {
            for (i, x) in items.iter().enumerate() {
                secret_shaped(c, x, &format!("{path}/{i}"));
            }
        }
        Value::Object(entries) => {
            for (k, x) in entries {
                secret_shaped(c, x, &format!("{path}/{k}"));
            }
        }
        _ => {}
    }
}

fn read_fields(v: &Value) -> Vec<ProtocolField> {
    let Some(Value::Array(items)) = Some(v) else {
        return Vec::new();
    };
    items
        .iter()
        .map(|it| ProtocolField {
            name: it.get("name").and_then(Value::as_str).unwrap_or("").into(),
            ty: it.get("type").and_then(Value::as_str).unwrap_or("").into(),
            required: matches!(it.get("required"), Some(Value::Bool(true))),
        })
        .collect()
}

/// Every structural and promotion rule, mirroring `schemas/protocol-evidence/v1.schema.json` and the TypeScript checker.
pub fn validate(v: &Value) -> Vec<Problem> {
    let mut c = Checker::default();
    let top_keys = [
        "schema",
        "id",
        "source_app_id",
        "origin",
        "kind",
        "provenance",
        "endpoint",
        "shape",
        "stability",
        "support",
        "promotion",
    ];
    let Some(top) = c.object(
        Some(v),
        "",
        &[
            "schema",
            "id",
            "source_app_id",
            "origin",
            "kind",
            "provenance",
            "endpoint",
            "shape",
            "stability",
            "support",
        ],
        &top_keys,
    ) else {
        if c.problems.is_empty() {
            c.bad("schema.type", "");
        }
        return c.problems;
    };
    if get(top, "schema").and_then(Value::as_str) != Some(SCHEMA) && get(top, "schema").is_some() {
        c.bad("schema.enum", "/schema");
    }
    c.string(
        get(top, "id"),
        "/id",
        Some(|x| x.strip_prefix("pev_").is_some_and(|h| hex_ok(h, 16, 64))),
        300,
    );
    c.string(
        get(top, "source_app_id"),
        "/source_app_id",
        Some(app_id_ok),
        300,
    );
    c.string(get(top, "origin"), "/origin", Some(origin_ok), 300);
    let kind = c.one_of(
        get(top, "kind"),
        "/kind",
        &[
            "documented_api",
            "published_schema",
            "user_supplied_spec",
            "observed_traffic",
        ],
    );
    let stability = c.one_of(
        get(top, "stability"),
        "/stability",
        &["documented", "undocumented"],
    );
    let support = c.one_of(
        get(top, "support"),
        "/support",
        &["unsupported", "candidate", "supported"],
    );

    let mut method = None;
    let mut basis = None;
    let mut recorded = None;
    let mut authz: Option<Fields> = None;
    if let Some(prov) = c.object(
        get(top, "provenance"),
        "/provenance",
        &["method", "recorded_at", "authorization"],
        &["method", "reference", "recorded_at", "authorization"],
    ) {
        method = c.one_of(
            get(prov, "method"),
            "/provenance/method",
            &[
                "vendor_documentation",
                "published_schema",
                "user_assertion",
                "runtime_observation",
            ],
        );
        c.string(get(prov, "reference"), "/provenance/reference", None, 300);
        recorded = c.safe_int(get(prov, "recorded_at"), "/provenance/recorded_at");
        authz = c.object(
            get(prov, "authorization"),
            "/provenance/authorization",
            &["basis"],
            &["basis", "principal_ref", "scope", "granted_at"],
        );
        if let Some(a) = authz {
            basis = c.one_of(
                get(a, "basis"),
                "/provenance/authorization/basis",
                &["public_documentation", "source_terms", "user_grant"],
            );
            c.string(
                get(a, "principal_ref"),
                "/provenance/authorization/principal_ref",
                Some(principal_ok),
                300,
            );
            c.string(
                get(a, "scope"),
                "/provenance/authorization/scope",
                Some(scope_ok),
                300,
            );
            if get(a, "granted_at").is_some() {
                c.safe_int(get(a, "granted_at"), "/provenance/authorization/granted_at");
            }
        }
    }
    if let Some(ep) = c.object(
        get(top, "endpoint"),
        "/endpoint",
        &["protocol", "path_template"],
        &["protocol", "method", "path_template", "operation"],
    ) {
        c.one_of(
            get(ep, "protocol"),
            "/endpoint/protocol",
            &["http", "graphql", "websocket", "grpc"],
        );
        if get(ep, "method").is_some() {
            c.one_of(
                get(ep, "method"),
                "/endpoint/method",
                &["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"],
            );
        }
        let path_ok: fn(&str) -> bool = |x| {
            x.starts_with('/')
                && x.len() <= 199
                && x.bytes()
                    .all(|b| b.is_ascii_alphanumeric() || b"_.{}/-".contains(&b))
        };
        if let Some(p) = c.string(
            get(ep, "path_template"),
            "/endpoint/path_template",
            Some(path_ok),
            300,
        ) {
            check_path(&mut c, p, "/endpoint/path_template");
        }
        c.string(
            get(ep, "operation"),
            "/endpoint/operation",
            Some(identifier_ok),
            300,
        );
    }
    if let Some(sh) = c.object(
        get(top, "shape"),
        "/shape",
        &["request", "response"],
        &["request", "response"],
    ) {
        check_fields(&mut c, get(sh, "request"), "/shape/request");
        check_fields(&mut c, get(sh, "response"), "/shape/response");
    }
    let mut promo: Option<Fields> = None;
    if get(top, "promotion").is_some() {
        let keys = [
            "decision_id",
            "reviewed_by",
            "documentation_reference",
            "witnesses",
        ];
        promo = c.object(get(top, "promotion"), "/promotion", &keys, &keys);
        if let Some(p) = promo {
            c.string(
                get(p, "decision_id"),
                "/promotion/decision_id",
                Some(scope_ok),
                300,
            );
            c.string(
                get(p, "reviewed_by"),
                "/promotion/reviewed_by",
                Some(principal_ok),
                300,
            );
            if let Some(r) = c.string(
                get(p, "documentation_reference"),
                "/promotion/documentation_reference",
                None,
                300,
            ) {
                https_reference(&mut c, r, "/promotion/documentation_reference");
            }
            let ok = matches!(get(p, "witnesses"), Some(Value::Array(w)) if !w.is_empty() && w.len() <= 64 && w.iter().all(|x| x.as_str().is_some_and(witness_id_ok)));
            if !ok {
                c.bad("evidence.promotion_invalid", "/promotion/witnesses");
            }
        }
    }
    if !c.problems.is_empty() {
        return c.problems;
    }

    let (Some(kind), Some(method), Some(stability), Some(support)) =
        (kind, method, stability, support)
    else {
        return c.problems;
    };
    let expected = match kind {
        "documented_api" => "vendor_documentation",
        "published_schema" => "published_schema",
        "user_supplied_spec" => "user_assertion",
        _ => "runtime_observation",
    };
    if method != expected {
        c.bad("evidence.kind_method_mismatch", "/provenance/method");
    }
    let documented = matches!(kind, "documented_api" | "published_schema");
    if (stability == "documented") != documented {
        c.bad("evidence.stability_mismatch", "/stability");
    }
    let reference = get(top, "provenance")
        .and_then(|p| p.get("reference"))
        .and_then(Value::as_str);
    if let Some(r) = reference {
        https_reference(&mut c, r, "/provenance/reference");
    }
    if documented && reference.is_none() {
        c.bad(
            "evidence.documented_without_reference",
            "/provenance/reference",
        );
    }
    if documented {
        if basis == Some("user_grant") {
            c.bad(
                "evidence.authorization_basis",
                "/provenance/authorization/basis",
            );
        }
    } else {
        if basis != Some("user_grant") {
            c.bad(
                "evidence.missing_authorization",
                "/provenance/authorization/basis",
            );
        }
        let a = authz.unwrap_or(&[]);
        if get(a, "principal_ref").is_none()
            || get(a, "scope").is_none()
            || get(a, "granted_at").is_none()
        {
            c.bad(
                "evidence.missing_authorization",
                "/provenance/authorization",
            );
        } else if get(a, "granted_at").and_then(Value::as_int) > recorded {
            c.bad(
                "evidence.authorization_after_record",
                "/provenance/authorization/granted_at",
            );
        }
    }
    if support == "supported" {
        if promo.is_none() && !(documented && basis != Some("user_grant")) {
            c.bad("evidence.undocumented_supported", "/support");
        }
    } else if promo.is_some() {
        c.bad("evidence.promotion_without_support", "/promotion");
    }
    secret_shaped(&mut c, v, "");
    c.problems
}

fn witness_id_ok(x: &str) -> bool {
    let b = x.as_bytes();
    !b.is_empty()
        && b.len() <= 64
        && b[0].is_ascii_lowercase()
        && b[1..]
            .iter()
            .all(|&c| c.is_ascii_lowercase() || c.is_ascii_digit() || b"_-".contains(&c))
}

impl Protocol {
    /// Parses and validates the whole document; any problem refuses it.
    pub fn from_json(bytes: &[u8]) -> Result<Protocol, String> {
        let v = vsf_canon::parse(bytes).map_err(|e| format!("protocol.parse:{}", e.code()))?;
        Protocol::from_value(&v)
    }

    pub fn from_value(v: &Value) -> Result<Protocol, String> {
        if let Some(p) = validate(v).first() {
            return Err(format!("{}@{}", p.code, p.path));
        }
        let text = |o: &Value, k: &str| o.get(k).and_then(Value::as_str).unwrap_or("").to_string();
        let prov = v.get("provenance").unwrap_or(v);
        let au = prov.get("authorization").unwrap_or(v);
        let ep = v.get("endpoint").unwrap_or(v);
        let shape = v.get("shape").unwrap_or(v);
        let path = text(ep, "path_template");
        let operation = format!("{} {path}", text(ep, "method")).trim().to_string();
        let promotion = v.get("promotion").map(|p| Promotion {
            decision_id: text(p, "decision_id"),
            reviewed_by: text(p, "reviewed_by"),
            documentation_reference: text(p, "documentation_reference"),
            witnesses: match p.get("witnesses") {
                Some(Value::Array(w)) => w
                    .iter()
                    .filter_map(|x| x.as_str().map(String::from))
                    .collect(),
                _ => Vec::new(),
            },
        });
        Ok(Protocol {
            id: text(v, "id"),
            digest: format!("sha256:{}", sha256_hex(&to_canonical(v))),
            source_app_id: text(v, "source_app_id"),
            origin: text(v, "origin"),
            kind: text(v, "kind"),
            stability: text(v, "stability"),
            support: text(v, "support"),
            method: text(prov, "method"),
            recorded_at: prov.get("recorded_at").and_then(Value::as_int).unwrap_or(0),
            authorization: Authorization {
                basis: text(au, "basis"),
                principal_ref: au
                    .get("principal_ref")
                    .and_then(Value::as_str)
                    .map(String::from),
                scope: au.get("scope").and_then(Value::as_str).map(String::from),
                granted_at: au.get("granted_at").and_then(Value::as_int),
            },
            operation,
            request: read_fields(shape.get("request").unwrap_or(v)),
            response: read_fields(shape.get("response").unwrap_or(v)),
            promotion,
        })
    }

    pub fn to_ref(&self) -> Value {
        obj(vec![("digest", s(&self.digest)), ("id", s(&self.id))])
    }
}

fn closed(v: &Value, required: &[&str], optional: &[&str], at: &str) -> Result<(), String> {
    let Value::Object(entries) = v else {
        return Err(format!("policy.not_object:{at}"));
    };
    for (k, _) in entries {
        if !required.contains(&k.as_str()) && !optional.contains(&k.as_str()) {
            return Err(format!("policy.unknown_key:{at}/{k}"));
        }
    }
    for k in required {
        if v.get(k).is_none() {
            return Err(format!("policy.missing:{at}/{k}"));
        }
    }
    Ok(())
}

/// A grant of authority the user's side currently holds.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Grant {
    pub principal: String,
    pub scope: String,
    pub granted_at: i64,
    pub revoked_at: Option<i64>,
}

/// What may be revoked: documents by content digest, people and verifiers by identity.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Revocations {
    pub documents: BTreeSet<String>,
    pub principals: BTreeSet<String>,
    pub witnesses: BTreeSet<String>,
    pub authorities: BTreeSet<String>,
    pub operations: BTreeSet<(u32, String)>,
}

impl Revocations {
    pub fn well_formed(&self) -> bool {
        self.documents.iter().all(|d| digest_ok(d))
            && self.principals.iter().all(|p| principal_ok(p))
    }
}

/// Everything the verdict is judged against; supplied by the host, never by the document.
#[derive(Debug, Clone)]
pub struct ProtocolPolicy {
    pub trust: TrustContext,
    pub now: i64,
    pub max_age_ms: i64,
    pub required_scope: String,
    pub reviewers: BTreeSet<String>,
    pub grants: Vec<Grant>,
    pub known_witnesses: BTreeSet<String>,
    pub revoked: Revocations,
}

fn strs(v: &Value, key: &str) -> Result<BTreeSet<String>, String> {
    match v.get(key) {
        None => Ok(BTreeSet::new()),
        Some(Value::Array(a)) => a
            .iter()
            .map(|x| {
                x.as_str()
                    .map(String::from)
                    .ok_or_else(|| format!("policy.type:{key}"))
            })
            .collect(),
        Some(_) => Err(format!("policy.type:{key}")),
    }
}

impl Revocations {
    pub fn from_value(v: &Value) -> Result<Revocations, String> {
        closed(
            v,
            &[],
            &["documents", "principals", "witnesses", "authorities"],
            "/revoked",
        )?;
        let r = Revocations {
            documents: strs(v, "documents")?,
            principals: strs(v, "principals")?,
            witnesses: strs(v, "witnesses")?,
            authorities: strs(v, "authorities")?,
            operations: BTreeSet::new(),
        };
        if !r.well_formed() {
            return Err("policy.revocations_malformed".into());
        }
        Ok(r)
    }
}

impl ProtocolPolicy {
    pub fn from_value(v: &Value) -> Result<ProtocolPolicy, String> {
        closed(
            v,
            &["schema", "trust", "now", "max_age_ms", "required_scope"],
            &["reviewers", "grants", "known_witnesses", "revoked"],
            "",
        )?;
        if v.get("schema").and_then(Value::as_str) != Some("vsf-protocol-policy-v1") {
            return Err("policy.bad_schema".into());
        }
        let trust = TrustContext::from_value(v.get("trust").ok_or("policy.missing:trust")?)?;
        let int = |k: &str| {
            v.get(k)
                .and_then(Value::as_int)
                .filter(|t| (0..=MAX_SAFE).contains(t))
                .ok_or_else(|| format!("policy.type:{k}"))
        };
        let required_scope = v
            .get("required_scope")
            .and_then(Value::as_str)
            .ok_or("policy.type:required_scope")?;
        if !scope_ok(required_scope) {
            return Err("policy.required_scope".into());
        }
        let reviewers = strs(v, "reviewers")?;
        if !reviewers.iter().all(|r| principal_ok(r)) {
            return Err("policy.reviewers".into());
        }
        let mut grants = Vec::new();
        if let Some(Value::Array(items)) = v.get("grants") {
            for g in items {
                closed(
                    g,
                    &["principal", "scope", "granted_at"],
                    &["revoked_at"],
                    "/grants",
                )?;
                let principal = g
                    .get("principal")
                    .and_then(Value::as_str)
                    .filter(|p| principal_ok(p));
                let scope = g
                    .get("scope")
                    .and_then(Value::as_str)
                    .filter(|x| scope_ok(x));
                let granted_at = g.get("granted_at").and_then(Value::as_int);
                let (Some(principal), Some(scope), Some(granted_at)) =
                    (principal, scope, granted_at)
                else {
                    return Err("policy.grant".into());
                };
                grants.push(Grant {
                    principal: principal.into(),
                    scope: scope.into(),
                    granted_at,
                    revoked_at: g.get("revoked_at").and_then(Value::as_int),
                });
            }
        }
        Ok(ProtocolPolicy {
            trust,
            now: int("now")?,
            max_age_ms: int("max_age_ms")?,
            required_scope: required_scope.into(),
            reviewers,
            grants,
            known_witnesses: strs(v, "known_witnesses")?,
            revoked: match v.get("revoked") {
                None => Revocations::default(),
                Some(r) => Revocations::from_value(r)?,
            },
        })
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Standing {
    /// May raise a conflict against the UI.
    Authoritative,
    /// Honest but insufficient: it can only raise a question.
    Advisory(&'static str),
    /// Wrong application, origin or principal, revoked, or claiming authority it does not have: no comparison is made.
    Rejected(&'static str),
}

impl Standing {
    pub fn as_str(self) -> &'static str {
        match self {
            Standing::Authoritative => "authoritative",
            Standing::Advisory(_) => "advisory",
            Standing::Rejected(_) => "rejected",
        }
    }

    pub fn reason(self) -> Option<&'static str> {
        match self {
            Standing::Authoritative => None,
            Standing::Advisory(r) | Standing::Rejected(r) => Some(r),
        }
    }
}

/// Only supported, promoted, fresh evidence for this application, origin and principal is authoritative.
pub fn assess(p: &Protocol, pol: &ProtocolPolicy) -> Standing {
    let t = &pol.trust;
    if p.source_app_id != t.app_id {
        return Standing::Rejected("wrong_application");
    }
    if p.origin != t.origin {
        return Standing::Rejected("wrong_origin");
    }
    let r = &pol.revoked;
    if r.documents.contains(&p.digest) {
        return Standing::Rejected("revoked_document");
    }
    if p.recorded_at > pol.now.saturating_add(SKEW_MS) {
        return Standing::Rejected("from_the_future");
    }
    let a = &p.authorization;
    if a.basis == "user_grant" {
        let (Some(who), Some(scope), Some(at)) =
            (a.principal_ref.as_deref(), a.scope.as_deref(), a.granted_at)
        else {
            return Standing::Rejected("grant_incomplete");
        };
        if who != t.principal {
            return Standing::Rejected("wrong_principal");
        }
        if r.principals.contains(who) {
            return Standing::Rejected("revoked_principal");
        }
        let held = pol
            .grants
            .iter()
            .find(|g| g.principal == who && g.scope == scope && g.granted_at == at);
        match held {
            None => return Standing::Rejected("grant_not_held"),
            Some(g) if g.revoked_at.is_some_and(|x| x <= pol.now) => {
                return Standing::Rejected("grant_revoked");
            }
            Some(_) => {}
        }
        if scope != pol.required_scope {
            return Standing::Rejected("scope_not_sufficient");
        }
    }
    if let Some(pm) = &p.promotion {
        if !pol.reviewers.contains(&pm.reviewed_by) {
            return Standing::Rejected("promotion_unauthorized");
        }
        if r.principals.contains(&pm.reviewed_by) {
            return Standing::Rejected("revoked_reviewer");
        }
        if a.principal_ref.as_deref() == Some(pm.reviewed_by.as_str()) {
            return Standing::Rejected("self_promotion");
        }
    }
    if p.recorded_at.saturating_add(pol.max_age_ms) < pol.now {
        return Standing::Advisory("stale");
    }
    match p.support.as_str() {
        "unsupported" => return Standing::Advisory("unsupported"),
        "candidate" => return Standing::Advisory("candidate"),
        _ => {}
    }
    if p.kind == "user_supplied_spec" {
        return Standing::Advisory("user_supplied");
    }
    let Some(pm) = &p.promotion else {
        return Standing::Advisory("unpromoted");
    };
    if !pm
        .witnesses
        .iter()
        .all(|w| pol.known_witnesses.contains(w) && !r.witnesses.contains(w))
    {
        return Standing::Advisory("promotion_unverified");
    }
    Standing::Authoritative
}

/// Operations two authoritative documents describe differently; both stay in the ledger and the clash is a conflict.
pub fn contradictions<'a>(
    docs: &[(&'a Protocol, Standing)],
) -> Vec<(&'a Protocol, &'a Protocol, String)> {
    let mut by_op: BTreeMap<(&str, &str, &str), Vec<&'a Protocol>> = BTreeMap::new();
    for (p, st) in docs {
        if *st == Standing::Authoritative {
            by_op
                .entry((&p.source_app_id, &p.origin, &p.operation))
                .or_default()
                .push(p);
        }
    }
    let mut out = Vec::new();
    for list in by_op.values() {
        for (i, a) in list.iter().enumerate() {
            for b in &list[i + 1..] {
                if a.digest == b.digest {
                    continue;
                }
                let shape = |p: &Protocol| {
                    let mut f: Vec<_> = p
                        .request
                        .iter()
                        .map(|x| (x.name.clone(), x.ty.clone(), x.required))
                        .collect();
                    f.sort();
                    f
                };
                if shape(a) != shape(b) {
                    out.push((*a, *b, a.operation.clone()));
                }
            }
        }
    }
    out
}
