package group

import "github.com/sagernet/sing-box/adapter"

// RuntimeMemberSample is an actual successful leaf URLTest history entry.
// Missing/deleted history is unknown, NOT zero latency or an inferred failure.
type RuntimeMemberSample struct {
	Tag        string `json:"tag"`
	Delay      uint16 `json:"delay"`
	SampleTime int64  `json:"sampleTime"`
	Source     string `json:"source"`
}

// RuntimeMemberHistory never tests, selects, or substitutes a group's delay.
func (s *URLTest) RuntimeMemberHistory() []RuntimeMemberSample {
	samples := make([]RuntimeMemberSample, 0)
	if s.group == nil {
		return samples
	}
	seen := make(map[string]bool)
	for _, member := range s.group.outbounds {
		if _, grouped := member.(adapter.OutboundGroup); grouped {
			continue
		}
		tag := member.Tag()
		if seen[tag] {
			continue
		}
		seen[tag] = true
		h := s.group.history.LoadURLTestHistory(tag)
		if h == nil || h.Time.IsZero() {
			continue
		}
		samples = append(samples, RuntimeMemberSample{tag, h.Delay, h.Time.UnixMilli(), "auto_urltest"})
	}
	return samples
}
