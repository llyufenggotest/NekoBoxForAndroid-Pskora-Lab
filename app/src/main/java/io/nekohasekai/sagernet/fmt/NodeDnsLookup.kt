package io.nekohasekai.sagernet.fmt

import java.io.*
import java.net.*
import javax.net.ssl.SSLSocketFactory

/** TCP ping resolver. Preserve configured transport/endpoint; never substitute a public DNS. */
internal object NodeDnsLookup {
    private const val MAX_DNS_MESSAGE = 64 * 1024
    private const val TIMEOUT_MS = 3000

    fun resolve(domain: String, resolver: String): String? = runCatching {
        if (resolver == "local") return@runCatching InetAddress.getAllByName(domain).firstOrNull()?.hostAddress
        val uri = URI(if (resolver.contains("://")) resolver else "udp://$resolver")
        require(uri.rawUserInfo == null && uri.rawFragment == null)
        val host = requireNotNull(uri.host)
        val port = if (uri.port > 0) uri.port else when (uri.scheme) { "tls" -> 853; "https" -> 443; else -> 53 }
        fun lookup(type: Int): String? {
            val request = query(domain, type)
        val response = when (uri.scheme) {
            "https" -> {
                val connection = uri.toURL().openConnection() as HttpURLConnection
                connection.connectTimeout = TIMEOUT_MS; connection.readTimeout = TIMEOUT_MS
                connection.instanceFollowRedirects = false
                connection.requestMethod = "POST"; connection.doOutput = true
                connection.setRequestProperty("Content-Type", "application/dns-message")
                connection.setRequestProperty("Accept", "application/dns-message")
                try {
                    connection.outputStream.use { it.write(request) }
                    require(connection.responseCode == HttpURLConnection.HTTP_OK)
                    connection.inputStream.use { readBounded(it) }
                } finally { connection.disconnect() }
            }
            "tcp", "tls" -> {
                val raw = Socket().apply { soTimeout = TIMEOUT_MS; connect(InetSocketAddress(host, port), TIMEOUT_MS) }
                raw.use {
                    val socket = if (uri.scheme == "tls") {
                        val layered = (SSLSocketFactory.getDefault() as javax.net.ssl.SSLSocketFactory).createSocket(it, host, port, true) as javax.net.ssl.SSLSocket
                        layered.sslParameters = layered.sslParameters.apply { endpointIdentificationAlgorithm = "HTTPS" }
                        layered.startHandshake(); layered
                    } else it
                    socket.use { connected ->
                        val output = DataOutputStream(connected.getOutputStream())
                        output.writeShort(request.size); output.write(request); output.flush()
                        val input = DataInputStream(connected.getInputStream())
                        val size = input.readUnsignedShort(); require(size <= MAX_DNS_MESSAGE)
                        ByteArray(size).also { bytes -> input.readFully(bytes) }
                    }
                }
            }
            "udp" -> DatagramSocket().use {
                it.soTimeout = TIMEOUT_MS
                it.connect(InetSocketAddress(host, port))
                it.send(DatagramPacket(request, request.size))
                val packet = DatagramPacket(ByteArray(MAX_DNS_MESSAGE), MAX_DNS_MESSAGE)
                it.receive(packet); packet.data.copyOf(packet.length)
            }
            else -> error("Unsupported DNS transport")
        }
        return answer(response, domain, request)
        }
        lookup(1) ?: lookup(28)
    }.getOrNull()

    private val random = java.security.SecureRandom()

    internal fun query(domain: String, type: Int = 1): ByteArray = ByteArrayOutputStream().also { bytes ->
        require(type == 1 || type == 28)
        val ascii = IDN.toASCII(domain.removeSuffix("."))
        require(ascii.length <= 253)
        DataOutputStream(bytes).use { out ->
            out.writeShort(random.nextInt(65536)); out.writeShort(0x0100); out.writeShort(1)
            repeat(3) { out.writeShort(0) }
            ascii.split('.').forEach { label ->
                val value = label.toByteArray(Charsets.US_ASCII)
                require(value.size in 1..63); out.writeByte(value.size); out.write(value)
            }
            out.writeByte(0); out.writeShort(type); out.writeShort(1)
        }
    }.toByteArray()

    private fun readBounded(input: InputStream): ByteArray {
        val output = ByteArrayOutputStream()
        val buffer = ByteArray(4096)
        while (true) {
            val count = input.read(buffer)
            if (count < 0) return output.toByteArray()
            require(output.size() + count <= MAX_DNS_MESSAGE)
            output.write(buffer, 0, count)
        }
    }
    /** Cursor advances over the encoded name, while pointers traverse the original message. */
    private class Reader(val bytes: ByteArray) {
        var position = 0
        fun u16(): Int {
            require(position + 2 <= bytes.size)
            return ((bytes[position++].toInt() and 255) shl 8) or (bytes[position++].toInt() and 255)
        }
        fun name(end: Int = bytes.size): String {
            var cursor = position
            var jumped = false
            var expanded = 1
            val visited = HashSet<Int>()
            val labels = mutableListOf<String>()
            while (true) {
                val limit = if (jumped) bytes.size else end
                require(cursor < limit && visited.add(cursor))
                val length = bytes[cursor].toInt() and 255
                if (length == 0) {
                    if (!jumped) position = cursor + 1
                    return labels.joinToString(".").lowercase(java.util.Locale.ROOT) + "."
                }
                if (length and 0xC0 == 0xC0) {
                    require(cursor + 1 < limit)
                    val target = ((length and 63) shl 8) or (bytes[cursor + 1].toInt() and 255)
                    // RFC compression refers to an earlier name, never header/forward/self offsets.
                    require(target >= 12 && target < cursor)
                    if (!jumped) position = cursor + 2
                    cursor = target; jumped = true
                } else {
                    require(length in 1..63 && cursor + 1 + length <= limit)
                    expanded += length + 1; require(expanded <= 255)
                    val label = bytes.copyOfRange(cursor + 1, cursor + 1 + length)
                    require(label.all { (it.toInt() and 255) in 33..126 && it != '.'.code.toByte() })
                    labels += label.toString(Charsets.US_ASCII)
                    cursor += 1 + length
                }
            }
        }
    }

    internal fun answer(bytes: ByteArray, domain: String, request: ByteArray): String? = runCatching {
        require(bytes.size in 12..MAX_DNS_MESSAGE && request.size >= 17)
        val expected = Reader(request)
        val id = expected.u16()
        expected.position = 12
        val expectedName = expected.name()
        val expectedType = expected.u16(); val expectedClass = expected.u16()
        require(expectedType == 1 || expectedType == 28)
        require(expectedClass == 1 && expectedName == IDN.toASCII(domain.trimEnd('.')).lowercase(java.util.Locale.ROOT) + ".")
        val input = Reader(bytes)
        require(input.u16() == id)
        val flags = input.u16()
        require(flags and 0x8000 != 0 && flags and 0x7800 == 0 && flags and 0x0200 == 0 && flags and 0x000F == 0)
        require(input.u16() == 1)
        val answers = input.u16(); val authorities = input.u16(); val additional = input.u16()
        require(input.name() == expectedName && input.u16() == expectedType && input.u16() == expectedClass)
        val aliases = mutableMapOf<String, String>()
        val addresses = mutableMapOf<String, MutableList<ByteArray>>()
        repeat(answers + authorities + additional) { index ->
            val owner = input.name()
            val type = input.u16(); val klass = input.u16()
            input.u16(); input.u16() // TTL
            val size = input.u16(); val end = input.position + size
            require(end <= bytes.size)
            if (type == 5) {
                val target = input.name(end)
                require(input.position == end && target != ".")
                if (index < answers && klass == 1) {
                    require(aliases[owner] == null || aliases[owner] == target)
                    aliases[owner] = target
                }
            } else if (type == 1 || type == 28) {
                require(size == if (type == 1) 4 else 16)
                if (index < answers && klass == 1 && type == expectedType) {
                    addresses.getOrPut(owner) { mutableListOf() }.add(bytes.copyOfRange(input.position, end))
                }
            }
            input.position = end
        }
        require(input.position == bytes.size)
        var owner = expectedName
        val visited = HashSet<String>()
        while (true) {
            require(visited.add(owner))
            val target = aliases[owner]
            if (target == null) break
            require(addresses[owner].isNullOrEmpty())
            owner = target
        }
        addresses[owner]?.firstOrNull()?.let { InetAddress.getByAddress(it).hostAddress }
    }.getOrNull()
}
