#![allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]

use std::collections::BTreeSet;
use vsf_ir::identity::*;

const KEY: &[u8] = b"test-key-0123456789";

fn hex(b: [u8; 32]) -> String {
    b.iter().map(|x| format!("{x:02x}")).collect()
}

#[test]
fn hmac_matches_rfc_4231_vectors() {
    let long = [0xaau8; 131];
    let seq: Vec<u8> = (1..=25).collect();
    let cases: [(&[u8], &[u8], &str); 6] = [
        (&[0x0b; 20], b"Hi There", "b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7"),
        (b"Jefe", b"what do ya want for nothing?", "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"),
        (&[0xaa; 20], &[0xdd; 50], "773ea91e36800e46854db8ebd09181a72959098b3ef8c122d9635514ced565fe"),
        (&seq, &[0xcd; 50], "82558a389a443c0ea4cc819899f2083a85f0faa3e578f8077a2e3ff46729665b"),
        (&long, b"Test Using Larger Than Block-Size Key - Hash Key First", "60e431591ee0b67f0d8a26aacbf5b77f8e0bc6213728c5140546040f0ee37f54"),
        (&long, b"This is a test using a larger than block-size key and a larger than block-size data. The key needs to be hashed before being used by the HMAC algorithm.", "9b09ffa71b942fcb27635fbcd5b0e944bfdc63644f0713938a7f51535c3a35e2"),
    ];
    for (key, msg, want) in cases {
        assert_eq!(hex(hmac_sha256(key, msg)), want);
    }
}

fn d(
    tenant: Option<&str>,
    roles: &[&str],
    locale: &str,
    flags: &[(&str, &str)],
) -> VariantDescriptor {
    VariantDescriptor {
        app_id: "vendor-console:app.vendor.test".into(),
        tenant: tenant.map(Into::into),
        roles: roles.iter().map(|s| (*s).into()).collect(),
        locale: locale.into(),
        flags: flags
            .iter()
            .map(|(k, v)| ((*k).into(), (*v).into()))
            .collect(),
    }
}

#[test]
fn variant_id_is_pinned_and_independently_reproducible() {
    let v = variant(
        KEY,
        &d(
            Some("acme"),
            &["sales", "admin"],
            "en-us",
            &[("beta", "on")],
        ),
    )
    .unwrap();
    assert_eq!(v.id, "var_a75553c70231887ad162c9f081a38f20");
    assert_eq!(v.class, "admin_sales");
    assert_eq!(
        String::from_utf8(v.canonical).unwrap(),
        r#"{"app_id":"vendor-console:app.vendor.test","flags":{"beta":"on"},"locale":"en-US","roles":["admin","sales"],"tenant":"acme"}"#
    );
}

#[test]
fn equivalent_descriptors_share_an_identity() {
    let a = variant(
        KEY,
        &d(
            Some("acme"),
            &["sales", "admin", "sales"],
            "EN-us",
            &[("b", "1"), ("a", "2")],
        ),
    )
    .unwrap();
    let b = variant(
        KEY,
        &d(
            Some("acme"),
            &["admin", "sales"],
            "en-US",
            &[("a", "2"), ("b", "1"), ("a", "2")],
        ),
    )
    .unwrap();
    assert_eq!(a, b);
}

#[test]
fn every_tenant_role_locale_flag_combination_is_distinct() {
    let tenants = [None, Some("acme"), Some("globex")];
    let roles: [&[&str]; 3] = [&[], &["admin"], &["admin", "sales"]];
    let locales = ["en-US", "en-GB", "de-DE", "zh-Hant-TW"];
    let flags: [&[(&str, &str)]; 3] = [&[], &[("beta", "on")], &[("beta", "off")]];
    let mut seen = BTreeSet::new();
    let mut total = 0;
    for t in tenants {
        for r in roles {
            for l in locales {
                for f in flags {
                    seen.insert(variant(KEY, &d(t, r, l, f)).unwrap().id);
                    total += 1;
                }
            }
        }
    }
    assert_eq!(total, 108);
    assert_eq!(seen.len(), total);
    let other = variant(b"another-deployment-key", &d(None, &[], "en-US", &[]))
        .unwrap()
        .id;
    assert_ne!(other, variant(KEY, &d(None, &[], "en-US", &[])).unwrap().id);
}

#[test]
fn tenant_absence_differs_from_an_empty_looking_tenant() {
    let none = variant(KEY, &d(None, &[], "en-US", &[])).unwrap();
    assert!(variant(KEY, &d(Some(""), &[], "en-US", &[])).is_err());
    let named = variant(KEY, &d(Some("null"), &[], "en-US", &[])).unwrap();
    assert_ne!(none.id, named.id);
}

#[test]
fn invalid_descriptors_fail_closed() {
    let bad = |x: VariantDescriptor| variant(KEY, &x).unwrap_err().code;
    assert_eq!(bad(d(None, &[], "english", &[])), "identity.bad_locale");
    assert_eq!(bad(d(None, &[], "e", &[])), "identity.bad_locale");
    assert_eq!(bad(d(None, &[], "en--US", &[])), "identity.bad_locale");
    assert_eq!(bad(d(None, &["a\u{202e}b"], "en-US", &[])), "ir.bad_text");
    assert_eq!(bad(d(None, &[""], "en-US", &[])), "identity.empty");
    assert_eq!(
        bad(d(None, &[], "en-US", &[("beta", "on"), ("beta", "off")])),
        "identity.conflicting_flag"
    );
    let mut x = d(None, &[], "en-US", &[]);
    x.app_id = "noprovider".into();
    assert_eq!(bad(x), "identity.bad_app_id");
}

#[test]
fn private_cache_state_is_never_shared_across_principals_or_variants() {
    let v1 = variant(KEY, &d(Some("acme"), &["admin"], "en-US", &[]))
        .unwrap()
        .id;
    let v2 = variant(KEY, &d(Some("globex"), &["admin"], "en-US", &[]))
        .unwrap()
        .id;
    let k = |vis, v: &str, p: &str, kind: &str| cache_key(KEY, vis, v, p, kind, "sha256:ab");
    assert_ne!(
        k(Visibility::Private, &v1, "prin_a", "semantic"),
        k(Visibility::Private, &v1, "prin_b", "semantic")
    );
    assert_ne!(
        k(Visibility::Private, &v1, "prin_a", "semantic"),
        k(Visibility::Private, &v2, "prin_a", "semantic")
    );
    assert_eq!(
        k(Visibility::Private, &v1, "prin_a", "semantic"),
        k(Visibility::Private, &v1, "prin_a", "semantic")
    );
    assert_eq!(
        k(Visibility::Public, &v1, "prin_a", "semantic"),
        k(Visibility::Public, &v1, "prin_b", "semantic")
    );
    assert_ne!(
        k(Visibility::Public, &v1, "prin_a", "semantic"),
        k(Visibility::Public, &v2, "prin_a", "semantic")
    );
    assert_ne!(
        k(Visibility::Public, &v1, "prin_a", "semantic"),
        k(Visibility::Private, &v1, "prin_a", "semantic")
    );
    assert_ne!(
        k(Visibility::Public, &v1, "p", "semantic"),
        k(Visibility::Public, &v1, "p", "operation")
    );
}

#[test]
fn cache_key_fields_cannot_be_shifted_across_boundaries() {
    let a = cache_key(KEY, Visibility::Private, "var_ab", "cd", "k", "x");
    let b = cache_key(KEY, Visibility::Private, "var_a", "bcd", "k", "x");
    assert_ne!(a, b);
}
