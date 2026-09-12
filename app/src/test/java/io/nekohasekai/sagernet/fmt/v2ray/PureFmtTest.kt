package io.nekohasekai.sagernet.fmt.v2ray

import org.junit.Assert.*
import org.junit.Test
import okhttp3.HttpUrl.Companion.toHttpUrl

class PureFmtTest {
 private val id = "123e4567-e89b-82d3-a456-426614174000"
 @Test fun xlessImportAndShareRoundTrip() {
  for (suffix in listOf("", "%23pure", "%23PuRe")) {
   val b = parsePureLink("xless://$id$suffix@node.example:443#display%20%23juzi")
   assertTrue(b.isVLESS); assertTrue(b.isPure());assertEquals("display #juzi",b.name)
   b.initializeDefaultValues()
   val link=b.toUriVMessVLESSTrojan(false)
   val restored=VMessBean().apply {alterId=-1;parseDuckSoft(link.replace("vless://","https://").toHttpUrl())}
   assertEquals(b.uuid,restored.uuid);assertEquals(b.name,restored.name)
  }
 }
 @Test fun rejectInvalidIdentity() {
  for (bad in listOf("bad#pure", "$id#juzi#pure", "$id#pure#sl", "$id#PURE-extra", "$id#tunnet#pure")) {
   val b=VMessBean().apply {alterId=-1;uuid=bad;initializeDefaultValues()}
   assertThrows(IllegalArgumentException::class.java){b.isPure()}
  }
 }
 @Test fun equivalentFinalCoreConfig() {
  val query="security=tls&alpn=http/1.1&allowInsecure=1&type=ws&path=%2Fwebsocket&mode=xless"
  val x=parsePureLink("xless://$id@node.example:443?$query#中文备注")
  val v=VMessBean().apply {alterId=-1;parseDuckSoft("https://$id%23pure@node.example:443?$query#中文备注".toHttpUrl());initializeDefaultValues()}
  val gson=com.google.gson.Gson()
  assertEquals(gson.toJson(buildSingBoxOutboundStandardV2RayBean(x)),gson.toJson(buildSingBoxOutboundStandardV2RayBean(v)))
  assertEquals("中文备注",x.name)
 }
 @Test fun batchCommonParser() = kotlinx.coroutines.runBlocking {
  val items=io.nekohasekai.sagernet.ktx.parseProxies("xless://$id@one.example:443#一\nxless://$id%23PURE@two.example:443#二")
  assertEquals(2,items.size)
  assertTrue(items.all{(it as VMessBean).isPure()})
 }
 @Test fun modeAndRemarkAreNotSelectors() {
  val b=VMessBean().apply {alterId=-1;parseDuckSoft("https://$id@node.example/?mode=xless#pure".toHttpUrl());initializeDefaultValues()}
  assertFalse(b.isPure());assertEquals(id,b.uuid)
 }
}
