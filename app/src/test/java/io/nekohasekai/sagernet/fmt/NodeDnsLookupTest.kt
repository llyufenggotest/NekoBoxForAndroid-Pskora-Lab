package io.nekohasekai.sagernet.fmt

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayOutputStream
import java.io.DataOutputStream

class NodeDnsLookupTest {
    private fun response(query: ByteArray, flags: Int = 0x8180, owner: String = "example.test.", type: Int = 1): ByteArray {
        val out = ByteArrayOutputStream()
        DataOutputStream(out).use { data ->
            data.writeShort(query[0].toInt() and 255 shl 8 or (query[1].toInt() and 255))
            data.writeShort(flags); data.writeShort(1); data.writeShort(1); data.writeShort(0); data.writeShort(0)
            data.write(query, 12, query.size - 12)
            owner.removeSuffix(".").split('.').forEach { label -> data.writeByte(label.length); data.writeBytes(label) }
            data.writeByte(0); data.writeShort(type); data.writeShort(1)
            data.writeInt(60); data.writeShort(if (type == 1) 4 else 16)
            if (type == 1) data.write(byteArrayOf(192.toByte(), 0, 2, 1))
            else data.write(ByteArray(15) + byteArrayOf(1))
        }
        return out.toByteArray()
    }

    private fun compressed(q: ByteArray): ByteArray {
        val full = response(q)
        val start = q.size
        return full.copyOfRange(0, start) + byteArrayOf(0xc0.toByte(), 12) + full.copyOfRange(start + 14, full.size)
    }

    @Test fun compressedOwnerIsAccepted() {
        val q = NodeDnsLookup.query("example.test")
        assertEquals("192.0.2.1", NodeDnsLookup.answer(compressed(q), "example.test", q))
    }

    @Test fun wrongQuestionTypeClassAndTruncationAreRejected() {
        val q = NodeDnsLookup.query("example.test")
        for (offset in listOf(q.size - 3, q.size - 1)) {
            val bytes = response(q).also { it[offset] = 28 }
            assertNull(NodeDnsLookup.answer(bytes, "example.test", q))
        }
        assertNull(NodeDnsLookup.answer(response(q, flags = 0x8380), "example.test", q))
    }

    private fun wireName(value: String): ByteArray = ByteArrayOutputStream().also { bytes ->
        DataOutputStream(bytes).use { out ->
            value.split('.').forEach { out.writeByte(it.length); out.writeBytes(it) }; out.writeByte(0)
        }
    }.toByteArray()

    private fun rr(owner: ByteArray, type: Int, payload: ByteArray): ByteArray = ByteArrayOutputStream().also { bytes ->
        DataOutputStream(bytes).use { out ->
            out.write(owner); out.writeShort(type); out.writeShort(1); out.writeInt(60)
            out.writeShort(payload.size); out.write(payload)
        }
    }.toByteArray()

    private fun withRecords(q: ByteArray, records: List<ByteArray>): ByteArray {
        val header = compressed(q).copyOfRange(0, q.size)
        header[6] = (records.size shr 8).toByte(); header[7] = records.size.toByte()
        return records.fold(header) { bytes, record -> bytes + record }
    }

    @Test fun compressedCnameChainAndNegativeCases() {
        val q = NodeDnsLookup.query("example.test")
        val ptr = byteArrayOf(0xc0.toByte(), 12)
        val addr = byteArrayOf(192.toByte(), 0, 2, 1)
        val alias = byteArrayOf(5) + "alias".toByteArray() + byteArrayOf(0xc0.toByte(), 20)
        val cname = rr(ptr, 5, alias)
        val targetPointer = byteArrayOf(0xc0.toByte(), (q.size + 12).toByte())
        assertEquals("192.0.2.1", NodeDnsLookup.answer(withRecords(q, listOf(cname, rr(targetPointer, 1, addr))), "example.test", q))
        assertEquals("192.0.2.1", NodeDnsLookup.answer(withRecords(q, listOf(rr(wireName("final.test"), 1, addr), rr(wireName("alias.test"), 5, wireName("final.test")), cname)), "example.test", q))
        val invalid = listOf(
            listOf(cname, rr(wireName("other.test"), 1, addr)),
            listOf(cname, rr(wireName("alias.test"), 5, ptr)),
            listOf(cname, rr(ptr, 5, wireName("other.test")), rr(wireName("alias.test"), 1, addr)),
            listOf(rr(ptr, 5, alias + byteArrayOf(0)), rr(wireName("alias.test"), 1, addr)),
            listOf(cname, rr(ptr, 1, addr))
        )
        invalid.forEach { assertNull(NodeDnsLookup.answer(withRecords(q, it), "example.test", q)) }
    }

    @Test fun malformedPointersLengthsAndTrailingRecordsAreRejected() {
        val q = NodeDnsLookup.query("example.test")
        val addr = byteArrayOf(192.toByte(), 0, 2, 1)
        val ptr = byteArrayOf(0xc0.toByte(), 12)
        for (owner in listOf(byteArrayOf(0xc0.toByte(), q.size.toByte()), byteArrayOf(0xff.toByte(), 0xff.toByte()), byteArrayOf(0xc0.toByte()), byteArrayOf(0x40, 0), byteArrayOf(63, 1))) {
            assertNull(NodeDnsLookup.answer(withRecords(q, listOf(rr(owner, 1, addr))), "example.test", q))
        }
        val valid = compressed(q)
        for (size in 0 until valid.size) assertNull(NodeDnsLookup.answer(valid.copyOf(size), "example.test", q))
        assertNull(NodeDnsLookup.answer(withRecords(q, listOf(rr(ptr, 1, addr), rr(byteArrayOf(0xff.toByte(), 0xff.toByte()), 1, addr))), "example.test", q))
        assertNull(NodeDnsLookup.answer(withRecords(q, listOf(rr(ptr, 1, addr + byteArrayOf(0)))), "example.test", q))
    }

    @Test fun realTcpPeerUsesLengthFraming() {
        java.net.ServerSocket(0, 1, java.net.InetAddress.getByName("127.0.0.1")).use { server ->
            server.soTimeout = 5000
            val executor = java.util.concurrent.Executors.newSingleThreadExecutor()
            try {
                val peer = executor.submit<Unit> { server.accept().use { socket ->
                    socket.soTimeout = 5000
                    val input = java.io.DataInputStream(socket.getInputStream())
                    val q = ByteArray(input.readUnsignedShort()).also { input.readFully(it) }
                    val bytes = compressed(q)
                    DataOutputStream(socket.getOutputStream()).apply { writeShort(bytes.size); write(bytes); flush() }
                } }
                assertEquals("192.0.2.1", NodeDnsLookup.resolve("example.test", "tcp://127.0.0.1:${server.localPort}"))
                peer.get(6, java.util.concurrent.TimeUnit.SECONDS)
            } finally { executor.shutdownNow() }
        }
    }

    @Test fun realUdpPeerUsesConfiguredEndpointForAAndAaaa() {
        java.net.DatagramSocket(0, java.net.InetAddress.getByName("127.0.0.1")).use { server ->
            server.soTimeout = 5000
            val executor = java.util.concurrent.Executors.newSingleThreadExecutor()
            try {
                val peer = executor.submit<Unit> {
                    for (type in listOf(1, 28)) {
                        val packet = java.net.DatagramPacket(ByteArray(4096), 4096); server.receive(packet)
                        val q = packet.data.copyOf(packet.length)
                        assertEquals(type, q[q.size - 3].toInt() and 255)
                        val records = if (type == 1) emptyList() else listOf(rr(byteArrayOf(0xc0.toByte(), 12), 28, ByteArray(15) + byteArrayOf(1)))
                        val bytes = withRecords(q, records)
                        server.send(java.net.DatagramPacket(bytes, bytes.size, packet.socketAddress))
                    }
                }
                assertEquals("0:0:0:0:0:0:0:1", NodeDnsLookup.resolve("example.test", "udp://127.0.0.1:${server.localPort}"))
                peer.get(6, java.util.concurrent.TimeUnit.SECONDS)
            } finally { executor.shutdownNow() }
        }
    }

    @Test fun queryBoundsAnswerClassAndCredentialsAreRejected() {
        assertTrue(runCatching { NodeDnsLookup.query("example.test", 5) }.isFailure)
        assertTrue(runCatching { NodeDnsLookup.query(List(4) { "a".repeat(63) }.joinToString(".")) }.isFailure)
        assertNull(NodeDnsLookup.resolve("example.test", "udp://user:password@127.0.0.1:9"))
        val q = NodeDnsLookup.query("example.test")
        val wrongClass = compressed(q).also { it[q.size + 5] = 3 }
        assertNull(NodeDnsLookup.answer(wrongClass, "example.test", q))
        assertNull(NodeDnsLookup.answer(response(q, flags = 0x0180), "example.test", q))
    }

    @Test fun randomQueryIdAndAaaaAreAccepted() {
        val q1 = NodeDnsLookup.query("example.test")
        // Random IDs can legitimately collide; do not make this a flaky two-query test.
        assertTrue((1..32).map { NodeDnsLookup.query("example.test").take(2) }.toSet().size > 1)
        assertEquals("192.0.2.1", NodeDnsLookup.answer(response(q1), "example.test", q1))
        val qAaaa = NodeDnsLookup.query("example.test", 28)
        assertEquals("0:0:0:0:0:0:0:1", NodeDnsLookup.answer(response(qAaaa, owner = "example.test.", type = 28), "example.test", qAaaa))
    }

    @Test fun wrongIdQuestionOwnerOpcodeAndQuestionAreRejected() {
        val q = NodeDnsLookup.query("example.test")
        val wrongId = response(q).also { it[1] = (it[1].toInt() xor 1).toByte() }
        assertNull(NodeDnsLookup.answer(wrongId, "example.test", q))
        assertNull(NodeDnsLookup.answer(response(q, owner = "other.test."), "example.test", q))
        assertNull(NodeDnsLookup.answer(response(q, flags = 0x8188), "example.test", q))
        assertNull(NodeDnsLookup.answer(response(q, type = 28), "example.test", q))
    }
}
