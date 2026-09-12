package latencyaudit

import (
 "context"
 "crypto/tls"
 "crypto/x509"
 "net"
 "net/http"
 "net/http/httptest"
 "sync"
 "testing"
 "time"

 "github.com/matsuridayo/libneko/speedtest"
 "github.com/sagernet/sing-box/adapter"
 "github.com/sagernet/sing-box/common/urltest"
 M "github.com/sagernet/sing/common/metadata"
 "github.com/sagernet/sing/service"
)

type certStore struct { adapter.CertificateStore; pool *x509.CertPool }
func(s certStore) Pool()*x509.CertPool{return s.pool}
type delayedDialer struct { delay time.Duration; mu sync.Mutex; calls int }
func(d *delayedDialer) DialContext(ctx context.Context, network string, addr M.Socksaddr)(net.Conn,error){
 d.mu.Lock(); d.calls++; d.mu.Unlock()
 select {case <-time.After(d.delay): case <-ctx.Done(): return nil,ctx.Err()}
 return (&net.Dialer{}).DialContext(ctx,network,addr.String())
}
func(d *delayedDialer) ListenPacket(context.Context,M.Socksaddr)(net.PacketConn,error){panic("unused")}

// Runs the production functions against the same real local HTTP(S) socket.
// The injected setup delay is a controlled variable, not a real-node benchmark.
func TestLatencyDefinitions(t *testing.T){
 for _,secure:=range []bool{false,true}{ for _,keepAlive:=range []bool{true,false}{
  name:="http";if secure{name="https"};if !keepAlive{name+="_close"}
  t.Run(name,func(t *testing.T){
   var mu sync.Mutex; var methods,peers []string
   server:=httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
    mu.Lock(); methods=append(methods,r.Method);peers=append(peers,r.RemoteAddr);mu.Unlock()
    time.Sleep(40*time.Millisecond);if !keepAlive{w.Header().Set("Connection","close")};w.WriteHeader(204)
   }))
   if secure{server.StartTLS()}else{server.Start()};defer server.Close()
   roots:=server.Client().Transport.(*http.Transport).TLSClientConfig
   if roots==nil{roots=&tls.Config{}}
   for repeat:=0;repeat<3;repeat++{
    mu.Lock();methods=nil;peers=nil;mu.Unlock()
    dashDial:=&delayedDialer{delay:180*time.Millisecond}
    tr:=&http.Transport{TLSClientConfig:roots.Clone(),DialContext:func(ctx context.Context,n,a string)(net.Conn,error){return dashDial.DialContext(ctx,n,M.ParseSocksaddr(a))}}
    begin:=time.Now();rtt,err:=speedtest.UrlTest(&http.Client{Transport:tr},server.URL,5000,speedtest.UrlTestStandard_RTT);wall:=time.Since(begin).Milliseconds();if err!=nil{t.Fatal(err)}
    mu.Lock();reused:=len(peers)==2&&peers[0]==peers[1];dashMethods:=append([]string(nil),methods...);methods=nil;peers=nil;mu.Unlock()
    autoDial:=&delayedDialer{delay:180*time.Millisecond};ctx:=service.ContextWithDefaultRegistry(context.Background());if secure{service.MustRegister[adapter.CertificateStore](ctx,certStore{pool:roots.RootCAs})}
    begin=time.Now();auto,err:=urltest.URLTest(ctx,server.URL,autoDial);autoWall:=time.Since(begin).Milliseconds();if err!=nil{t.Fatal(err)}
    mu.Lock();autoMethods:=append([]string(nil),methods...);mu.Unlock()
    t.Logf("run=%d dashboard_ms=%d wall_ms=%d dials=%d methods=%v reused=%t auto_ms=%d wall_ms=%d dials=%d methods=%v",repeat,rtt,wall,dashDial.calls,dashMethods,reused,auto,autoWall,autoDial.calls,autoMethods)
    if int(auto)-int(rtt)<120{t.Fatalf("setup must be included only in auto: auto=%d rtt=%d",auto,rtt)}
    if len(dashMethods)!=2||dashMethods[0]!="GET"||len(autoMethods)!=1||autoMethods[0]!="HEAD"{t.Fatal("request contract changed")}
    if reused!=keepAlive{t.Fatal("unexpected connection reuse")}
   }
  })
 }}
}
