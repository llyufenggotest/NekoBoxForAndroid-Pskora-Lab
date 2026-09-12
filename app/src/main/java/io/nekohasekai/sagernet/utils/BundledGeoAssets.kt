package io.nekohasekai.sagernet.utils

import android.content.Context
import java.io.File
import java.io.RandomAccessFile
import org.tukaani.xz.XZInputStream

/** Install missing/empty bundled rules before native initialization, across both app processes. */
object BundledGeoAssets {
    fun ensure(context: Context, destination: File) = ensure(destination) { context.assets.open(it) }

    internal fun ensure(destination: File, open: (String) -> java.io.InputStream) {
        destination.mkdirs()
        RandomAccessFile(File(destination, ".geo-install.lock"), "rw").use { lockFile ->
            lockFile.channel.lock().use {
                for (name in listOf("geoip", "geosite")) {
                    val target = File(destination, "$name.db")
                    if (target.isFile && target.length() > 0) continue
                    val temporary = File(destination, "$name.db.installing")
                    try {
                        open("sing-box/$name.db.xz").use { input ->
                            XZInputStream(input).use { source ->
                                temporary.outputStream().use { output -> source.copyTo(output); output.fd.sync() }
                            }
                        }
                        check(temporary.length() > 0) { "Empty bundled $name database" }
                        if (target.exists()) check(target.delete()) { "Cannot replace empty $target" }
                        check(temporary.renameTo(target)) { "Cannot install $target" }
                        open("sing-box/$name.version.txt").use { input ->
                            File(destination, "$name.version.txt").outputStream().use { input.copyTo(it) }
                        }
                    } finally {
                        temporary.delete()
                    }
                }
            }
        }
    }
}
