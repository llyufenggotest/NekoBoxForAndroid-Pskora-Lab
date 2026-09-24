package io.nekohasekai.sagernet.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class IpQualityPresentationTest {
    @Test fun countryCodeBecomesFlag() {
        assertEquals("🇭🇰", countryFlag("hk"))
        assertEquals("🌐", countryFlag(""))
    }

    @Test fun providerValuesUseChineseLabels() {
        assertEquals("原生 IP", ipSourceChinese("native"))
        assertEquals("广播 IP", ipSourceChinese("broadcast"))
        assertEquals("住宅 IP", ipAttributeChinese("residential"))
        assertEquals("机房 IP", ipAttributeChinese("datacenter"))
        assertEquals("纯净", ipQualityTierChinese("green"))
        assertEquals("风险", ipQualityTierChinese("red"))
    }
}
