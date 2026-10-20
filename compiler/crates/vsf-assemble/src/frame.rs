use vsf_canon::Value;
use vsf_ir::{Origin, SourceVariantId};

const MAX_HEADER: usize = 4096;

#[derive(Debug, Clone)]
pub struct Params {
    pub origin: String,
    pub variant_id: String,
    pub observed_at: i64,
    pub epoch: i64,
}

pub fn parse(input: &[u8]) -> Result<(Params, &[u8]), &'static str> {
    let head_len = input
        .get(..4)
        .and_then(|b| <[u8; 4]>::try_from(b).ok())
        .map(u32::from_be_bytes)
        .ok_or("assemble.bad_frame")? as usize;
    if head_len == 0 || head_len > MAX_HEADER {
        return Err("assemble.bad_frame");
    }
    let rest = input.get(4..).ok_or("assemble.bad_frame")?;
    let (head, snapshot) = rest
        .split_at_checked(head_len)
        .ok_or("assemble.bad_frame")?;
    let header = vsf_canon::parse(head).map_err(|_| "assemble.bad_header")?;
    let text = |k: &str| {
        header
            .get(k)
            .and_then(Value::as_str)
            .ok_or("assemble.bad_header")
    };
    let number = |k: &str| match header.get(k).and_then(Value::as_int) {
        Some(n) if n >= 0 => Ok(n),
        _ => Err("assemble.bad_header"),
    };
    let origin = text("origin")?;
    Origin::new(origin).map_err(|_| "assemble.bad_origin")?;
    let variant = text("variant_id")?;
    SourceVariantId::new(variant).map_err(|_| "assemble.bad_variant")?;
    Ok((
        Params {
            origin: origin.to_string(),
            variant_id: variant.to_string(),
            observed_at: number("observed_at")?,
            epoch: number("epoch")?,
        },
        snapshot,
    ))
}
