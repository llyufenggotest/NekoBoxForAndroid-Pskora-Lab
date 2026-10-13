# Alpha.12 source lineage

The private native tree is **based on** upstream sing-box tag `v1.15.0-alpha.12`; it is not claimed to be the same commit as that tag.

Lineage is proved by `scripts/lifecycle_v4_gate.go`, which compares the upstream tag and the private tree with a path-by-path lifecycle matrix. The committed migration evidence records each compared path and its structural result. Provenance for a produced AAR is separate: `native-baseline.json` binds the exact clean private source commit/tree, deterministic tracked-source snapshot and manifest, AAR, `classes.jar`, and every embedded `libgojni.so`.

Release procedure:

1. Commit and test source changes; the worktree must be clean.
2. Build the AAR from that exact source commit (`built_source_head`).
3. Generate the source snapshot/manifest and artifact attestation.
4. Commit those outputs as the single direct child attestation commit.
5. Verify that `HEAD^` is exactly `built_source_head`, then cross-check the source tree/manifest/snapshot, AAR, `classes.jar`, and every embedded native library.

This two-commit model avoids the impossible self-reference of requiring an AAR to contain the hash of the commit that itself stores the AAR hash.
