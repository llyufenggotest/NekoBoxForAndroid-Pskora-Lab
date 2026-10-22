package android.content
open class Context { val packageName="fixture"; fun getString(id:Int,vararg args:Any?):String = "$id:"+args.joinToString(); fun getText(id:Int)="text$id"; fun registerReceiver(r:BroadcastReceiver,f:IntentFilter){}; fun unregisterReceiver(r:BroadcastReceiver){} }
open class BroadcastReceiver { open fun onReceive(context:Context,intent:Intent){} }
class Intent(var action:String?=null) { constructor(c:Context,k:Class<*>):this(); fun setPackage(s:String)=this; companion object {const val ACTION_SCREEN_ON="on"; const val ACTION_SCREEN_OFF="off"} }
class IntentFilter {fun addAction(s:String){} }
