#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

use vsf_canon::parse;
use vsf_ir::doc::{decode_document, digest_at};
use vsf_ir::{Body, Count, Instant, Label, VersionManifest};
use vsf_registry::manifest::{Accepted, Freshness, Signer, Trust, TrustedKey};
use vsf_versioning::promotion::{sign, signed_digest, verify};

type Edit = Box<dyn Fn(&mut VersionManifest)>;

const NOW: i64 = 1_792_368_600_000;

fn fixture() -> VersionManifest {
    let path = concat!(
        env!("CARGO_MANIFEST_DIR"),
        "/../../../schemas/ir/fixtures/valid/version-manifest-promotion-1-2.json"
    );
    let digest = digest_at("version", (1, 2, 0)).unwrap();
    let text = std::fs::read_to_string(path)
        .unwrap()
        .replace("$DIGEST", digest);
    let Body::Version(m) =
        decode_document(&vsf_canon::to_canonical(&parse(text.as_bytes()).unwrap())).unwrap()
    else {
        panic!("not a version manifest")
    };
    m
}

fn fresh(last: Option<Accepted>) -> Freshness {
    Freshness { now_ms: NOW, last }
}

fn trust() -> Trust {
    Trust::development().unwrap()
}

fn signed() -> VersionManifest {
    sign(fixture(), &Signer::development().unwrap()).unwrap()
}

fn code(m: &VersionManifest, f: &Freshness) -> &'static str {
    verify(m, &trust(), f).unwrap_err().code
}

#[test]
fn a_signed_promotion_verifies_and_names_its_high_water_mark() {
    let m = signed();
    let v = verify(&m, &trust(), &fresh(None)).unwrap();
    assert_eq!(
        (v.sequence, v.key_id.as_str(), v.purpose.as_str()),
        (7, "vsf-dev-1", "development")
    );
    assert_eq!(v.digest, signed_digest(&m).unwrap());
    assert!(
        verify(&m, &trust(), &fresh(Some(v.accepted()))).is_ok(),
        "the accepted manifest verifies again"
    );
}

#[test]
fn nothing_inside_the_signed_body_can_change_after_signing() {
    let edits: Vec<(&str, Edit)> = vec![
        (
            "sequence",
            Box::new(|m| m.promotion.as_mut().unwrap().sequence = Some(Count(99))),
        ),
        (
            "window",
            Box::new(|m| {
                m.promotion.as_mut().unwrap().not_after = Some(Instant(1_999_999_999_999))
            }),
        ),
        (
            "channel",
            Box::new(|m| m.channel = vsf_ir::Channel::Preview),
        ),
        ("lineage", Box::new(|m| m.lineage.clear())),
        (
            "artifact bytes",
            Box::new(|m| m.artifacts[0].bytes = Count(1)),
        ),
        (
            "gates",
            Box::new(|m| m.promotion.as_mut().unwrap().gates.clear()),
        ),
    ];
    for (name, edit) in edits {
        let mut m = signed();
        edit(&mut m);
        assert_eq!(
            code(&m, &fresh(None)),
            "promotion.digest_mismatch",
            "{name}"
        );
    }
}

#[test]
fn a_recomputed_digest_without_the_key_still_fails_the_signature() {
    let mut m = signed();
    m.promotion.as_mut().unwrap().sequence = Some(Count(99));
    let digest = signed_digest(&m).unwrap();
    m.promotion
        .as_mut()
        .unwrap()
        .attestation
        .as_mut()
        .unwrap()
        .signed_digest = vsf_ir::Digest::new(&digest).unwrap();
    assert_eq!(code(&m, &fresh(None)), "manifest.bad_signature");
}

#[test]
fn replay_rollback_expiry_and_conflict_are_refused() {
    let m = signed();
    let v = verify(&m, &trust(), &fresh(None)).unwrap();
    assert_eq!(
        code(
            &m,
            &fresh(Some(Accepted {
                sequence: 8,
                digest: v.digest.clone()
            }))
        ),
        "promotion.rollback"
    );
    assert_eq!(
        code(
            &m,
            &fresh(Some(Accepted {
                sequence: 7,
                digest: "sha256:other".into()
            }))
        ),
        "promotion.sequence_conflict"
    );
    assert_eq!(
        code(
            &m,
            &Freshness {
                now_ms: v.not_after_ms,
                last: None
            }
        ),
        "promotion.expired"
    );
    let mut zero = fixture();
    zero.promotion.as_mut().unwrap().sequence = Some(Count(0));
    let zero = sign(zero, &Signer::development().unwrap()).unwrap();
    assert_eq!(code(&zero, &fresh(None)), "promotion.bad_sequence");
}

#[test]
fn promotions_without_freshness_or_signature_are_not_trusted() {
    let mut legacy = fixture();
    legacy.promotion.as_mut().unwrap().sequence = None;
    legacy.promotion.as_mut().unwrap().not_after = None;
    assert_eq!(code(&legacy, &fresh(None)), "promotion.no_freshness");
    assert_eq!(
        sign(legacy, &Signer::development().unwrap())
            .unwrap_err()
            .code,
        "promotion.no_freshness"
    );
    let mut bare = signed();
    bare.promotion.as_mut().unwrap().attestation = None;
    assert_eq!(code(&bare, &fresh(None)), "promotion.unattested");
    let mut none = signed();
    none.promotion = None;
    assert_eq!(code(&none, &fresh(None)), "promotion.missing");
}

#[test]
fn only_trusted_active_keys_that_match_the_named_signer_pass() {
    let m = signed();
    let other = Signer::from_seed_hex("vsf-dev-1", &"07".repeat(32)).unwrap();
    let forged = sign(fixture(), &other).unwrap();
    assert_eq!(code(&forged, &fresh(None)), "manifest.bad_signature");
    let attacker = Signer::from_seed_hex("attacker", &"09".repeat(32)).unwrap();
    let unknown = sign(fixture(), &attacker).unwrap();
    assert_eq!(code(&unknown, &fresh(None)), "manifest.untrusted_key");
    let mut revoked = trust();
    revoked.keys[0].active = false;
    assert_eq!(
        verify(&m, &revoked, &fresh(None)).unwrap_err().code,
        "manifest.untrusted_key"
    );
    let mut relabelled = m.clone();
    relabelled.promotion.as_mut().unwrap().signer = Label::new("someone-else").unwrap();
    assert_eq!(code(&relabelled, &fresh(None)), "promotion.digest_mismatch");
    let digest = signed_digest(&relabelled).unwrap();
    relabelled
        .promotion
        .as_mut()
        .unwrap()
        .attestation
        .as_mut()
        .unwrap()
        .signed_digest = vsf_ir::Digest::new(&digest).unwrap();
    assert_eq!(code(&relabelled, &fresh(None)), "promotion.signer_mismatch");
    let mut two = trust();
    two.keys.push(TrustedKey {
        id: "vsf-dev-2".into(),
        public_key: other.public_hex(),
        purpose: "production".into(),
        active: true,
    });
    assert_eq!(
        verify(&m, &two, &fresh(None)).unwrap().purpose,
        "development"
    );
}
