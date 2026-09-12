"""Run actual Kotlin model without invoking Gradle (uses cached compiler jars)."""
from pathlib import Path
import subprocess, os, tempfile
root = Path(__file__).resolve().parents[2]
home = Path.home()
java = home / 'tools/jdk-17.0.20.1+1/bin/java.exe'
lib = next((home / '.gradle/wrapper/dists').glob('gradle-8.10.2-bin/*/gradle-8.10.2/lib'))
cp = os.pathsep.join(str(p) for p in lib.glob('*.jar') if p.name.startswith(('kotlin-', 'trove4j-', 'annotations-')))
stdlib = next(lib.glob('kotlin-stdlib-*.jar'))
with tempfile.TemporaryDirectory(prefix='preferred-model-') as d:
    out = str(Path(d) / 'classes')
    subprocess.run([str(java), '-cp', cp, 'org.jetbrains.kotlin.cli.jvm.K2JVMCompiler', '-no-stdlib', '-no-reflect', '-classpath', str(stdlib), '-d', out,
        str(root/'app/src/main/java/moe/matsuri/nb4a/proxy/config/PreferredGroupSpec.kt'), str(Path(__file__).with_name('PreferredModelTest.kt'))], check=True)
    subprocess.run([str(java), '-cp', out + os.pathsep + str(stdlib), 'PreferredModelTestKt'], check=True)
