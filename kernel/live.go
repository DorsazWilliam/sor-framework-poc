package kernel

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// hub fans the sor_live notifications out to the subscribed streams.
type hub struct {
	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

type subscriber struct {
	reach map[string][]int64
	ch    chan []byte
}

type liveKey struct {
	T   string  `json:"t"`
	IDs []int64 `json:"ids,omitempty"`
	A   []int64 `json:"a,omitempty"`
	C   string  `json:"c"`
	E   string  `json:"e"`
}

// Subscribe returns the stream of live keys visible to p, until cancel is called.
func (k *Kernel) Subscribe(p *Principal) (<-chan []byte, func()) {
	s := &subscriber{reach: p.Reach, ch: make(chan []byte, 64)}
	k.hub.mu.Lock()
	if k.hub.subs == nil {
		k.hub.subs = map[*subscriber]struct{}{}
	}
	k.hub.subs[s] = struct{}{}
	k.hub.mu.Unlock()
	return s.ch, func() {
		k.hub.mu.Lock()
		delete(k.hub.subs, s)
		k.hub.mu.Unlock()
	}
}

func (k *Kernel) publish(payload string) {
	var key liveKey
	if err := json.Unmarshal([]byte(payload), &key); err != nil {
		return
	}
	var anchor *AnchorDecl
	if t := k.tables[key.T]; t != nil {
		anchor = t.Anchor
	}
	out, _ := json.Marshal(struct {
		T   string  `json:"t"`
		IDs []int64 `json:"ids,omitempty"`
		C   string  `json:"c"`
		E   string  `json:"e"`
	}{key.T, key.IDs, key.C, key.E})
	k.hub.mu.Lock()
	defer k.hub.mu.Unlock()
	for s := range k.hub.subs {
		if anchor != nil && !slices.ContainsFunc(key.A, func(a int64) bool { return slices.Contains(s.reach[anchor.Reach], a) }) {
			continue
		}
		select {
		case s.ch <- out:
		default: // a stream that does not keep up drops keys; its client reloads on reconnect
		}
	}
}

// Listen holds one LISTEN connection and publishes every sor_live notification until ctx ends.
func (k *Kernel) Listen(ctx context.Context) {
	for ctx.Err() == nil {
		if err := k.listen(ctx); err != nil && ctx.Err() == nil {
			slog.Error("live listener", "err", err)
			time.Sleep(time.Second)
		}
	}
}

func (k *Kernel) listen(ctx context.Context) error {
	conn, err := k.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "listen sor_live"); err != nil {
		return err
	}
	defer conn.Exec(context.WithoutCancel(ctx), "unlisten sor_live")
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		k.publish(n.Payload)
	}
}
