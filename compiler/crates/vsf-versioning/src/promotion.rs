//! A promoted manifest is trusted only while its signature, its sequence and its window all hold.

use crate::{Res, refuse};
use vsf_ir::doc::typed_digest;
use vsf_ir::{Body, Digest, Ident, Label, PromotionSignature, SignatureAlg, VersionManifest};
use vsf_registry::manifest::{Accepted, Freshness, Signer, Trust, verify_bytes};

const DOMAIN: &[u8] = b"vsf-promotion-v1\n";

/// The manifest as the signer saw it: no attestation and no signature text.
fn signing_body(m: &VersionManifest) -> Option<Vec<u8>> {
    let mut copy = m.clone();
    let p = copy.promotion.as_mut()?;
    p.attestation = None;
    p.signature = Label::new("-").ok()?;
    Some(vsf_canon::to_canonical(&Body::Version(copy).encode()))
}

pub fn signed_digest(m: &VersionManifest) -> Res<String> {
    let bytes =
        signing_body(m).ok_or_else(|| refuse("promotion.missing", "no promotion record"))?;
    Ok(typed_digest("promotion", &bytes))
}

fn message(digest: &str) -> Vec<u8> {
    let mut out = DOMAIN.to_vec();
    out.extend_from_slice(digest.as_bytes());
    out
}

/// Signs the promotion in place; sequence and window are part of what is signed.
pub fn sign(mut m: VersionManifest, signer: &Signer) -> Res<VersionManifest> {
    let p = m
        .promotion
        .as_ref()
        .ok_or_else(|| refuse("promotion.missing", ""))?;
    if p.sequence.is_none() || p.not_after.is_none() {
        return Err(refuse(
            "promotion.no_freshness",
            "sequence and not_after are required",
        ));
    }
    if let Some(p) = m.promotion.as_mut() {
        p.signer =
            Label::new(&signer.key_id).map_err(|e| refuse("promotion.bad_key_id", e.code))?;
    }
    let digest = signed_digest(&m)?;
    let sig = signer.sign_bytes(&message(&digest));
    let key = Ident::new(&signer.key_id).map_err(|e| refuse("promotion.bad_key_id", e.code))?;
    let signed = Digest::new(&digest).map_err(|e| refuse("promotion.bad_digest", e.code))?;
    if let Some(p) = m.promotion.as_mut() {
        p.signer =
            Label::new(&signer.key_id).map_err(|e| refuse("promotion.bad_key_id", e.code))?;
        p.signature = Label::new(&sig).map_err(|e| refuse("promotion.bad_signature", e.code))?;
        p.attestation = Some(PromotionSignature {
            alg: SignatureAlg::Ed25519,
            key_id: key,
            signed_digest: signed,
        });
    }
    Ok(m)
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Verified {
    pub key_id: String,
    pub purpose: String,
    pub sequence: i64,
    pub not_after_ms: i64,
    /// Store with the sequence as the new high-water mark.
    pub digest: String,
}

impl Verified {
    pub fn accepted(&self) -> Accepted {
        Accepted {
            sequence: self.sequence,
            digest: self.digest.clone(),
        }
    }
}

/// `fresh.last` is the newest promotion this verifier accepted from the same signing key.
pub fn verify(m: &VersionManifest, trust: &Trust, fresh: &Freshness) -> Res<Verified> {
    let p = m
        .promotion
        .as_ref()
        .ok_or_else(|| refuse("promotion.missing", ""))?;
    let att = p
        .attestation
        .as_ref()
        .ok_or_else(|| refuse("promotion.unattested", ""))?;
    let (Some(seq), Some(end)) = (&p.sequence, &p.not_after) else {
        return Err(refuse(
            "promotion.no_freshness",
            "sequence and not_after are required",
        ));
    };
    let digest = signed_digest(m)?;
    if att.signed_digest.as_str() != digest {
        return Err(refuse(
            "promotion.digest_mismatch",
            att.signed_digest.as_str(),
        ));
    }
    if p.signer.as_str() != att.key_id.as_str() {
        return Err(refuse("promotion.signer_mismatch", p.signer.as_str()));
    }
    let purpose = verify_bytes(
        trust,
        att.key_id.as_str(),
        &message(&digest),
        p.signature.as_str(),
    )
    .map_err(|e| refuse(e.code, e.detail))?;
    let (sequence, not_after) = (seq.0, end.0);
    if sequence < 1 {
        return Err(refuse("promotion.bad_sequence", sequence.to_string()));
    }
    if fresh.now_ms >= not_after {
        return Err(refuse("promotion.expired", not_after.to_string()));
    }
    if let Some(last) = &fresh.last {
        if sequence < last.sequence {
            return Err(refuse(
                "promotion.rollback",
                format!("{sequence} < {}", last.sequence),
            ));
        }
        if sequence == last.sequence && digest != last.digest {
            return Err(refuse("promotion.sequence_conflict", sequence.to_string()));
        }
    }
    Ok(Verified {
        key_id: att.key_id.as_str().to_string(),
        purpose,
        sequence,
        not_after_ms: not_after,
        digest,
    })
}
