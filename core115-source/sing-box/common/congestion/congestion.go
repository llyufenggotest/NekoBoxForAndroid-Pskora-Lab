package congestion

import (
	"time"

	"github.com/sagernet/quic-go"
	qcongestion "github.com/sagernet/quic-go/congestion"
	congestion_meta2 "github.com/sagernet/sing-box/common/congestion/legacybbr"
	congestion_meta1 "github.com/sagernet/sing-quic/congestion_meta1"
	E "github.com/sagernet/sing/common/exceptions"
)

func NewCongestionControl(name string, cwnd int, timeFunc func() time.Time) (func(conn *quic.Conn) qcongestion.CongestionControl, error) {
	if timeFunc == nil {
		timeFunc = time.Now
	}
	if cwnd == 0 {
		cwnd = 32
	}
	switch name {
	case "", "bbr":
		return func(conn *quic.Conn) qcongestion.CongestionControl {
			return congestion_meta2.NewPrivateSender(
				conn.InitialPacketSize(),
				qcongestion.ByteCount(cwnd),
			)
		}, nil
	case "cubic":
		return func(conn *quic.Conn) qcongestion.CongestionControl {
			return congestion_meta1.NewCubicSender(
				congestion_meta1.DefaultClock{TimeFunc: timeFunc},
				conn.InitialPacketSize(),
				false,
			)
		}, nil
	case "reno":
		return func(conn *quic.Conn) qcongestion.CongestionControl {
			return congestion_meta1.NewCubicSender(
				congestion_meta1.DefaultClock{TimeFunc: timeFunc},
				conn.InitialPacketSize(),
				true,
			)
		}, nil
	default:
		return nil, E.New("unknown congestion control: ", name)
	}
}
