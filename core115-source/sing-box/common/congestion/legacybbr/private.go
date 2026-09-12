package congestion

import "github.com/sagernet/quic-go/congestion"

// NewPrivateSender retains XHTTP's configurable initial congestion window
// on the current upstream monotonic-clock congestion API.
func NewPrivateSender(size, packets congestion.ByteCount) congestion.CongestionControl {
	return newBbrSender(size, packets*size, congestion.MaxCongestionWindowPackets*size, ProfileStandard)
}
