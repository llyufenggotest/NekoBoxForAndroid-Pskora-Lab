package android.app
import android.content.*
open class Service:Context() { var starts=0; var stops=0; fun startForeground(i:Int,n:Any,t:Int=0){starts++}; fun stopForeground(b:Boolean){stops++}; fun stopForeground(i:Int){stops++}; companion object {const val STOP_FOREGROUND_REMOVE=1} }
class PendingIntent { companion object {const val FLAG_IMMUTABLE=1; fun getBroadcast(c:Context,i:Int,t:Intent,f:Int)=PendingIntent(); fun getActivity(c:Context,i:Int,t:Intent,f:Int)=PendingIntent()} }
