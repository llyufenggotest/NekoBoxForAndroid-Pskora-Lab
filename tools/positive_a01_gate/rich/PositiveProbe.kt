import io.nekohasekai.sagernet.bg.*
import io.nekohasekai.sagernet.bg.proto.*
import io.nekohasekai.sagernet.database.*
import io.nekohasekai.sagernet.aidl.*
import kotlinx.coroutines.*
import java.util.PriorityQueue
import java.util.Base64
import java.lang.management.ManagementFactory
@OptIn(InternalCoroutinesApi::class)
class V:CoroutineDispatcher(),Delay {
 data class Task(val due:Long,val seq:Long,val run:Runnable):Comparable<Task>{override fun compareTo(o:Task)=compareValuesBy(this,o,{it.due},{it.seq})}
 var now=0L;var seq=0L;val q=PriorityQueue<Task>()
 override fun dispatch(context:kotlin.coroutines.CoroutineContext,block:Runnable){q.add(Task(now,seq++,block))}
 override fun scheduleResumeAfterDelay(timeMillis:Long,continuation:CancellableContinuation<Unit>){q.add(Task(now+timeMillis,seq++,Runnable {if(continuation.isActive)continuation.resume(Unit){}}))}
 fun drain(){var n=0;while(q.isNotEmpty()&&q.peek().due<=now){check(n++<20000);q.poll().run.run()}}
 fun tick(){now+=10;fixture.Clock.now+=10;drain()}
}
fun encoded(s:String)=Base64.getEncoder().encodeToString(s.toByteArray())
fun result(name:String,rest:String){println("RESULT {\"name\":\"$name\",$rest}")}
class Rig(val n:Int=4,val fg:Boolean=false,val stats:Boolean=true,val direct:Boolean=true,val delta:Long=128,val selector:Boolean=false,val duplicate:Boolean=false,val interval:Int=10) {
 val s=FakeService();val p=ProxyInstance();val cb=BaseService.Callback();val other=BaseService.Callback("other");val v=V();val owner=SupervisorJob();val scope=CoroutineScope(owner+v);val l=TrafficLooper(s.data,scope)
 init {fixture.Clock.now=1000000L;DataStore.speedInterval=interval;DataStore.profileTrafficStatistics=stats;DataStore.showDirectSpeed=direct;TrafficData.created=0;ProfileManager.writes=0;ProfileManager.resets=0;ProfileManager.rows.clear();Trace.events.clear();s.data.proxy=p;p.box.delta=delta
 repeat(n){val ent=ProxyEntity((it+1).toLong(),rx=1000L+it,tx=2000L+it);if(duplicate){val old=p.config.trafficMap["shared"]?:emptyList();p.config.trafficMap["shared"]=old+ent}else p.config.trafficMap["tag$it"]=listOf(ent)}
 if(selector){p.config.selectorGroupId=1;p.config.mainEntId=1}
 s.data.binder.callbackIdMap[cb]=if(fg)2 else 3;s.data.binder.callbackIdMap[other]=3
 }
 fun start(){l.start();v.drain()}
 fun command(f:suspend ()->Unit){val j=scope.launch{f()};v.drain();check(j.isCompleted)}
 fun stop(){command{l.stop()}}
 fun state():String {
  val f=l.javaClass.getDeclaredField("idMap");f.isAccessible=true
  @Suppress("UNCHECKED_CAST") val map=f.get(l) as Map<Long,TrafficUpdater.TrafficLooperData>
  return map.toSortedMap().entries.joinToString(";"){(id,x)->"$id:${x.tag},${x.tx},${x.rx},${x.txBase},${x.rxBase},${x.txRate},${x.rxRate},${x.ignore},${x.hasTrafficDelta},${x.lastUpdate}"}+"|dao="+ProfileManager.rows.toSortedMap().entries.joinToString()+"|entities="+p.config.trafficMap.values.flatten().joinToString{"${it.id},${it.rx},${it.tx}"}+"|selector=${l.selectorNowId},${l.selectorNowFakeTag}"
 }
}
fun one(name:String,n:Int=4,fg:Boolean=false,stats:Boolean=true,direct:Boolean=true,delta:Long=128,selector:Boolean=false,duplicate:Boolean=false,interval:Int=10,change:(Rig,Int)->Unit={_,_->}){
 Trace.enabled=true;val r=Rig(n,fg,stats,direct,delta,selector,duplicate,interval);r.start();val states=ArrayList<String>();states.add(r.state())
 repeat(9){i->change(r,i);r.v.tick();states.add(r.state())}
 val dto=TrafficData.created;val pre=r.p.box.calls;val preState=r.state();check(selector || ProfileManager.writes==0)
 r.stop();val writes=ProfileManager.writes;val events=Trace.events.toList();val finalState=r.state();repeat(4){r.v.tick()};check(pre==r.p.box.calls);check(writes==ProfileManager.writes)
 result(name,"\"dto\":$dto,\"queries\":$pre,\"stop_dao_writes\":$writes,\"resets\":${ProfileManager.resets},\"trace_b64\":\"${encoded(events.joinToString("\n"))}\",\"states_b64\":\"${encoded(states.joinToString("\n"))}\",\"final_b64\":\"${encoded(finalState)}\",\"post_stop_queries\":${r.p.box.calls-pre}")
 r.owner.cancel();r.v.drain()
}
fun lifecycle(){
 one("foreground")
 one("foreground_actual",fg=true)
 one("background_screen_off")
 one("idle",delta=0)
 one("stats_disabled",stats=false)
 one("direct_hidden",fg=true,direct=false)
 one("sampling_disabled",interval=0)
 one("foreground_1001",n=1001,fg=true)
 one("duplicate_tags",fg=true,duplicate=true)
 one("disconnected",fg=true,change={r,i->if(i==0)r.s.data.state=BaseService.State.Stopped})
 one("register_unregister",change={r,i->when(i){1->r.s.data.binder.callbackIdMap[r.cb]=2;4->r.s.data.binder.callbackIdMap.remove(r.cb);6->r.s.data.binder.callbackIdMap[r.cb]=2;7->r.s.data.binder.callbackIdMap[r.cb]=3}})
 one("foreground_state_changes",change={r,i->when(i){0->r.s.data.binder.callbackIdMap[r.cb]=2;2->r.s.data.state=BaseService.State.Stopped;4->r.s.data.state=BaseService.State.Connected;6->r.s.data.binder.callbackIdMap[r.cb]=3}})
 one("selector_reset",fg=true,selector=true,change={r,i->when(i){1->r.command{r.l.selectMain(2)};3->r.command{r.l.resetTraffic(longArrayOf(2,-1,0,2))};6->r.command{r.l.selectMain(1)}}})
 // Suspension at Binder mutex/broadcast boundary admits a real queued coroutine unsubscribe before callback delivery.
 for(action in listOf("unsubscribe","demote","cancel_owner","stop")){
  Trace.enabled=true;val r=Rig(fg=true);r.start();val before=Trace.events.size
  val entered=CompletableDeferred<Unit>();val resume=CompletableDeferred<Unit>()
  r.s.data.binder.beforeBroadcast={entered.complete(Unit);resume.await()}
  r.v.tick();check(entered.isCompleted)
  when(action){"unsubscribe"->r.scope.launch{r.s.data.binder.callbackIdMap.remove(r.cb);resume.complete(Unit)};"demote"->r.scope.launch{r.s.data.binder.callbackIdMap[r.cb]=3;resume.complete(Unit)};"cancel_owner"->r.owner.cancel();"stop"->{r.s.data.binder.beforeBroadcast=null;r.scope.launch{r.l.stop()}}}
  r.v.drain();r.s.data.binder.beforeBroadcast=null
  if(action!="cancel_owner"){r.stop()}
  val calls=r.p.box.calls;repeat(3){r.v.tick()};check(calls==r.p.box.calls)
  result("suspended_$action","\"queries\":$calls,\"trace_b64\":\"${encoded(Trace.events.joinToString("\n"))}\",\"final_b64\":\"${encoded(r.state())}\",\"post_stop_queries\":0")
  r.owner.cancel();r.v.drain()
 }
 // Cancellation while the synchronous native query is executing reaches ensureActive before DTOs/callbacks.
 Trace.enabled=true;val r=Rig(fg=true);r.start();val initial=r.cb.speeds;var once=false;r.p.box.hook={if(!once){once=true;r.owner.cancel()}};r.v.tick();check(r.cb.speeds==initial);val calls=r.p.box.calls;repeat(5){r.v.tick()};check(calls==r.p.box.calls)
 result("native_query_cancel","\"queries\":$calls,\"trace_b64\":\"${encoded(Trace.events.joinToString("\n"))}\",\"final_b64\":\"${encoded(r.state())}\",\"post_cancel_queries\":0")
 // Actual second JVM thread mutates only while producer is held inside native boundary.
 for(action in listOf("unsubscribe","demote","promote","cancel")){
  Trace.enabled=true;val t=Rig(fg=action!="promote");t.start();var fired=false
  t.p.box.hook={if(!fired){fired=true;val ready=java.util.concurrent.CountDownLatch(1);val done=java.util.concurrent.CountDownLatch(1);val worker=Thread {ready.await();when(action){"unsubscribe"->t.s.data.binder.callbackIdMap.remove(t.cb);"demote"->t.s.data.binder.callbackIdMap[t.cb]=3;"promote"->t.s.data.binder.callbackIdMap[t.cb]=2;"cancel"->t.owner.cancel()};done.countDown()};worker.start();ready.countDown();check(done.await(5,java.util.concurrent.TimeUnit.SECONDS));worker.join()}}
  t.v.tick();t.p.box.hook=null
  if(action!="cancel")t.stop()
  val sampled=t.p.box.calls;repeat(3){t.v.tick()};check(sampled==t.p.box.calls)
  result("threaded_$action","\"queries\":$sampled,\"trace_b64\":\"${encoded(Trace.events.joinToString("\n"))}\",\"final_b64\":\"${encoded(t.state())}\",\"post_stop_queries\":0")
  t.owner.cancel();t.v.drain()
 }
}
fun benchmark(){
 Trace.enabled=false
 for(n in listOf(4,128,1001)){
  val r=Rig(n=n);r.start();repeat(5000){r.v.tick()};TrafficData.created=0
  val iterations=if(n==4)200000 else if(n==128)30000 else 6000
  val b=ManagementFactory.getThreadMXBean() as com.sun.management.ThreadMXBean;check(b.isThreadAllocatedMemorySupported);b.isThreadAllocatedMemoryEnabled=true;val id=Thread.currentThread().id
  val cpu=b.currentThreadCpuTime;val alloc=b.getThreadAllocatedBytes(id);val start=System.nanoTime();val initialCalls=r.p.box.calls
  repeat(iterations){r.v.tick()}
  val wall=System.nanoTime()-start;val cpuNs=b.currentThreadCpuTime-cpu;val bytes=b.getThreadAllocatedBytes(id)-alloc
  result("loop_bench_$n","\"profiles\":$n,\"iterations\":$iterations,\"cpu_ns_per_tick\":${cpuNs.toDouble()/iterations},\"wall_ns_per_tick\":${wall.toDouble()/iterations},\"allocated_bytes_per_tick\":${bytes.toDouble()/iterations},\"dto\":${TrafficData.created},\"queries\":${r.p.box.calls-initialCalls},\"state_b64\":\"${encoded(r.state())}\"")
  r.stop();r.owner.cancel();r.v.drain()
 }
}
fun retained(){Trace.enabled=false;val r=Rig(n=1001);r.start();repeat(10){r.v.tick()};System.gc();println("READY ${ProcessHandle.current().pid()} ${encoded(r.state())}");System.out.flush();readLine();r.stop();r.owner.cancel();r.v.drain()}
fun main(args:Array<String>){when(args[0]){"lifecycle"->lifecycle();"benchmark"->benchmark();"retained"->retained();else->error("unknown action")}}
