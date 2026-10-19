#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

use super::bytes;
use vsf_canon::{Value, parse, sha256_hex};
use vsf_ir::doc::{document_digest_at, encode_document_at};
use vsf_ir::matrix::Support;
use vsf_ir::rt::Ctx;
use vsf_ir::{
    ArtifactRef, ArtifactRole, Channel, Count, Digest, GateResult, Ident, Instant, Label,
    PrincipalRef, PromotionRecord, Retention, RollbackKind, RollbackSpec, Tier, VersionAxes,
    VersionManifest, Visibility,
};
use vsf_registry::json::pretty;
use vsf_registry::manifest::{Signer, Trust};
use vsf_versioning::lineage::manifest_id;
use vsf_versioning::promotion::sign;

pub const PRINCIPAL: &str = "prin_b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2";

pub struct Sample {
    /// Content-addressed artifact files: digest and bytes.
    pub blobs: Vec<(String, Vec<u8>)>,
    /// Named manifest documents in promotion order, with their ids.
    pub manifests: Vec<(&'static str, String, Vec<u8>)>,
    pub trust: Vec<u8>,
    pub support: Vec<u8>,
}

fn matrix() -> Value {
    let path = format!(
        "{}/../../../schemas/ir/fixtures/matrix/matrix.json",
        env!("CARGO_MANIFEST_DIR")
    );
    parse(&std::fs::read(path).unwrap()).unwrap()
}

fn doc(name: &str, version: (u32, u32, u32), edit: impl Fn(&mut Value)) -> (String, Vec<u8>) {
    let family = name.split('-').next().unwrap();
    let path = format!(
        "{}/../../../schemas/ir/fixtures/valid/{name}.json",
        env!("CARGO_MANIFEST_DIR")
    );
    let digest = vsf_ir::doc::digest_at(family, version).unwrap();
    let text = std::fs::read_to_string(path)
        .unwrap()
        .replace("$DIGEST", digest);
    let mut v = parse(text.as_bytes()).unwrap();
    edit(&mut v);
    let body = vsf_ir::doc::decode_document(&vsf_canon::to_canonical(&v)).unwrap();
    (
        document_digest_at(&body, version).unwrap(),
        encode_document_at(&body, version).unwrap(),
    )
}

fn set(v: &mut Value, pointer: &str, new: Value) {
    let mut cur = v;
    let segs: Vec<&str> = pointer.trim_start_matches('/').split('/').collect();
    for s in &segs[..segs.len() - 1] {
        cur = match cur {
            Value::Object(e) => &mut e.iter_mut().find(|(k, _)| k == s).unwrap().1,
            Value::Array(a) => &mut a[s.parse::<usize>().unwrap()],
            _ => panic!(),
        };
    }
    let Value::Object(e) = cur else { panic!() };
    let last = segs[segs.len() - 1];
    match e.iter_mut().find(|(k, _)| k == last) {
        Some(slot) => slot.1 = new,
        None => e.push((last.to_string(), new)),
    }
}

fn artifact(role: ArtifactRole, digest: &str, blob: &[u8]) -> ArtifactRef {
    let private = matches!(
        role,
        ArtifactRole::Observation | ArtifactRole::Semantic | ArtifactRole::OperationContract
    );
    ArtifactRef {
        role,
        digest: Digest::new(digest).unwrap(),
        bytes: Count(blob.len() as i64),
        media_type: Label::new("application/json").unwrap(),
        visibility: Some(if private {
            Visibility::PrivatePrincipal
        } else {
            Visibility::GlobalStructure
        }),
        principal_ref: private.then(|| PrincipalRef::new(PRINCIPAL).unwrap()),
    }
}

#[allow(clippy::too_many_arguments)]
fn manifest(
    channel: Channel,
    created: i64,
    presentation_version: &str,
    artifacts: Vec<ArtifactRef>,
    lineage: &[&str],
    previous: Option<&str>,
    sequence: i64,
    rollback: Option<RollbackSpec>,
) -> VersionManifest {
    let mut axes = VersionAxes::decode(
        matrix().get("base").unwrap(),
        "",
        &Ctx { version: (1, 2, 0) },
    )
    .unwrap();
    axes.presentation = vsf_ir::PresentationVersion::new(presentation_version).unwrap();
    let promoted = created + 1_000;
    VersionManifest {
        created_at: Instant(created),
        channel,
        axes,
        artifacts,
        lineage: lineage.iter().map(|d| Digest::new(d).unwrap()).collect(),
        migrations: vec![],
        retention: Retention {
            tier: Tier::Pro,
            immutable: true,
            pinned: false,
            legal_hold: false,
            expires_at: None,
        },
        promotion: (channel == Channel::Stable).then(|| PromotionRecord {
            promoted_at: Instant(promoted),
            gates: vec![GateResult {
                gate: Ident::new("g2").unwrap(),
                passed: true,
            }],
            signer: Label::new("vsf-dev-1").unwrap(),
            signature: Label::new("-").unwrap(),
            previous: previous.map(|p| Digest::new(p).unwrap()),
            attestation: None,
            sequence: Some(Count(sequence)),
            not_after: Some(Instant(promoted + 365 * 86_400_000)),
        }),
        origin: None,
        rollback,
        revocations: None,
    }
}

fn finish(m: VersionManifest) -> (String, Vec<u8>) {
    let signed = if m.promotion.is_some() {
        sign(m, &Signer::development().unwrap()).unwrap()
    } else {
        m
    };
    let b = bytes(&signed);
    (manifest_id(&b).unwrap().0, b)
}

pub fn build() -> Sample {
    let (obs_d, obs) = doc("observation-full", (1, 0, 0), |_| {});
    let (sem_d, sem) = doc("semantic-orders", (1, 0, 0), |_| {});
    let (op_d, op) = doc("operation-update-stage", (1, 0, 0), |_| {});
    let (pres1_d, pres1) = doc("presentation-orders", (1, 0, 0), |_| {});
    let (pres2_d, pres2) = doc("presentation-orders", (1, 0, 0), |v| {
        set(v, "/body/theme/density", Value::Str("compact".into()))
    });
    let (pres3_d, pres3) = doc("presentation-orders", (1, 0, 0), |v| {
        set(v, "/body/theme/accent", Value::Str("blue".into()))
    });
    let pack = b"vsf-primitive-pack fixture v1".to_vec();
    let pack_d = format!("sha256:{}", sha256_hex(&pack));
    let arts = |pres_d: &str, pres: &[u8]| {
        vec![
            artifact(ArtifactRole::Observation, &obs_d, &obs),
            artifact(ArtifactRole::Semantic, &sem_d, &sem),
            artifact(ArtifactRole::OperationContract, &op_d, &op),
            artifact(ArtifactRole::Presentation, pres_d, pres),
            artifact(ArtifactRole::PrimitivePack, &pack_d, &pack),
        ]
    };
    let t0 = 1_792_368_400_000;
    let (m1_id, m1) = finish(manifest(
        Channel::Stable,
        t0,
        "pv1",
        arts(&pres1_d, &pres1),
        &[],
        None,
        1,
        None,
    ));
    let (m2_id, m2) = finish(manifest(
        Channel::Stable,
        t0 + 10_000,
        "pv2",
        arts(&pres2_d, &pres2),
        &[&m1_id],
        Some(&m1_id),
        2,
        None,
    ));
    let (b_id, b) = finish(manifest(
        Channel::Preview,
        t0 + 11_000,
        "pv3",
        arts(&pres3_d, &pres3),
        &[&m1_id],
        None,
        0,
        None,
    ));
    let (m3_id, m3) = finish(manifest(
        Channel::Stable,
        t0 + 20_000,
        "pv3",
        arts(&pres3_d, &pres3),
        &[&m2_id, &b_id],
        Some(&m2_id),
        3,
        None,
    ));
    let rollback = RollbackSpec {
        kind: RollbackKind::Interface,
        target: Digest::new(&m1_id).unwrap(),
        roles: vec![ArtifactRole::Presentation],
    };
    let (r_id, r) = finish(manifest(
        Channel::Stable,
        t0 + 30_000,
        "pv1",
        arts(&pres1_d, &pres1),
        &[&m3_id, &m1_id],
        Some(&m3_id),
        4,
        Some(rollback),
    ));
    let mut blobs = vec![];
    for (d, bytes) in [
        (obs_d, obs),
        (sem_d, sem),
        (op_d, op),
        (pres1_d, pres1),
        (pres2_d, pres2),
        (pres3_d, pres3),
        (pack_d, pack),
    ] {
        blobs.push((d, bytes));
    }
    blobs.sort();
    let _ = Support::from_value(matrix().get("host").unwrap()).unwrap();
    Sample {
        blobs,
        manifests: vec![
            ("m1", m1_id, m1),
            ("m2", m2_id, m2),
            ("branch", b_id, b),
            ("m3", m3_id, m3),
            ("rollback", r_id, r),
        ],
        trust: pretty(&Trust::development().unwrap().encode()).into_bytes(),
        support: pretty(matrix().get("host").unwrap()).into_bytes(),
    }
}
