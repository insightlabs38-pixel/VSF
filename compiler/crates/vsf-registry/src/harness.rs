//! Loads the whole primitive package and checks every claim against its witnesses.

use crate::compose::run_fixture;
use crate::fixture::{self, Fixture};
use crate::graph;
use crate::json::{Error, err};
use crate::model::{Maturity, Registry, load};
use crate::policy::Policy;
use crate::register::{Registrar, Registration};
use crate::status::Status;
use std::collections::{BTreeMap, BTreeSet};
use std::path::{Path, PathBuf};

pub struct Package {
    pub root: PathBuf,
    pub registry: Registry,
    pub policy: Policy,
    pub status: Status,
    pub fixtures: Vec<Fixture>,
    pub registered: BTreeMap<String, Registration>,
}

fn read(root: &Path, rel: &str) -> Result<Vec<u8>, Error> {
    std::fs::read(root.join(rel)).map_err(|e| err("package.io", format!("{rel}: {e}")))
}

pub const REGISTRY: &str = "registry/primitives/registry.v1.json";
pub const POLICY: &str = "registry/primitives/policy.v1.json";
pub const STATUS: &str = "registry/primitives/status.v1.json";
pub const CATALOG: &str = "registry/candidates/catalog.v0.json";
pub const FIXTURES: &str = "fixtures/primitives";

/// Loads and registers everything; any inconsistency is an error naming the primitive.
pub fn load_package(root: &Path) -> Result<Package, Error> {
    let registry = Registry::decode(&load(&read(root, REGISTRY)?)?)?;
    let policy = Policy::decode(&load(&read(root, POLICY)?)?)?;
    let status = Status::decode(&load(&read(root, STATUS)?)?)?;
    let fixtures = fixture::load_dir(&root.join(FIXTURES))?;
    graph::verify(&registry).map_err(|e| err(e[0].code(), e[0].describe()))?;
    let known: BTreeSet<String> = fixtures.iter().map(|f| f.id.clone()).collect();
    let mut registrar = Registrar::new(&registry, &policy, known);
    for r in &status.registrations {
        registrar
            .register(r.clone())
            .map_err(|e| err(e.code(), format!("{e:?}")))?;
    }
    let registered = registrar.registered().clone();
    let pkg = Package {
        root: root.to_path_buf(),
        registry,
        policy,
        status,
        fixtures,
        registered,
    };
    pkg.check_witnesses()?;
    Ok(pkg)
}

impl Package {
    /// Fixtures named as witnesses must pass and carry the polarity claimed; files must exist.
    fn check_witnesses(&self) -> Result<(), Error> {
        let by_id: BTreeMap<&str, &Fixture> =
            self.fixtures.iter().map(|f| (f.id.as_str(), f)).collect();
        for r in self.registered.values() {
            for e in &r.evidence {
                if e.kind == "fixture" {
                    let f = by_id
                        .get(e.id.as_str())
                        .ok_or_else(|| err("witness.unknown_fixture", e.id.clone()))?;
                    if Some(f.polarity.as_str()) != e.polarity.as_deref() {
                        return Err(err(
                            "witness.polarity_mismatch",
                            format!("{} claims {:?} for {}", r.id, e.polarity, e.id),
                        ));
                    }
                    run_fixture(f, &self.registered).map_err(|m| err("witness.failed", m))?;
                } else if e.kind == "spec-document" && !self.root.join(&e.id).is_file() {
                    return Err(err("witness.missing_file", e.id.clone()));
                }
            }
            if let Some(path) = &r.implementation
                && !self.root.join(path).is_file()
            {
                return Err(err("witness.missing_file", path.clone()));
            }
        }
        for c in &self.status.compositions {
            if !self.root.join(&c.implementation).is_file() {
                return Err(err("witness.missing_file", c.implementation.clone()));
            }
            for id in &c.reused_by {
                if self
                    .registered
                    .get(id)
                    .and_then(|r| r.implementation.as_ref())
                    != Some(&c.implementation)
                {
                    return Err(err(
                        "witness.composition_not_shared",
                        format!("{id} does not use {}", c.id),
                    ));
                }
            }
        }
        Ok(())
    }

    pub fn effective_maturity(&self, id: &str) -> Maturity {
        self.registered.get(id).map_or_else(
            || {
                self.registry
                    .entries
                    .iter()
                    .find(|e| e.id == id)
                    .map_or(Maturity::Candidate, |e| e.maturity)
            },
            |r| r.maturity,
        )
    }
}
