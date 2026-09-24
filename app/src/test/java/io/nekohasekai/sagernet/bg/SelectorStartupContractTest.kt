package io.nekohasekai.sagernet.bg

import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Test

class SelectorStartupContractTest {
    private fun source(): String = listOf(
        File("src/main/java/io/nekohasekai/sagernet/bg/proto/BoxInstance.kt"),
        File("app/src/main/java/io/nekohasekai/sagernet/bg/proto/BoxInstance.kt"),
    ).first { it.isFile }.readText().replace("\r\n", "\n")

    @Test fun launchedProfileOverridesCachedSelectorChoiceBeforeConnected() {
        val source = source()
        val start = source.indexOf("box.start()")
        val expectedTag = source.indexOf("config.profileTagMap[profile.id]", start)
        val select = source.indexOf("box.selectOutbound(expectedTag)", expectedTag)
        assertTrue(start >= 0)
        assertTrue(expectedTag > start)
        assertTrue(select > expectedTag)
        assertTrue(source.contains("check(box.selectOutbound(expectedTag))"))
    }
}
