package io.nekohasekai.sagernet.aidl
data class SpeedDisplayData(var txRateProxy:Long=0,var rxRateProxy:Long=0,var txRateDirect:Long=0,var rxRateDirect:Long=0,var txTotal:Long=0,var rxTotal:Long=0)
data class TrafficData(val id:Long,val rx:Long,val tx:Long){init{created++};companion object{var created=0}}
data class TrafficDataBatch(val items:ArrayList<TrafficData>)
