# Local native recovery — Actions 33967191242

Scope: isolated F:/pskora-lab/nb4a-lab only. No remote operations, Gradle execution, UI/routing edits, device or live-network verification.

## Recovery

No matching original AAR was found in the lab/temp or checked local native artifact locations. The other local AAR in F:/nb4a1.4.2-mod-12 is also missing Juzi and was not modified. A broader F: search timed out; this is not a claim of exhaustive disk discovery.

Recovered the exact arm64 libgojni.so from the user-supplied original Actions APK, retaining the existing Java bridge only after checking it against the original APK DEX:

- 31/31 bridge classes found; every existing class field/method descriptor and static/native flag matched (excluding class initializers); no mismatches.
- 86 Java native methods; old/new native ELF JNI exports identical (86).
- This is static interface compatibility, not Android runtime compatibility proof.
- Non-arm64 native entries were removed rather than shipping stale cores on other ABIs. This recovery is arm64-only.
- The recovered AAR is a locally assembled recovery artifact, not the original Actions AAR.

Hashes and source identity: `native-baseline.json`.
Backup: `F:/pskora-lab/evidence/libcore-before-juzi-3f76228f4b39ecf5504d08ebcd903616f4123235f1542c454440fceb54d6d0c4.aar`.
Detailed JNI inventory and comparison: `F:/pskora-lab/evidence/native-jni-compatibility.json`; reproducible comparison tool: `compare_native_bridge.py` in that directory (requires androguard and pyelftools; reads the pre-recovery AAR when repeating).

## Protocol evidence

| Private mode | Restored binary evidence | Verification level |
|---|---|---|
| Juzi VLESS | `#juzi` and `hello_pidun`, each present once | Pinned sing-vmess local tests passed: suffix isolation/casing, known HMAC fixture, eight-byte request insertion, standard/X365 non-regression |
| X365 VLESS | `#x365` once | Above local non-regression test passed |
| Shanlian VLESS | `#sl` once | Static marker; pinned sing-box outbound strips only this mode at its layer |
| Fastup Trojan | `fastup` once | Static marker; pinned sing-box has `#fastup` derivation and h2mux option path |
| TunNet | `libcore/tunnetcontrol` 78 occurrences | Static package/symbol evidence; bridge methods match original DEX |
| Oppa | `libcore/protocol/oppa` 35 occurrences | Static package/symbol evidence; local registry present |

The exact restored SO hash matches the original APK, so all original core bytes—not just those markers—are preserved. Other dependency package evidence includes sing-shadowsocks2 (398 occurrences) and sing-anytls (289); package presence is not proof of custom semantics. No live protocol claims are made. No new Fastup/TunNet/Oppa/SL execution tests were run in this task.

## Gates and actual results

- `python buildScript/lib/core/test_verify_native.py -v`: six tests passed, including old AAR, missing Juzi marker, mixed stale ABI, APK/AAR mismatch rejection. These are explicitly synthetic verifier fixtures, not protocol traffic.
- `python buildScript/lib/core/verify_native.py --apk <original APK>`: passed against installed recovered AAR.
- Verifying the real backup AAR: rejected as unattested.
- Verifying existing local release APK: rejected for APK/AAR native mismatch. Existing APK is still stale; it was not rebuilt or delivered.
- `go test ./vless -v` at local sing-vmess checkout 5537b1238054eb9f755082df6a0c936baf77df90: four tests passed.
- `git diff --check`: passed (unrelated existing line-ending warning only).

Gradle preBuild now calls the fail-closed verifier; per-variant package finalizers check actual APK core identity even on cached/up-to-date packaging. **The Gradle wiring was not executed, per instruction**, and must be exercised by the parent build before claiming build completion. Python can be overridden with NATIVE_VERIFY_PYTHON.

Native build script now also runs sing-vmess VLESS tests. The lock intentionally rejects new AARs until explicit source/JNI/protocol attestation is refreshed; do not auto-update the lock from arbitrary cached binaries. This strict recovery lock is not a general reproducible-source builder.
