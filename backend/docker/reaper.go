package docker

import (
	"context"
	"time"
)

// Reaper periodically scans the Docker daemon for orphaned sandbox containers
// and removes them to prevent host resource leaks after crashes.
type Reaper struct {
	client   *Client
	interval time.Duration
	stopCh   chan struct{}
	doneCh   chan struct{}
}

func newReaper(client *Client, interval time.Duration) *Reaper {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	r := &Reaper{
		client:   client,
		interval: interval,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	go r.run()
	return r
}

func (r *Reaper) run() {
	defer close(r.doneCh)
	// Initial cleanup on boot
	r.reapOnce()

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.reapOnce()
		}
	}
}

func (r *Reaper) reapOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ids, err := r.client.ListSandboxes(ctx)
	if err != nil {
		return
	}

	for _, id := range ids {
		info, err := r.client.ContainerInspect(ctx, id)
		if err != nil {
			continue
		}
		// If container has stopped or finished, remove it
		if !info.State.Running {
			_ = r.client.ContainerRemove(ctx, id)
		}
	}
}

func (r *Reaper) Close() {
	close(r.stopCh)
	<-r.doneCh
}
