use vsf_ir::ValueType;

fn is_integer(s: &str) -> bool {
    let d = s.strip_prefix('-').unwrap_or(s);
    !d.is_empty() && d.len() <= 15 && d.bytes().all(|b| b.is_ascii_digit())
}

fn is_decimal(s: &str) -> bool {
    let d = s.strip_prefix('-').unwrap_or(s);
    match d.split_once('.') {
        Some((a, b)) => {
            !a.is_empty()
                && !b.is_empty()
                && a.len() + b.len() <= 15
                && a.bytes().all(|c| c.is_ascii_digit())
                && b.bytes().all(|c| c.is_ascii_digit())
        }
        None => false,
    }
}

fn is_date(s: &str) -> bool {
    let b = s.as_bytes();
    if b.len() != 10 || b[4] != b'-' || b[7] != b'-' {
        return false;
    }
    let digits = |r: std::ops::Range<usize>| b[r].iter().all(u8::is_ascii_digit);
    if !(digits(0..4) && digits(5..7) && digits(8..10)) {
        return false;
    }
    let num = |r: std::ops::Range<usize>| s[r].parse::<u32>().unwrap_or(0);
    (1..=12).contains(&num(5..7)) && (1..=31).contains(&num(8..10))
}

/// A column takes a narrower type only when every non-empty cell has that shape; otherwise it stays text.
pub fn column_type(cells: &[&str]) -> ValueType {
    let filled: Vec<&str> = cells.iter().copied().filter(|c| !c.is_empty()).collect();
    if filled.is_empty() {
        return ValueType::Text;
    }
    if filled.iter().all(|c| is_integer(c)) {
        ValueType::Integer
    } else if filled.iter().all(|c| is_integer(c) || is_decimal(c)) {
        ValueType::Decimal
    } else if filled.iter().all(|c| is_date(c)) {
        ValueType::Date
    } else {
        ValueType::Text
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn shapes_are_exact_not_guessed() {
        assert_eq!(column_type(&["1", "-20", ""]), ValueType::Integer);
        assert_eq!(column_type(&["12.50", "8", "99.99"]), ValueType::Decimal);
        assert_eq!(column_type(&["2026-01-31", "2026-12-01"]), ValueType::Date);
        assert_eq!(column_type(&["2026-13-01"]), ValueType::Text);
        assert_eq!(column_type(&["1,234.50"]), ValueType::Text);
        assert_eq!(column_type(&["12", "ab"]), ValueType::Text);
        assert_eq!(column_type(&["", ""]), ValueType::Text);
        assert_eq!(column_type(&["1234567890123456"]), ValueType::Text);
        assert_eq!(column_type(&["1e5"]), ValueType::Text);
        assert_eq!(column_type(&["١٢"]), ValueType::Text);
    }
}
