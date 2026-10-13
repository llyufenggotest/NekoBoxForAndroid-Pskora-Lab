# Maintenance baseline: recovery timing layers

This branch promotes the device-tested d8cc candidate as the long-term maintenance line.

- Candidate APK SHA-256: `d8cc10463bc14a99184c9c80bfc2d54232203d9d280cc5f3647ac2069d8e2765`
- Architecture/signing: arm64-v8a only, unsigned
- Core: private sing-box 1.15 migration with Pure and the complete private-protocol set
- Recovery semantics: the route/network recovery implementation settles a `VALIDATED` transition after 500 ms and uses 1500 ms for fallback; underlying networks are not forced.
- 250 ms evidence layer: the historical candidate's 250 ms observations belong to Android monitor integration timeout/speed reporting and other non-route timing. They do not define the TUN route/network settle constant.
- UI: dual-column layout, transparent node backgrounds, night resources, and the validated crash fix
- Device evidence: three user test rounds reported faster recovery; a 250 ms trace appeared in two rounds, but that observation must not be used to replace the actual 500/1500 ms route recovery constants.

The historical APK is evidence for promotion, not a committed build input. GitHub Actions rebuilds the native AAR from the pinned `core115-source` tree, passes it as an artifact to the Android job, runs Debug and Release tests plus native/provenance/Pure/geo/UI gates, and emits only the unsigned arm64 APK and its SHA-256 file.

## Open acceptance boundary

The rebuilt CI artifact must still be installed and exercised on a physical device before replacing the d8cc binary as the device-accepted artifact. In particular, repeat network handover/recovery and confirm the 500 ms `VALIDATED` and 1500 ms fallback route/network behavior separately from any 250 ms Android monitor integration timing.
