@file:Suppress("UnstableApiUsage")

plugins {
    id("com.android.application")
    id("kotlin-android")
    id("com.google.devtools.ksp")
    id("kotlin-parcelize")
}

setupApp()

val verifyBundledGeoAssets by tasks.registering {
    doLast {
        for (name in listOf("geoip", "geosite")) {
            for (suffix in listOf("db.xz", "version.txt")) {
                val asset = file("src/main/assets/sing-box/$name.$suffix")
                check(asset.isFile && asset.length() > 0) { "Missing bundled rules: $asset. Run buildScript/lib/assets.sh" }
            }
        }
    }
}
val nativeVerifier = rootProject.file("buildScript/lib/core/verify_native.py")
val nativePython = providers.environmentVariable("NATIVE_VERIFY_PYTHON").orElse(
    if (System.getProperty("os.name").startsWith("Windows")) "python" else "python3"
)
val verifyBundledNativeCore by tasks.registering {
    // Always run, including after cache restore; the ignored AAR is not source provenance.
    doLast {
        exec { commandLine(nativePython.get(), nativeVerifier.absolutePath) }
    }
}
tasks.named("preBuild").configure {
    dependsOn(verifyBundledGeoAssets, verifyBundledNativeCore)
}
// A separate always-run finalizer also inspects package outputs restored from cache.
android.applicationVariants.all {
    val variantSuffix = name.replaceFirstChar { it.uppercaseChar() }
    val apkDirectory = layout.buildDirectory.dir("outputs/apk/$dirName")
    val nativeCheck = tasks.register("verifyPackagedNative$variantSuffix") {
        doLast {
            val apks = apkDirectory.get().asFile.walkTopDown().filter { it.isFile && it.extension == "apk" }.toList()
            check(apks.isNotEmpty()) { "No APK available for native identity verification: $apkDirectory" }
            apks.forEach { apk ->
                exec { commandLine(nativePython.get(), nativeVerifier.absolutePath, "--apk", apk.absolutePath) }
            }
        }
    }
    tasks.matching { it.name == "package$variantSuffix" }.configureEach { finalizedBy(nativeCheck) }
}

android {
    compileOptions {
        isCoreLibraryDesugaringEnabled = true
    }
    ksp {
        arg("room.incremental", "true")
        arg("room.schemaLocation", "$projectDir/schemas")
    }
    bundle {
        language {
            enableSplit = false
        }
    }
    buildFeatures {
        buildConfig = true
        viewBinding = true
        aidl = true
    }
    namespace = "io.nekohasekai.sagernet"
    packaging {
        jniLibs {
            useLegacyPackaging = true
            keepDebugSymbols += "**/libgojni.so"
        }
    }
    androidResources {
        generateLocaleConfig = true
    }
}

dependencies {

    implementation(fileTree("libs"))

    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.6.4")
    implementation("androidx.core:core-ktx:1.9.0")
    implementation("androidx.recyclerview:recyclerview:1.3.0")
    implementation("androidx.activity:activity-ktx:1.10.1")
    implementation("androidx.fragment:fragment-ktx:1.5.6")
    implementation("androidx.browser:browser:1.5.0")
    implementation("androidx.swiperefreshlayout:swiperefreshlayout:1.1.0")
    implementation("androidx.constraintlayout:constraintlayout:2.1.4")
    implementation("androidx.navigation:navigation-fragment-ktx:2.5.3")
    implementation("androidx.navigation:navigation-ui-ktx:2.5.3")
    implementation("androidx.preference:preference-ktx:1.2.0")
    implementation("androidx.appcompat:appcompat:1.6.1")
    implementation("androidx.work:work-runtime-ktx:2.8.1")
    implementation("androidx.work:work-multiprocess:2.8.1")

    implementation("org.tukaani:xz:1.10")

    implementation("com.google.android.material:material:1.8.0")
    implementation("com.google.code.gson:gson:2.9.0")

    implementation("com.github.jenly1314:zxing-lite:2.1.1")
    implementation("com.blacksquircle.ui:editorkit:2.6.0")
    implementation("com.blacksquircle.ui:language-base:2.6.0")
    implementation("com.blacksquircle.ui:language-json:2.6.0")

    implementation("com.squareup.okhttp3:okhttp:5.0.0-alpha.3")
    implementation("org.yaml:snakeyaml:1.30")
    implementation("com.github.daniel-stoneuk:material-about-library:3.2.0-rc01")
    implementation("com.jakewharton:process-phoenix:2.1.2")
    implementation("com.esotericsoftware:kryo:5.2.1")
    implementation("com.google.guava:guava:31.0.1-android")
    implementation("org.ini4j:ini4j:0.5.4")

    implementation("com.simplecityapps:recyclerview-fastscroll:2.0.1") {
        exclude(group = "androidx.recyclerview")
        exclude(group = "androidx.appcompat")
    }

    implementation("androidx.room:room-runtime:2.6.1")
    ksp("androidx.room:room-compiler:2.6.1")
    implementation("androidx.room:room-ktx:2.6.1")
    implementation("com.github.MatrixDev.Roomigrant:RoomigrantLib:0.3.4")
    ksp("com.github.MatrixDev.Roomigrant:RoomigrantCompiler:0.3.4")

    coreLibraryDesugaring("com.android.tools:desugar_jdk_libs:2.0.3")
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.robolectric:robolectric:4.14.1")
}

val buildHevTun by tasks.registering {
    val hevAbis = listOf("arm64-v8a")
    doLast {
        val missing = hevAbis.any { !file("src/main/jniLibs/$it/libhev-socks5-tunnel.so").exists() }
        if (missing || System.getenv("FORCE_HEV") == "1") {
            exec {
                commandLine("bash", rootProject.file("buildScript/compile-hevtun.sh").absolutePath)
            }
        }
    }
}

tasks.named("preBuild") {
    dependsOn(buildHevTun)
}
