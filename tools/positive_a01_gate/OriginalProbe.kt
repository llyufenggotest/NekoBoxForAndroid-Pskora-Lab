import io.nekohasekai.sagernet.bg.*
import io.nekohasekai.sagernet.bg.proto.*
import io.nekohasekai.sagernet.database.*
import io.nekohasekai.sagernet.aidl.*
import kotlinx.coroutines.*
import java.util.PriorityQueue
import java.lang.management.ManagementFactory
@OptIn(InternalCoroutinesApi::class)
class Virtual:CoroutineDispatcher(),Delay {
 data class Task(val due:Long,val seq:Long,val run:Runnable):Comparable<Task>{override fun compareTo(o:Task)=compareValuesBy(this,o,{it.due},{it.seq})}
 var now=0L;var seq=0L;val q=PriorityQueue<Task>()
 override fun dispatch(context:kotlin.coroutines.CoroutineContext,block:Runnable){q.add(Task(now,seq++,block))}
 override fun scheduleResumeAfterDelay(timeMillis:Long,continuation:CancellableContinuation<Unit>){q.add(Task(now+timeMillis,seq++,Runnable {if(continuation.isActive)continuation.resume(Unit){}}))}
 fun current(){var n=0;while(q.isNotEmpty()&&q.peek().due<=now){check(n++<10000);q.poll().run.run()}}
 fun tick(){Thread.sleep(2);now+=10;current()}
}
fun emit(name:String,fields:String){println("RESULT {\"name\":\"$name\",$fields}")}
fun scenario(name:String,fg:Boolean,screen:Boolean,direct:Boolean=true,stats:Boolean=true,delta:Long=128,interval:Int=10,n:Int=4){
 DataStore.speedInterval=interval;DataStore.showDirectSpeed=direct;DataStore.profileTrafficStatistics=stats
 ProfileManager.writes=0;TrafficData.created=0;androidx.core.app.NotificationManagerCompat.notifications=0;android.text.format.Formatter.calls=0
 val s=FakeService();val p=ProxyInstance();s.data.proxy=p;p.box.delta=delta
 repeat(n){p.config.trafficMap["tag$it"]=listOf(ProxyEntity((it+1).toLong()))}
 val cb=BaseService.Callback();s.data.binder.callbackIdMap[cb]=if(fg)2 else 3
 val notif=ServiceNotification(s,"synthetic","fixture");s.data.notification=notif
 if(!screen)notif.onReceive(s,android.content.Intent(android.content.Intent.ACTION_SCREEN_OFF))
 val v=Virtual();val scope=CoroutineScope(SupervisorJob()+v);val l=TrafficLooper(s.data,scope);l.start();v.current();repeat(9){v.tick()}
 val pre=p.box.calls;val created=TrafficData.created; if(name=="video_other_app_screen_on") check(created==0){"A01 no-consumer DTO budget expected=0 actual=$created"};val writesDuringLoop=ProfileManager.writes;check(writesDuringLoop==0);val speed=cb.speeds;val bat=cb.batches;val notes=androidx.core.app.NotificationManagerCompat.notifications;val fmt=android.text.format.Formatter.calls
 val stopper=scope.launch{l.stop()};v.current();check(stopper.isCompleted);val finalBatches=cb.batches;val writes=ProfileManager.writes;repeat(3){v.tick()};check(p.box.calls==pre);check(ProfileManager.writes==writes)
 val expectedTicks=if(interval==0)0 else 10;check(pre==expectedTicks*2*(n+1));check(speed==if(fg)expectedTicks else 0);check(notes==if(screen)expectedTicks else 0);check(writes==if(stats&&interval!=0)n else 0)
 emit(name,"\"trafficData_constructions_during_loop\":$created,\"db_writes_during_loop\":$writesDuringLoop,\"ticks\":$expectedTicks,\"queryStats_boundary_calls\":$pre,\"speed_callbacks\":$speed,\"periodic_traffic_batches\":$bat,\"stop_total_batches\":$finalBatches,\"notification_calls\":$notes,\"formatter_calls\":$fmt,\"db_write_boundary_calls_at_stop\":$writes,\"post_stop_queries\":${p.box.calls-pre}")
 notif.destroy();scope.cancel()
}

fun extraProbes(){
 val s=FakeService();val n=ServiceNotification(s,"synthetic","fixture");s.data.notification=n
 androidx.core.app.NotificationManagerCompat.notifications=0;android.text.format.Formatter.calls=0
 val same=SpeedDisplayData()
 runBlocking{repeat(100){n.postNotificationSpeedUpdate(same)}}
 check(androidx.core.app.NotificationManagerCompat.notifications==100)
 emit("notification_identical_snapshot_100","\"notification_boundary_calls\":100,\"formatter_calls\":${android.text.format.Formatter.calls},\"identical_display_snapshots\":100")
 n.onReceive(s,android.content.Intent(android.content.Intent.ACTION_SCREEN_OFF));check(!n.listenPostSpeed)
 n.onReceive(s,android.content.Intent(android.content.Intent.ACTION_SCREEN_ON));check(n.listenPostSpeed)
 n.destroy();check(!n.listenPostSpeed)
 // Same exact six-value snapshot suppression at an external boundary adapter.
 val s2=FakeService();val dedup=ServiceNotification(s2,"synthetic","fixture");androidx.core.app.NotificationManagerCompat.notifications=0
 var last:SpeedDisplayData?=null
 runBlocking{repeat(100){if(last!=same){dedup.postNotificationSpeedUpdate(same);last=same.copy()}}}
 check(androidx.core.app.NotificationManagerCompat.notifications==1)
 emit("external_exact_snapshot_dedup_comparison","\"baseline_notify_calls\":100,\"adapter_notify_calls\":1,\"feature_limit\":\"identical_all_six_raw_values_only_title_actions_not_intercepted\"")
 dedup.destroy()
 emit("notification_screen_cycle_destroy","\"off_on_destroy_assertions\":3,\"post_destroy_listen\":false")
 DataStore.speedInterval=10;DataStore.profileTrafficStatistics=true
 val v=Virtual();val owner=CoroutineScope(SupervisorJob()+v);s.data.proxy=ProxyInstance();s.data.proxy!!.config.trafficMap["tag"]=listOf(ProxyEntity(1));s.data.notification=null
 val l=TrafficLooper(s.data,owner);l.start();v.current();val before=s.data.proxy!!.box.calls
 owner.cancel();v.current();repeat(5){v.tick()};check(before==s.data.proxy!!.box.calls)
 emit("traffic_owner_cancel","\"post_cancel_query_calls\":0")
 val b=ManagementFactory.getThreadMXBean() as com.sun.management.ThreadMXBean;val id=Thread.currentThread().id
 repeat(1000){io.nekohasekai.sagernet.ktx.Logs.d("synthetic")}
 val cpu=b.currentThreadCpuTime;val alloc=b.getThreadAllocatedBytes(id);val start=System.nanoTime();val count=10000
 repeat(count){io.nekohasekai.sagernet.ktx.Logs.d("synthetic")}
 emit("logger_disabled_sink_boundary_benchmark","\"iterations\":$count,\"native_sink_calls\":${libcore.Libcore.logCalls},\"wall_ns_per_log\":${(System.nanoTime()-start).toDouble()/count},\"cpu_ns_per_log\":${(b.currentThreadCpuTime-cpu).toDouble()/count},\"allocated_bytes_per_log\":${(b.getThreadAllocatedBytes(id)-alloc).toDouble()/count},\"sink\":\"no_op_boundary_not_native_disk_measurement\"")
}

fun main(){
 extraProbes()
 scenario("foreground_screen_on",true,true)
 scenario("video_other_app_screen_on",false,true)
 scenario("background_screen_off",false,false)
 scenario("idle_background_screen_on",false,true,delta=0)
 scenario("direct_hidden_stats_off",false,true,direct=false,stats=false)
 scenario("sampling_disabled",false,true,interval=0)
 scenario("foreground_1001_profiles_batching",true,true,n=1001)
 // Actual notification constructor receives no initial power/screen query.
 val s=FakeService();androidx.core.app.NotificationManagerCompat.notifications=0
 val no=ServiceNotification(s,"synthetic","fixture");check(no.listenPostSpeed)
 runBlocking {no.postNotificationSpeedUpdate(SpeedDisplayData())};check(androidx.core.app.NotificationManagerCompat.notifications==1)
 emit("initial_screen_off_without_broadcast","\"listenPostSpeed\":${no.listenPostSpeed},\"notification_calls\":1,\"device_power_state\":\"not_consulted_by_production_constructor\"")
 no.destroy()
 // Dedupe repeated tags, preserve ignored entries and all totals.
 val b=libcore.BoxInstance();val items=listOf(TrafficUpdater.TrafficLooperData("a",lastUpdate=1),TrafficUpdater.TrafficLooperData("a",lastUpdate=1),TrafficUpdater.TrafficLooperData("b",lastUpdate=1),TrafficUpdater.TrafficLooperData("ignored",ignore=true))
 val u=TrafficUpdater(b,items);u.updateAll();check(b.calls==4);check(items[0].rx==128L&&items[1].rx==128L&&items[3].rx==0L)
 emit("updater_tag_dedup","\"queryStats_boundary_calls\":4,\"active_items\":3,\"unique_active_tags\":2")
 // Direct production CPU/allocation benchmark: lastUpdate reset ensures not same-ms early return.
 for(n in listOf(2,32,1000)){
 val box=libcore.BoxInstance();val rows=List(n){TrafficUpdater.TrafficLooperData("tag$it",lastUpdate=1)};val up=TrafficUpdater(box,rows)
 repeat(1000){rows.forEach{it.lastUpdate=1};up.updateAll()}
 val count=if(n==1000)10000 else 100000;val bean=ManagementFactory.getThreadMXBean() as com.sun.management.ThreadMXBean;val id=Thread.currentThread().id
 val cpu=bean.currentThreadCpuTime;val alloc=bean.getThreadAllocatedBytes(id);val wall=System.nanoTime()
 repeat(count){rows.forEach{it.lastUpdate=1};up.updateAll()}
 val ns=System.nanoTime()-wall;val cpuNs=bean.currentThreadCpuTime-cpu;val bytes=bean.getThreadAllocatedBytes(id)-alloc
 emit("updater_benchmark_$n","\"iterations\":$count,\"active_unique_tags\":$n,\"wall_ns_per_updateAll\":${ns.toDouble()/count},\"cpu_ns_per_updateAll\":${cpuNs.toDouble()/count},\"allocated_bytes_per_updateAll\":${bytes.toDouble()/count},\"allocation_scope\":\"HotSpot_thread_allocated_bytes_not_ART_objects\"")
 }
}
