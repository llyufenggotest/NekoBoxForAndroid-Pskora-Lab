#!/usr/bin/env python3
"""Compile complete checkout production sources against explicit host boundaries.

Not an Android/AAR build. Outputs and downloaded tools stay outside the checkout.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import sys
import tempfile
import time
import zipfile

HERE = Path(__file__).resolve().parent
SOURCES = ["app/src/main/java/io/nekohasekai/sagernet/" + p for p in (
    "bg/proto/TrafficLooper.kt", "bg/proto/TrafficUpdater.kt",
    "bg/ServiceNotification.kt", "ktx/Logs.kt")]


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def save(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2), encoding="utf-8")


def rows(result):
    if result["exit"] != 0:
        raise RuntimeError("Probe failed; see command log")
    data = [json.loads(line[7:]) for line in result["stdout"].splitlines()
            if line.startswith("RESULT ")]
    result_rows = {row["name"]: row for row in data}
    assert len(data) == len(result_rows), "duplicate scenario"
    return result_rows


class Gate:
    def __init__(self, args):
        self.args = args
        self.out = args.output.resolve()
        self.out.mkdir(parents=True, exist_ok=False)
        self.commands = []
        lock = json.loads((HERE / "dependencies.json").read_text(encoding="utf-8"))
        self.jars = {}
        cache = args.cache.resolve()
        cache.mkdir(parents=True, exist_ok=True)
        for item in lock:
            filename = item["url"].rsplit("/", 1)[1]
            path = cache / filename
            if not path.exists():
                # Reuse only exact pinned bytes, not whichever compiler Gradle finds.
                gradle = Path(os.environ.get("GRADLE_USER_HOME", "F:/tools/gradle-home"))
                matches = list((gradle / "caches/modules-2/files-2.1").glob(
                    "*/" + item["artifact"] + "/" + item["version"] + "/*/" + filename))
                cached = next((p for p in matches if sha(p) == item["sha256"]), None)
                if cached:
                    data = cached.read_bytes()
                else:
                    download = cache / (filename + ".download-" + str(os.getpid()))
                    try:
                        result = self.run("download-" + item["artifact"], [
                            "curl", "--fail", "--location", "--connect-timeout", "20",
                            "--max-time", "240", "--output", download, item["url"]])
                        if result["exit"] != 0:
                            raise RuntimeError("Pinned public dependency download failed: " + filename)
                        data = download.read_bytes()
                    finally:
                        download.unlink(missing_ok=True)
                assert hashlib.sha256(data).hexdigest() == item["sha256"], filename
                with tempfile.NamedTemporaryFile(dir=cache, prefix=filename + ".", delete=False) as temporary:
                    temporary_path = Path(temporary.name)
                    temporary.write(data)
                try:
                    assert sha(temporary_path) == item["sha256"], filename
                    os.replace(temporary_path, path)
                finally:
                    temporary_path.unlink(missing_ok=True)
            assert sha(path) == item["sha256"], "Dependency hash mismatch: " + str(path)
            self.jars[item["artifact"]] = str(path)
        save(self.out / "toolchain.json", {"dependencies": lock, "java": args.java})
        self.compiler = os.pathsep.join(self.jars[k] for k in (
            "kotlin-compiler-embeddable", "kotlin-stdlib", "kotlin-script-runtime",
            "kotlin-reflect", "trove4j", "annotations"))
        self.cp = [self.jars[k] for k in (
            "kotlin-stdlib", "kotlinx-coroutines-core-jvm", "annotations")]
        self.run("java-version", [args.java, "-version"])
        self.manifest = {}

    def run(self, name, command):
        start = time.monotonic()
        command = [str(s) for s in command]
        try:
            p = subprocess.run(command, capture_output=True, text=True,
                               encoding="utf-8", errors="replace", timeout=300)
            record = {"command": command, "exit": p.returncode,
                      "stdout": p.stdout, "stderr": p.stderr}
        except subprocess.TimeoutExpired as exc:
            record = {"command": command, "exit": 124, "stdout": str(exc.stdout),
                      "stderr": str(exc.stderr), "timeout": True}
        record["seconds"] = time.monotonic() - start
        record["cwd"] = str(Path.cwd())
        save(self.out / (name + ".json"), record)
        self.commands.append({"name": name, "exit": record["exit"]})
        save(self.out / "commands.json", self.commands)
        print(name + ": exit=" + str(record["exit"]), flush=True)
        return record

    def compile(self, label, root, rich):
        directory = self.out / label
        directory.mkdir(exist_ok=True)
        production = []
        for relative in SOURCES:
            path = root / relative
            target = directory / path.name
            shutil.copyfile(path, target)
            assert path.read_bytes() == target.read_bytes()
            production.append(target)
        self.manifest[label] = {str(p): sha(p) for p in production}
        save(self.out / "production-inputs.json", self.manifest)
        boundaries = list((HERE / "boundary").glob("*.kt"))
        if rich:
            boundaries = [p for p in boundaries if p.name not in ("Base.kt", "Data.kt", "Core.kt")]
            probes = list((HERE / "rich").glob("*.kt"))
        else:
            probes = [HERE / "OriginalProbe.kt"]
        jar = directory / ("rich.jar" if rich else "budget.jar")
        result = self.run(label + "-compile", [self.args.java, "-cp", self.compiler,
            "org.jetbrains.kotlin.cli.jvm.K2JVMCompiler", "-no-stdlib", "-no-reflect",
            "-jvm-target", "1.8", "-classpath", os.pathsep.join(self.cp), "-d", jar]
            + production + boundaries + probes)
        if result["exit"] != 0:
            raise RuntimeError("Production fixture compilation failed: " + label)
        if rich:
            jar = self.fixed_clock(jar)
        return jar

    def fixed_clock(self, jar):
        output = jar.with_name("rich-fixed-clock.jar")
        changed = 0
        with zipfile.ZipFile(jar) as src, zipfile.ZipFile(output, "w") as dst:
            for info in src.infolist():
                data = src.read(info.filename)
                if info.filename == "io/nekohasekai/sagernet/bg/proto/TrafficUpdater.class":
                    old, new = b"java/lang/System", b"fixture/Clock"
                    token = struct.pack(">BH", 1, len(old)) + old
                    assert data.count(token) == 1
                    data = data.replace(token, struct.pack(">BH", 1, len(new)) + new)
                    changed += 1
                dst.writestr(info, data)
        assert changed == 1
        save(output.parent / "clock-boundary.json", {
            "input_sha256": sha(jar), "output_sha256": sha(output),
            "class": "TrafficUpdater.class", "source_edited": False,
            "only_change": "CONSTANT_Utf8 owner java/lang/System -> fixture/Clock; currentTimeMillis ()J"})
        return output

    def execute(self, name, jar, main, *arguments):
        return self.run(name, [self.args.java, "-Xms128m", "-Xmx512m", "-cp",
            os.pathsep.join([str(jar)] + self.cp), main] + list(arguments))

    def verify(self):
        original = self.compile("current-budget", self.args.source_root, False)
        budget = self.execute("budget-current", original, "OriginalProbeKt")
        if budget["exit"] != 0:
            print(budget["stderr"], file=sys.stderr)
            raise RuntimeError("A01 no-consumer budget failed")
        current = self.compile("current-rich", self.args.source_root, True)
        actual = rows(self.execute("lifecycle-current", current, "PositiveProbeKt", "lifecycle"))
        assert len(actual) == 22, "expected original 22 lifecycle scenarios"
        checks = semantic_oracles(actual)
        save(self.out / "semantic-oracles.json", {"count": len(checks), "checks": checks})
        baseline = None
        if self.args.compare_source:
            baseline = self.compile("baseline-rich", self.args.compare_source, True)
            expected = rows(self.execute("lifecycle-baseline", baseline, "PositiveProbeKt", "lifecycle"))
            assert expected.keys() == actual.keys()
            compared = []
            for name, old in expected.items():
                new = actual[name]
                assert old.keys() == new.keys()
                for key, value in old.items():
                    if key != "dto":
                        assert value == new[key], (name, key)
                for key in ("trace_b64", "states_b64", "final_b64"):
                    if key in old:
                        for mode, row in (("baseline", old), ("current", new)):
                            target = self.out / "traces" / (name + "-" + mode + "-" + key + ".txt")
                            target.parent.mkdir(exist_ok=True)
                            target.write_bytes(base64.b64decode(row[key]))
                compared.append({"name": name, "semantics_equal": True,
                                 "baseline_dto": old.get("dto"), "current_dto": new.get("dto")})
            assert expected["foreground"]["dto"] == 40 and actual["foreground"]["dto"] == 0
            assert expected["foreground_actual"]["dto"] == actual["foreground_actual"]["dto"] == 40
            assert expected["foreground_1001"]["dto"] == actual["foreground_1001"]["dto"] == 10010
            save(self.out / "lifecycle-comparison.json", {"scenario_count": len(compared), "checks": compared})
        if self.args.benchmark:
            assert baseline is not None, "--benchmark requires --compare-source"
            old = rows(self.execute("benchmark-baseline", baseline, "PositiveProbeKt", "benchmark"))
            new = rows(self.execute("benchmark-current", current, "PositiveProbeKt", "benchmark"))
            assert old.keys() == new.keys()
            comparison = []
            for name, x in old.items():
                y = new[name]
                assert x["state_b64"] == y["state_b64"] and x["queries"] == y["queries"]
                assert y["dto"] == 0 and x["dto"] == x["profiles"] * x["iterations"]
                assert y["allocated_bytes_per_tick"] < x["allocated_bytes_per_tick"]
                comparison.append({"name": name, "baseline": x, "current": y,
                    "state_equal": True, "allocation_reduced": True,
                    "cpu_is_noisy_host_measurement_not_ART": True})
            save(self.out / "benchmark-comparison.json", comparison)
        save(self.out / "results.json", {"passed": True, "lifecycle_scenarios": len(actual),
            "semantic_oracles": len(checks), "full_four_production_sources": True,
            "baseline_compared": baseline is not None, "android_build": False,
            "clock_boundary_instrumented": True, "benchmark": self.args.benchmark})


def semantic_oracles(data):
    checks = []
    def check(name, condition):
        assert condition, name
        checks.append({"gate": name, "passed": True})
    def trace(name):
        return base64.b64decode(data[name]["trace_b64"]).decode().splitlines()
    check("native_background_sampling_not_skipped", data["foreground"]["queries"] == 100 and data["foreground"]["stop_dao_writes"] == 4)
    check("background_periodic_dto_zero", data["foreground"]["dto"] == data["background_screen_off"]["dto"] == 0)
    check("idle_no_delta_no_periodic_dto", data["idle"]["dto"] == 0)
    check("stats_disabled_not_persisted", data["stats_disabled"]["dto"] == 0 and data["stats_disabled"]["stop_dao_writes"] == 0)
    check("sampling_disabled_zero_work", data["sampling_disabled"]["queries"] == data["sampling_disabled"]["dto"] == 0)
    fg = trace("foreground_actual")
    speed = [x for x in fg if x.startswith("ui:s:")]
    check("foreground_exact_total_and_rate", speed[0] == "ui:s:0,0,0,0,512,512" and speed[-1] == "ui:s:51200,51200,12800,12800,5120,5120")
    direct = [x for x in trace("direct_hidden") if x.startswith("ui:s:")]
    check("direct_hidden_totals_unaffected", direct[-1] == "ui:s:51200,51200,0,0,5120,5120")
    check("final_stop_updates_historical_entities", "dao:1,2280,3280" in fg)
    sizes = [len(x.split(":", 2)[2].split(";")) for x in trace("foreground_1001") if x.startswith("ui:t:")]
    check("batch500_order_and_final_stop", sizes == [500, 500, 1] * 11)
    check("duplicate_tag_one_native_query_each_direction", data["duplicate_tags"]["queries"] == 40)
    last = [x for x in trace("duplicate_tags") if x.startswith("ui:s:")][-1]
    check("duplicate_tag_totals_not_multiplied", last.endswith(",1280,1280"))
    selector = trace("selector_reset")
    check("selector_reset_persistence", data["selector_reset"]["resets"] == 1 and "reset:2" in selector and any(x.startswith("dao:1,") for x in selector) and any(x.startswith("dao:2,") for x in selector))
    for name, row in data.items():
        if name.startswith(("suspended_", "threaded_")) or name == "native_query_cancel":
            check(name + "_no_late_sampling", row.get("post_stop_queries", row.get("post_cancel_queries")) == 0)
    assert len(checks) == 21
    return checks


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-root", type=Path, default=HERE.parents[1])
    parser.add_argument("--compare-source", type=Path)
    parser.add_argument("--output", type=Path, required=True, help="new directory; never overwrites prior evidence")
    parser.add_argument("--cache", type=Path, default=Path(os.environ.get("A01_CACHE", "F:/tools/positive-a01-cache" if os.name == "nt" else "~/.cache/positive-a01")).expanduser())
    parser.add_argument("--java", default="java")
    parser.add_argument("--benchmark", action="store_true")
    args = parser.parse_args()
    gate = Gate(args)
    try:
        gate.verify()
    except Exception as exc:
        save(gate.out / "results.json", {"passed": False, "error": str(exc), "android_build": False})
        raise
    print("A01 PASS: budget, 22 lifecycle scenarios, 21 semantic oracles", flush=True)


if __name__ == "__main__":
    main()
