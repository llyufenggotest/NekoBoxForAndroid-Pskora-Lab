package io.nekohasekai.sagernet.bg
import io.nekohasekai.sagernet.aidl.*
class BaseService {enum class State {Connected,Stopped}; interface Interface {val data:Data}
class Data {var proxy:io.nekohasekai.sagernet.bg.proto.ProxyInstance?=null;var state=State.Connected;val binder=Binder();var notification:ServiceNotification?=null}
class Callback {var speeds=0;var batches=0;var items=0;fun cbSpeedUpdate(s:SpeedDisplayData){speeds++};fun cbTrafficUpdate(t:TrafficDataBatch){batches++;items+=t.items.size}}
class Binder {val callbackIdMap=mutableMapOf<Callback,Int>();suspend fun broadcast(f:(Callback)->Unit){callbackIdMap.keys.toList().forEach(f)}} }
object SagerConnection {const val CONNECTION_ID_MAIN_ACTIVITY_FOREGROUND=2}
class FakeService:android.app.Service(),BaseService.Interface {override val data=BaseService.Data()}
