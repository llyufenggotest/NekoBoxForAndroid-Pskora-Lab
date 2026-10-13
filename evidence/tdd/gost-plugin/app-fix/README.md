# gost app review fix evidence

- KSP/resource compilation passes on parent `744197c0`.
- Full Kotlin/test execution remains blocked by the parent core115 source snapshot: `local_resolved_linux.go` does not build an exact-head `libcore.aar`; older available AARs fail the native identity gate or lack current diagnostics APIs.
- `compile-blocked.log` records the API mismatch when using the closest locally rebuilt AAR.
