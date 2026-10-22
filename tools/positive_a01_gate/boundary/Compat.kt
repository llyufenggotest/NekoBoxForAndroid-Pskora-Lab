package androidx.core.app
import android.content.Context
object NotificationCompat {
const val PRIORITY_HIGH=1; const val PRIORITY_LOW=0;const val PRIORITY_MIN=-1;const val CATEGORY_SERVICE="service"
class BigTextStyle { fun bigText(s:String)=this }
class Action { class Builder(a:Int,b:CharSequence,c:Any) {fun setShowsUserInterface(b:Boolean)=this;fun build()=Action()} }
class Builder(c:Context,s:String) {var priority=0; var color=0
fun setWhen(v:Long)=this;fun setTicker(s:String)=this;fun setContentTitle(s:String)=this;fun setOnlyAlertOnce(b:Boolean)=this;fun setContentIntent(a:Any)=this;fun setSmallIcon(i:Int)=this;fun setCategory(s:String)=this;fun setPriority(i:Int)=this;fun clearActions()=this;fun addAction(a:Action)=this;fun setStyle(s:BigTextStyle)=this;fun setContentText(s:String)=this;fun setSubText(s:String)=this;fun build():Any=Any()
}}
class NotificationManagerCompat { fun notify(i:Int,n:Any){notifications++}; companion object {var notifications=0;fun from(s:Context)=NotificationManagerCompat()} }
