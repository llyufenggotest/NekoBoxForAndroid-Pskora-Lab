package multiuser

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
)

const (
	ParallelThreshold  = 8
	MaxParallelWorkers = 8
	PriorityLimit      = 8
	decayInterval      = 1024
	decayUnderflowAge  = 2048
	coverageEnter      = 90
	coverageExit       = 89
	coverageScale      = 100
)

type ranking struct {
	order          [PriorityLimit]int
	orderCount     int
	serialHotCount int
}

func (r *ranking) priority() []int {
	if r == nil {
		return nil
	}
	return r.order[:r.orderCount]
}

type usage struct {
	value float64
	epoch uint64
}

type Authenticator struct {
	credentials           [][]byte
	usageAccess           sync.Mutex
	usage                 []usage
	usageTotal            float64
	usageEvents           uint64
	usageEpoch            uint64
	order                 []int
	positions             []int
	serialHotCount        int
	ranking               atomic.Pointer[ranking]
	activeAuthentications atomic.Int32
}

var errNoMatchingPSK = errors.New("snell: authentication failed: no matching PSK")

func New(credentials [][]byte) *Authenticator {
	credentialCopy := make([][]byte, len(credentials))
	for index, credential := range credentials {
		credentialCopy[index] = append([]byte(nil), credential...)
	}
	order := make([]int, min(len(credentials), PriorityLimit))
	positions := make([]int, len(credentials))
	for index := range positions {
		positions[index] = -1
	}
	for index := range order {
		order[index] = index
		positions[index] = index
	}
	return &Authenticator{
		credentials: credentialCopy,
		usage:       make([]usage, len(credentials)),
		order:       order,
		positions:   positions,
	}
}

func (a *Authenticator) Authenticate(match func([]byte) bool) ([]byte, int, error) {
	credential, index, _, err := authenticate(a, booleanMatcher(match))
	return credential, index, err
}

// AuthenticateWithResult returns the value produced while authenticating the
// matched credential, allowing callers to reuse expensive derived state.
// match may be called concurrently for different credentials.
func AuthenticateWithResult[T any](a *Authenticator, match func([]byte) (T, bool)) ([]byte, int, T, error) {
	return authenticate(a, resultMatcher[T](match))
}

type credentialMatcher[T any] interface {
	Match(candidate []byte) (T, bool)
}

type booleanMatcher func(candidate []byte) bool

func (m booleanMatcher) Match(candidate []byte) (struct{}, bool) {
	return struct{}{}, m(candidate)
}

type resultMatcher[T any] func(candidate []byte) (T, bool)

func (m resultMatcher[T]) Match(candidate []byte) (T, bool) {
	return m(candidate)
}

func authenticate[T any, M credentialMatcher[T]](a *Authenticator, match M) ([]byte, int, T, error) {
	allowParallel := a.activeAuthentications.Add(1) == 1
	defer a.activeAuthentications.Add(-1)
	var zero T

	snapshot := a.ranking.Load()
	var priority []int
	var serialHotCount int
	if snapshot != nil {
		priority = snapshot.priority()
		serialHotCount = snapshot.serialHotCount
	}
	serialHotCount = min(max(serialHotCount, 0), len(priority))
	for _, index := range priority[:serialHotCount] {
		result, loaded := match.Match(a.credentials[index])
		if loaded {
			a.record(index)
			return a.credentials[index], index, result, nil
		}
	}

	var matched int
	var matchedResult T
	var loaded bool
	candidateCount := len(a.credentials) - serialHotCount
	if allowParallel && candidateCount >= ParallelThreshold {
		matched, matchedResult, loaded = matchParallel(a, priority, serialHotCount, candidateCount, match)
	} else {
		for _, index := range priority[serialHotCount:] {
			result, candidateLoaded := match.Match(a.credentials[index])
			if candidateLoaded {
				matched, matchedResult, loaded = index, result, true
				break
			}
		}
		if !loaded {
			for index := range a.credentials {
				if contains(priority, index) {
					continue
				}
				result, candidateLoaded := match.Match(a.credentials[index])
				if candidateLoaded {
					matched, matchedResult, loaded = index, result, true
					break
				}
			}
		}
	}
	if !loaded {
		return nil, -1, zero, errNoMatchingPSK
	}
	a.record(matched)
	return a.credentials[matched], matched, matchedResult, nil
}

type matchResult[T any] struct {
	index int
	value T
}

func matchParallel[T any, M credentialMatcher[T]](
	a *Authenticator,
	priority []int,
	serialHotCount int,
	candidateCount int,
	match M,
) (int, T, bool) {
	result := make(chan matchResult[T], 1)
	priorityTail := priority[serialHotCount:]
	var cursor atomic.Uint64
	var found atomic.Bool
	var group sync.WaitGroup
	workerCount := min(candidateCount, MaxParallelWorkers)
	group.Add(workerCount)
	for range workerCount {
		go func() {
			defer group.Done()
			for !found.Load() {
				position := int(cursor.Add(1) - 1)
				if position >= len(priorityTail)+len(a.credentials) {
					return
				}
				var index int
				if position < len(priorityTail) {
					index = priorityTail[position]
				} else {
					index = position - len(priorityTail)
					if contains(priority, index) {
						continue
					}
				}
				value, loaded := match.Match(a.credentials[index])
				if loaded && found.CompareAndSwap(false, true) {
					result <- matchResult[T]{index: index, value: value}
					return
				}
			}
		}()
	}
	group.Wait()
	select {
	case matched := <-result:
		return matched.index, matched.value, true
	default:
		var zero T
		return 0, zero, false
	}
}

func contains(indexes []int, target int) bool {
	for _, index := range indexes {
		if index == target {
			return true
		}
	}
	return false
}

func (a *Authenticator) record(index int) {
	a.usageAccess.Lock()
	defer a.usageAccess.Unlock()
	a.usageEvents++
	if a.usageEvents >= decayInterval {
		a.usageEvents = 0
		a.usageEpoch++
		a.usageTotal *= 0.5
	}
	for _, rankedIndex := range a.order {
		a.normalize(rankedIndex)
	}
	a.normalize(index)
	a.usage[index].value++
	a.usageTotal++

	position := a.positions[index]
	if position < 0 {
		position = len(a.order) - 1
		previous := a.order[position]
		if a.usage[index].value <= a.usage[previous].value {
			a.publish(a.calculateSerialHotCount())
			return
		}
		a.positions[previous] = -1
		a.order[position] = index
		a.positions[index] = position
	}
	for position > 0 && a.usage[index].value > a.usage[a.order[position-1]].value {
		previous := a.order[position-1]
		a.order[position] = previous
		a.positions[previous] = position
		position--
	}
	a.order[position] = index
	a.positions[index] = position
	a.publish(a.calculateSerialHotCount())
}

func (a *Authenticator) normalize(index int) {
	entry := &a.usage[index]
	elapsed := a.usageEpoch - entry.epoch
	if elapsed == 0 {
		return
	}
	if elapsed >= decayUnderflowAge {
		entry.value = 0
	} else {
		entry.value = math.Ldexp(entry.value, -int(elapsed))
	}
	entry.epoch = a.usageEpoch
}

func (a *Authenticator) publish(serialHotCount int) {
	orderCount := len(a.order)
	current := a.ranking.Load()
	if current != nil && current.orderCount == orderCount && current.serialHotCount == serialHotCount {
		unchanged := true
		for index := range orderCount {
			if current.order[index] != a.order[index] {
				unchanged = false
				break
			}
		}
		if unchanged {
			return
		}
	}
	a.serialHotCount = serialHotCount
	next := &ranking{orderCount: orderCount, serialHotCount: serialHotCount}
	copy(next.order[:], a.order)
	a.ranking.Store(next)
}

func (a *Authenticator) calculateSerialHotCount() int {
	if a.usageTotal == 0 {
		return 0
	}
	count := a.capSerialHotCount(a.popularityPrefix(coverageEnter))
	if count > 0 {
		return count
	}
	if a.serialHotCount > 0 {
		return a.capSerialHotCount(a.popularityPrefix(coverageExit))
	}
	return 0
}

func (a *Authenticator) popularityPrefix(coverage float64) int {
	var cumulative float64
	for index := range a.order {
		cumulative += a.usage[a.order[index]].value
		if cumulative*coverageScale >= a.usageTotal*coverage {
			if index+1 == len(a.usage) {
				return 0
			}
			return index + 1
		}
	}
	return 0
}

func (a *Authenticator) capSerialHotCount(count int) int {
	if len(a.usage) > ParallelThreshold {
		count = min(count, len(a.usage)-ParallelThreshold)
	}
	return count
}
