package io.nekohasekai.sagernet.bg.proto
class ProxyInstance {val box=libcore.BoxInstance();val config=Config();fun isInitialized()=true}
class Config {val trafficMap=linkedMapOf<String,List<io.nekohasekai.sagernet.database.ProxyEntity>>();var selectorGroupId=-1L;var mainEntId=1L}
