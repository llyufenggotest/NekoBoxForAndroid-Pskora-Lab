package libcore
import io.nekohasekai.sagernet.bg.Trace
object Libcore {var logCalls=0;fun nekoLogPrintln(s:String){logCalls++}}
class BoxInstance {var calls=0;var delta=128L;var setCalls=0;var hook:(()->Unit)?=null;fun queryStats(tag:String,dir:String):Long{calls++;hook?.invoke();if(Trace.enabled)Trace.add("q:$tag,$dir,$delta");return delta};fun setV2rayStats(s:String){setCalls++;if(Trace.enabled)Trace.add("native-tags:"+s.split('\n').sorted().joinToString())}}
