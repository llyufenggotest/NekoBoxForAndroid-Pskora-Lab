package io.nekohasekai.sagernet.database
object DataStore {var speedInterval=10;var showDirectSpeed=true;var profileTrafficStatistics=true;var showGroupInNotification=false}
class ProxyEntity(val id:Long,var rx:Long=0,var tx:Long=0,val groupId:Long=1){fun displayName()="synthetic"}
object SagerDatabase {val groupDao=GroupDao();class GroupDao {fun getById(i:Long):ProxyEntity?=null}}
object ProfileManager {var writes=0;var resets=0;suspend fun updateTraffic(id:Long,rx:Long,tx:Long){writes++};suspend fun resetTraffic(ids:LongArray){resets++} }
