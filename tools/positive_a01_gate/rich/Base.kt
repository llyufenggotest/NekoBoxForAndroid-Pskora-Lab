package io.nekohasekai.sagernet.bg
import io.nekohasekai.sagernet.aidl.*
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
object Trace {var enabled=true;val events=ArrayList<String>();fun add(s:String){if(enabled)events.add(s)}}
class BaseService {
 enum class State {Connected,Stopped}
 interface Interface {val data:Data}
 class Data {var proxy:io.nekohasekai.sagernet.bg.proto.ProxyInstance?=null;var state=State.Connected;val binder=Binder();var notification:ServiceNotification?=null}
 class Callback(val name:String="ui") {var speeds=0;var batches=0;var items=0;fun cbSpeedUpdate(s:SpeedDisplayData){speeds++;if(Trace.enabled)Trace.add("$name:s:${s.txRateProxy},${s.rxRateProxy},${s.txRateDirect},${s.rxRateDirect},${s.txTotal},${s.rxTotal}")};fun cbTrafficUpdate(t:TrafficDataBatch){batches++;items+=t.items.size;if(Trace.enabled)Trace.add("$name:t:"+t.items.joinToString(";"){"${it.id},${it.rx},${it.tx}"})}}
 class Binder {val callbackIdMap=mutableMapOf<Callback,Int>();val mutex=Mutex();var beforeBroadcast:(suspend ()->Unit)?=null;suspend fun broadcast(f:(Callback)->Unit){mutex.withLock{beforeBroadcast?.invoke();callbackIdMap.keys.toList().forEach(f)}}}
}
object SagerConnection {const val CONNECTION_ID_MAIN_ACTIVITY_FOREGROUND=2}
class FakeService:android.app.Service(),BaseService.Interface {override val data=BaseService.Data()}
