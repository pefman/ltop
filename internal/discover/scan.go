// Package discover finds llama.cpp servers listening on the local machine.
package discover

import (
	"context"
	"errors"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/pefman/ltop/internal/llama"
)

// CandidatePorts are the ports llama.cpp and its common wrappers listen on.
// 11434 is included because Ollama squats there and must be identified so it
// can be rejected rather than silently monitored.
var CandidatePorts = []int{11436, 8080, 8000, 8081, 8090, 1234, 5000, 11434}

// Found describes a reachable llama.cpp server.
type Found struct {
	Endpoint  string
	Model     string
	Build     string
	HasSlots  bool
	HasMetric bool
	// NeedsAuth reports that the server answered but demanded an API key.
	NeedsAuth bool
}

// Scan probes localhost for llama.cpp servers. Ports are dialled concurrently
// and only endpoints answering /props are reported, which excludes Ollama and
// other OpenAI-compatible servers that ltop cannot read telemetry from.
func Scan(ctx context.Context, ports []int) []Found {
	if len(ports) == 0 {
		ports = CandidatePorts
	}

	var (
		mu    sync.Mutex
		found []Found
		wg    sync.WaitGroup
	)
	for _, port := range ports {
		wg.Add(1)
		go func(port int) {
			defer wg.Done()
			f, ok := probe(ctx, port)
			if !ok {
				return
			}
			mu.Lock()
			found = append(found, f)
			mu.Unlock()
		}(port)
	}
	wg.Wait()

	sort.Slice(found, func(i, j int) bool { return found[i].Endpoint < found[j].Endpoint })
	return found
}

func probe(ctx context.Context, port int) (Found, bool) {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	// A TCP dial first keeps closed ports from costing a full HTTP timeout.
	dialCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return Found{}, false
	}
	_ = conn.Close()

	endpoint := "http://" + addr
	c := llama.New(endpoint, "")

	propsCtx, cancelProps := context.WithTimeout(ctx, 2*time.Second)
	defer cancelProps()
	props, err := c.Props(propsCtx)
	if err != nil {
		// A server behind --api-key still identifies itself as worth offering.
		if errors.Is(err, llama.ErrUnauthorized) {
			return Found{Endpoint: endpoint, NeedsAuth: true}, true
		}
		return Found{}, false
	}
	if props.ModelPath == "" {
		return Found{}, false
	}

	return Found{
		Endpoint:  endpoint,
		Model:     props.ModelName(),
		Build:     props.BuildInfo,
		HasSlots:  props.EndpointSlots,
		HasMetric: props.EndpointMetrics,
	}, true
}
