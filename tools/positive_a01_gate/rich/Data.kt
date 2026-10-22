package io.nekohasekai.sagernet.database
import io.nekohasekai.sagernet.bg.Trace
object DataStore {var speedInterval=10;var showDirectSpeed=true;var profileTrafficStatistics=true;var showGroupInNotification=false}
class ProxyEntity(val id:Long,var rx:Long=0,var tx:Long=0,val groupId:Long=1){fun displayName()="synthetic"}
object SagerDatabase {val groupDao=GroupDao();class GroupDao {fun getById(i:Long):ProxyEntity?=null}}
object ProfileManager {var writes=0;var resets=0;val rows=linkedMapOf<Long,Pair<Long,Long>>();suspend fun updateTraffic(id:Long,rx:Long,tx:Long){writes++;rows[id]=rx to tx;if(Trace.enabled)Trace.add("dao:$id,$rx,$tx")};suspend fun resetTraffic(ids:LongArray){resets++;ids.forEach{rows[it]=0L to 0L};if(Trace.enabled)Trace.add("reset:"+ids.joinToString())}}
