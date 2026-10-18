//! Maturity overlay: claims above `candidate` live here, each with its witnesses.

use crate::json::{Obj, Res, array, err};
use crate::model::Maturity;
use crate::register::{Evidence, Recognition, Registration};
use vsf_canon::Value;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Composition {
    pub id: String,
    pub implementation: String,
    pub reused_by: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Status {
    pub registrations: Vec<Registration>,
    pub compositions: Vec<Composition>,
}

fn opt_str(o: &mut Obj, k: &str) -> Res<Option<String>> {
    match o.opt(k) {
        Some(v) => Ok(Some(crate::json::text(v, k)?)),
        None => Ok(None),
    }
}

impl Status {
    pub fn decode(v: &Value) -> Res<Status> {
        let mut o = Obj::new(v, "")?;
        if o.str("schema")? != "vsf-primitive-status-v1" {
            return Err(err("registry.bad_schema", "status"));
        }
        let mut registrations = Vec::new();
        for (i, x) in array(o.req("registrations")?, "/registrations")?
            .iter()
            .enumerate()
        {
            let p = format!("/registrations/{i}");
            let mut r = Obj::new(x, &p)?;
            let recognition = match r.opt("recognition") {
                Some(rv) => {
                    let mut q = Obj::new(rv, &format!("{p}/recognition"))?;
                    let rec = Recognition {
                        produces: q.str("produces")?,
                        signature: q.strs("signature")?,
                        excludes: q.strs("excludes")?,
                        precedence: u32::try_from(q.int("precedence")?)
                            .map_err(|_| err("registry.wrong_type", "precedence"))?,
                    };
                    q.finish()?;
                    Some(rec)
                }
                None => None,
            };
            let mut evidence = Vec::new();
            for (j, ev) in array(r.req("evidence")?, &format!("{p}/evidence"))?
                .iter()
                .enumerate()
            {
                let mut e = Obj::new(ev, &format!("{p}/evidence/{j}"))?;
                let item = Evidence {
                    id: e.str("id")?,
                    independent: e
                        .opt("independent")
                        .is_some_and(|b| matches!(b, Value::Bool(true))),
                    kind: e.str("kind")?,
                    polarity: opt_str(&mut e, "polarity")?,
                };
                e.finish()?;
                evidence.push(item);
            }
            let reg = Registration {
                evidence,
                execution: r.strs("execution")?,
                id: r.str("id")?,
                implementation: opt_str(&mut r, "implementation")?,
                maturity: Maturity::parse(&r.str("maturity")?)?,
                needs_capabilities: r.strs("needs_capabilities")?,
                recognition,
            };
            r.finish()?;
            registrations.push(reg);
        }
        let mut compositions = Vec::new();
        for (i, x) in array(o.req("compositions")?, "/compositions")?
            .iter()
            .enumerate()
        {
            let mut c = Obj::new(x, &format!("/compositions/{i}"))?;
            compositions.push(Composition {
                id: c.str("id")?,
                implementation: c.str("implementation")?,
                reused_by: c.strs("reused_by")?,
            });
            c.finish()?;
        }
        o.finish()?;
        Ok(Status {
            registrations,
            compositions,
        })
    }
}
