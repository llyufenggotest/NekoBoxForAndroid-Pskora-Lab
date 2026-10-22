package libcore
object Libcore {var logCalls=0;fun nekoLogPrintln(s:String){logCalls++}}
class BoxInstance {var calls=0;var delta=128L;var setCalls=0;fun queryStats(tag:String,dir:String):Long{calls++;return delta};fun setV2rayStats(s:String){setCalls++}}
