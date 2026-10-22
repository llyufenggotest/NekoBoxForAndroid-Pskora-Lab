# A01: background UI DTO materialization regression

This gate compiles **all four complete production source files from the current
checkout**: `TrafficLooper`, `TrafficUpdater`, `ServiceNotification`, and `Logs`.
It never substitutes a simplified product body, downloads an AAR, or skips native
provenance verification. Small files in `boundary/` model Android/Room/Binder and
libcore boundaries; they are **not** Android implementations.

## Required CI entrypoint

`app/src/test/java/io/nekohasekai/sagernet/bg/proto/TrafficLooperMaterializationTest.java`
is an ordinary JUnit 4 test, discovered by the existing required
`app:testOssDebugUnitTest` and `app:testOssReleaseUnitTest` tasks. It launches this
compiled-source gate and fails on its exit status. No workflow edits are needed.
Python 3, curl, and JDK 21 are expected (the existing maintenance workflow supplies all three).
`A01_PYTHON` optionally overrides the Python executable. No skip-on-CI switch exists.

Standalone replay (choose a **new** output directory each time):

```sh
python3 tools/positive_a01_gate/run.py --output "$RUNNER_TEMP/a01-standalone"
```

Windows: use `python` and an output path on F:/G: or Hermes scratch. Cache defaults
to `F:/tools/positive-a01-cache` on Windows and `~/.cache/positive-a01` elsewhere;
`--cache` / `A01_CACHE` override it. All outputs, compiler copies, and dependencies
stay outside the checkout by default. `dependencies.json` pins public Maven
Central artifacts by version and SHA-256. Local Gradle cache bytes are accepted
only when their hashes match; otherwise exactly those public URLs are downloaded
and checked. A mismatch fails closed.

## Assertions and evidence

1. Real constructor budget in the original notification-enabled probe: four
   profiles over ten background ticks must construct **zero periodic TrafficData**.
   Sampling remains 100 native boundary queries and final stop persists four rows.
   Foreground still constructs 40 DTOs; screen-off notification delivery, disabled
   sampling/statistics, batching, and owner cancellation are exercised.
2. `rich/PositiveProbe.kt`: original **22 scenarios** including selector/reset,
   duplicate tags, direct-hidden totals, 1001 profiles / batches 500/500/1,
   Connected changes and foreground subscription transitions. Real coroutines
   suspend at broadcast; a second JVM thread uses latches at the native-query
   boundary for unsubscribe/promote/demote/cancel. These controlled interleavings
   do not prove arbitrary mutable-map thread safety.
3. **21 explicit semantic oracles** check rates, totals, initial history, DAO
   values, unique native tag queries and no sampling after stop/cancel. They do
   not merely accept two equally wrong implementations.
4. Optional `--compare-source BASELINE_ROOT` compiles the complete four immutable
   baseline files as well and compares exact event values/order, state and final
   persistence, excluding only intended DTO construction counts. Full decoded
   traces and original command stdout/stderr/exit codes are retained.
5. Optional `--benchmark` (requires baseline) runs the same complete looper load at
   4/128/1001 profiles, recording DTOs, native queries, allocated bytes, thread CPU
   and exact accumulated state. Allocation reduction is asserted, **CPU reduction
   is not**: short Windows/HotSpot measurements can regress due to noise.

The deterministic rich probe changes **only a compiled constant-pool clock
owner** in `TrafficUpdater.class`: `java/lang/System` -> `fixture/Clock`, keeping
`currentTimeMillis ()J`. Both baseline/current receive the same instrumentation;
all source inputs remain exact byte copies. `clock-boundary.json` exposes input
and output hashes. The original constructor-budget probe uses the unmodified
wall clock and no class instrumentation.

Limits: host HotSpot, fake DAO/native/Binder boundaries, virtual ticks and
controlled threads; not ART, actual SQLite durability, real Binder parcel bytes,
JNI/network behavior, Android OS notification eligibility, device energy or
thermal evidence. This gate complements, never replaces, the real native-AAR
provenance and Android build gates. Empty ArrayList and snapshot allocations
remain; this is not a zero-allocation-background claim.
