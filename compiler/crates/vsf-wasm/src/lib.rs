#![cfg_attr(test, allow(clippy::unwrap_used, clippy::expect_used, clippy::panic))]
#![allow(unsafe_code)]

use std::cell::RefCell;

#[cfg(not(feature = "inject-incompatible-abi"))]
pub const WASM_ABI_VERSION: u32 = 1;
#[cfg(feature = "inject-incompatible-abi")]
pub const WASM_ABI_VERSION: u32 = 1000;

#[derive(Default)]
struct Slot {
    output: Vec<u8>,
    error: Vec<u8>,
}

thread_local! {
    static SLOT: RefCell<Slot> = RefCell::new(Slot::default());
}

#[unsafe(no_mangle)]
pub extern "C" fn vsf_abi_version() -> u32 {
    WASM_ABI_VERSION
}

#[unsafe(no_mangle)]
pub extern "C" fn vsf_alloc(len: u32) -> *mut u8 {
    if len as usize > vsf_canon::MAX_INPUT_BYTES {
        return std::ptr::null_mut();
    }
    let mut buf = Vec::<u8>::with_capacity(len as usize);
    let ptr = buf.as_mut_ptr();
    std::mem::forget(buf);
    ptr
}

/// # Safety
/// `ptr` and `len` must come from a prior `vsf_alloc(len)` and not be freed twice.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn vsf_free(ptr: *mut u8, len: u32) {
    if !ptr.is_null() {
        drop(unsafe { Vec::from_raw_parts(ptr, 0, len as usize) });
    }
}

/// # Safety
/// `ptr` must point to `len` initialized bytes allocated by `vsf_alloc`.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn vsf_compile(ptr: *const u8, len: u32) -> u32 {
    if ptr.is_null() {
        return 1;
    }
    let input = unsafe { std::slice::from_raw_parts(ptr, len as usize) };
    let result = vsf_fabric::compile_envelope(input);
    SLOT.with(|slot| {
        let mut slot = slot.borrow_mut();
        match result {
            Ok(bytes) => {
                slot.output = bytes;
                slot.error.clear();
                0
            }
            Err(err) => {
                slot.output.clear();
                slot.error = err.code().as_bytes().to_vec();
                2
            }
        }
    })
}

#[unsafe(no_mangle)]
pub extern "C" fn vsf_output_ptr() -> *const u8 {
    SLOT.with(|slot| slot.borrow().output.as_ptr())
}

#[unsafe(no_mangle)]
pub extern "C" fn vsf_output_len() -> u32 {
    SLOT.with(|slot| slot.borrow().output.len() as u32)
}

#[unsafe(no_mangle)]
pub extern "C" fn vsf_error_ptr() -> *const u8 {
    SLOT.with(|slot| slot.borrow().error.as_ptr())
}

#[unsafe(no_mangle)]
pub extern "C" fn vsf_error_len() -> u32 {
    SLOT.with(|slot| slot.borrow().error.len() as u32)
}

#[cfg(all(test, not(feature = "inject-incompatible-abi")))]
mod tests {
    use super::*;

    fn run(input: &[u8]) -> (u32, Vec<u8>, Vec<u8>) {
        let ptr = vsf_alloc(input.len() as u32);
        assert!(!ptr.is_null());
        unsafe {
            std::ptr::copy_nonoverlapping(input.as_ptr(), ptr, input.len());
            let status = vsf_compile(ptr, input.len() as u32);
            vsf_free(ptr, input.len() as u32);
            let out =
                std::slice::from_raw_parts(vsf_output_ptr(), vsf_output_len() as usize).to_vec();
            let err =
                std::slice::from_raw_parts(vsf_error_ptr(), vsf_error_len() as usize).to_vec();
            (status, out, err)
        }
    }

    #[test]
    fn abi_matches_native_compile_exactly() {
        assert_eq!(vsf_abi_version(), 1);
        let input = vsf_fixtures::generate_envelope_bytes(42);
        let (status, out, _) = run(&input);
        assert_eq!(status, 0);
        assert_eq!(out, vsf_fabric::compile_envelope(&input).unwrap());
    }

    #[test]
    fn errors_surface_stable_codes_and_clear_output() {
        let (status, out, err) = run(b"{\"a\":1.5}");
        assert_eq!((status, out.len()), (2, 0));
        assert_eq!(err, b"canon.non_integer");
    }

    #[test]
    fn alloc_rejects_oversized_requests() {
        assert!(vsf_alloc(u32::MAX).is_null());
        assert_eq!(unsafe { vsf_compile(std::ptr::null(), 0) }, 1);
    }
}
