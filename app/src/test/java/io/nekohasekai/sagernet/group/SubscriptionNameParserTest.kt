package io.nekohasekai.sagernet.group

import org.junit.Assert.assertEquals
import org.junit.Test

class SubscriptionNameParserTest {
    @Test
    fun plainContentDispositionFilenameIsDetected() {
        assertEquals(
            "iKuuu_V2",
            RawUpdater.parseContentDisposition("attachment; filename=iKuuu_V2.yaml"),
        )
    }

    @Test
    fun quotedContentDispositionFilenameIsDetected() {
        assertEquals(
            "Airport Name",
            RawUpdater.parseContentDisposition("attachment; filename=\"Airport Name.yml\""),
        )
    }

    @Test
    fun encodedContentDispositionFilenameIsDetected() {
        assertEquals(
            "机场订阅",
            RawUpdater.parseContentDisposition("attachment; filename*=UTF-8''%E6%9C%BA%E5%9C%BA%E8%AE%A2%E9%98%85.yaml"),
        )
    }
}