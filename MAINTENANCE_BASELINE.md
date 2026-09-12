# Validated 250 ms maintenance baseline

This branch promotes the device-tested d8cc candidate as the long-term maintenance line.

- Candidate APK SHA-256: `d8cc10463bc14a99184c9c80bfc2d54232203d9d280cc5f3647ac2069d8e2765`
- Architecture/signing: arm64-v8a only, unsigned
- Core: private sing-box 1.15 migration with Pure and the complete private-protocol set
- Recovery semantics: validated path settles at 250 ms; fallback remains 1500 ms; underlying networks are not forced
- UI: dual-column layout, transparent node backgrounds, night resources, and the validated crash fix
- Device evidence: three user test rounds reported faster recovery; the 250 ms trace hit in two of three rounds

The historical APK is evidence for promotion, not a committed build input. GitHub Actions rebuilds the native AAR from the pinned `core115-source` tree, passes it as an artifact to the Android job, runs Debug and Release tests plus native/provenance/Pure/geo/UI gates, and emits only the unsigned arm64 APK and its SHA-256 file.

## Open acceptance boundary

The rebuilt CI artifact must still be installed and exercised on a physical device before replacing the d8cc binary as the device-accepted artifact. In particular, repeat network handover/recovery and confirm the 250 ms trace behavior.
