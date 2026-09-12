package tun
import("context";"fmt";"net/netip";"strings";"testing";"time")
type natCapture struct{lines []string}
func(c *natCapture) DiagnosticEnabled()bool{return true}
func(c *natCapture) Debug(a ...any){c.lines=append(c.lines,fmt.Sprint(a...))}
func TestDiagnosticNATNoBehaviorChange(t *testing.T){ctx,cancel:=context.WithCancel(context.Background());defer cancel();l:=&natCapture{};n:=NewNat(ctx,time.Hour,l);src:=netip.MustParseAddrPort("192.0.2.1:12345");dst:=netip.MustParseAddrPort("198.51.100.2:443");p:=n.Lookup(src,dst);if p!=10000||n.Lookup(src,dst)!=p||n.LookupBack(p)==nil{t.Fatal("NAT changed")};n.Purge();if n.LookupBack(p)!=nil{t.Fatal("purge")};text:=strings.Join(l.lines,"\n");if !strings.Contains(text,"event=8")||!strings.Contains(text,"event=9")||strings.Contains(text,"192.0.2")||strings.Contains(text,"198.51"){t.Fatal(text)}}
