package io.nekohasekai.sagernet.bg.proto

import com.google.gson.JsonParser
import java.net.ServerSocket
import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test
import java.net.InetSocketAddress
import java.net.HttpURLConnection
import java.net.URL
import java.io.File
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong

/** Host scheduler benchmark, not Android/native protocol speed: real loopback HTTP + JSON work. */
class UrlTestBenchmarkTest {
    @Test fun compareSameWorkloadOverRealLocalHttp() = runBlocking {
        val server = ServerSocket(0, 64, java.net.InetAddress.getByName("127.0.0.1"))
        val executor = Executors.newFixedThreadPool(16)
        val acceptor = kotlin.concurrent.thread(isDaemon = true) {
            while (!server.isClosed) {
                val socket = try { server.accept() } catch (_: java.io.IOException) { break }
                executor.submit {
                    socket.use {
                        it.soTimeout = 5000
                        val reader = it.getInputStream().bufferedReader()
                        while (!reader.readLine().isNullOrEmpty()) { }
                        it.getOutputStream().write("HTTP/1.1 204 No Content\r\nContent-Length: 0\r\nConnection: close\r\n\r\n".toByteArray())
                        it.getOutputStream().flush()
                    }
                }
            }
        }
        val endpoint = "http://127.0.0.1:${server.localPort}/generate_204"
        val config = "{\"outbounds\":[" + (1..100).joinToString(",") { "{\"type\":\"socks\",\"server\":\"127.0.0.1\",\"server_port\":1080,\"tag\":\"test-$it\"}" } + "]}"
        val rows = mutableListOf("mode,round,count,workers,total_ms,first_result_ms,discovery_ms,prepare_sum_ms,start_sum_ms,http_sum_ms,failures")
        fun prepare() { repeat(3) { JsonParser.parseString(config) } }
        suspend fun measure(old: Boolean, round: Int) {
            val items = (1..100).toList()
            val failures = AtomicInteger(); val first = AtomicLong(); val prep = AtomicLong(); val start = AtomicLong(); val http = AtomicLong()
            val begin = System.nanoTime()
            if (old) items.forEach { prepare() }
            val discovery = System.nanoTime() - begin
            val action: suspend (Int) -> Unit = {
                val a = System.nanoTime(); prepare(); val b = System.nanoTime()
                val connection = URL(endpoint).openConnection() as HttpURLConnection
                connection.connectTimeout = 5000; connection.readTimeout = 5000
                val c = System.nanoTime()
                try { if (connection.responseCode != 204) failures.incrementAndGet() } catch (_: Exception) { failures.incrementAndGet() } finally { connection.disconnect() }
                val d = System.nanoTime(); first.compareAndSet(0, d-begin)
                prep.addAndGet(b-a); start.addAndGet(c-b); http.addAndGet(d-c)
            }
            if (old) coroutineScope {
                val queue = ConcurrentLinkedQueue(items)
                List(5) { launch(Dispatchers.IO) { while (isActive) { val id = queue.poll() ?: break; action(id) } } }.joinAll()
            } else runUrlTestBatch(items, 5, action)
            fun ms(ns: Long) = "%.3f".format(java.util.Locale.US, ns / 1e6)
            rows.add(listOf(if (old) "old" else "new", round, items.size, 5, ms(System.nanoTime()-begin), ms(first.get()), ms(discovery), ms(prep.get()), ms(start.get()), ms(http.get()), failures.get()).joinToString(","))
            assertEquals(0, failures.get())
        }
        try {
            // Warm-up both paths, then alternate order to reduce JVM bias.
            measure(true, -1); measure(false, -1); rows.subList(1, rows.size).clear()
            repeat(6) { if (it % 2 == 0) { measure(true, it); measure(false, it) } else { measure(false, it); measure(true, it) } }
            val target = File("../evidence/urltest-optimized/host-benchmark.csv")
            target.parentFile.mkdirs(); target.writeText(rows.joinToString("\n") + "\n")
        } finally { server.close(); acceptor.join(5000); executor.shutdownNow() }
    }
}
