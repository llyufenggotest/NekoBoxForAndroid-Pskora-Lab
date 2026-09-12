package io.nekohasekai.sagernet.fmt.trojan

import io.nekohasekai.sagernet.fmt.KryoConverters
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

class TrojanFastupFmtTest {

    @Test
    fun fastupSuffixSurvivesImportAndExport() {
        val link = "trojan://synthetic-password%23fastup@node.example:443?security=tls&type=tcp&mpw=rotated-mpw#Fastup"
        val bean = parseTrojan(link)

        assertEquals("synthetic-password#fastup", bean.password)
        assertEquals("rotated-mpw", bean.mpw)
    }

    @Test
    fun standardTrojanPasswordIsUnchanged() {
        val bean = parseTrojan("trojan://ordinary-password@node.example:443?security=tls&type=tcp&mpw=must-be-ignored#Standard")
        assertEquals("ordinary-password", bean.password)
        assertEquals("", bean.mpw)
    }

    @Test
    fun fastupShareLinkExportsMpwOnlyWithSuffix() {
        val bean = parseTrojan("trojan://synthetic-password%23fastup@node.example:443?security=tls&type=tcp&mpw=rotated-mpw#Fastup")
        assertEquals("rotated-mpw", bean.fastupMpwQueryParameter())

        bean.password = "ordinary-password"
        assertEquals(null, bean.fastupMpwQueryParameter())
        assertFalse(bean.password.endsWith("#fastup"))
    }

    @Test
    fun fastupMpwSurvivesDatabaseSerialization() {
        val bean = TrojanBean().apply {
            password = "synthetic-password#fastup"
            mpw = "rotated-mpw"
            initializeDefaultValues()
        }
        val restored = KryoConverters.trojanDeserialize(KryoConverters.serialize(bean))

        assertEquals(bean.password, restored.password)
        assertEquals(bean.mpw, restored.mpw)
    }
}
