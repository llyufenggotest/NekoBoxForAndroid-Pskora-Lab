package io.nekohasekai.sagernet.ktx
val app=android.content.Context()
fun android.content.Context.getColorAttr(i:Int)=0
fun runOnMainDispatcher(f:suspend ()->Unit){kotlinx.coroutines.runBlocking {f()} }
