package main

import (
	"context"
	"fmt"
	"sort"
	"sync"

	qbt "github.com/autobrr/go-qbittorrent"
	"golang.org/x/sync/errgroup"
)

// node pairs a configured instance name with its API client.
type node struct {
	name   string
	client *qbt.Client
}

// Cluster presents several qBittorrent instances as one. It fans command out to
// the underlying clients concurrently and aggregates results and errors.
type Cluster struct {
	nodes []node

	loginOnce sync.Once
	loginErr  error
}

// newCluster builds a Cluster from config. If only is non-empty, the cluster is
// scoped to that single named instance.
func newCluster(cfg *Config, only string) (*Cluster, error) {
	var nodes []node
	for _, in := range cfg.Instances {
		if only != "" && in.Name != only {
			continue
		}
		c := qbt.NewClient(qbt.Config{
			Host:          in.Host,
			Username:      in.Username,
			Password:      in.Password,
			TLSSkipVerify: in.TLSSkipVerify,
			BasicUser:     in.BasicUser,
			BasicPass:     in.BasicPass,
			Log:           clientLogger(),
		})
		nodes = append(nodes, node{name: in.Name, client: c})
	}
	if only != "" && len(nodes) == 0 {
		return nil, fmt.Errorf("no instance named %q in config", only)
	}
	return &Cluster{nodes: nodes}, nil
}

// login authenticates every node once. A node that fails to log in is dropped
// with a warning so the rest of the cluster stays usable; an error is returned
// only when no node could log in.
func (cl *Cluster) login(ctx context.Context) error {
	cl.loginOnce.Do(func() {
		debugf("logging in to %d instance(s)", len(cl.nodes))
		var live []node
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, n := range cl.nodes {
			wg.Add(1)
			go func(n node) {
				defer wg.Done()
				if err := n.client.LoginCtx(ctx); err != nil {
					warnf("instance %q: login failed: %v", n.name, err)
					return
				}
				debugf("instance %q: login ok", n.name)
				mu.Lock()
				live = append(live, n)
				mu.Unlock()
			}(n)
		}
		wg.Wait()

		if len(live) == 0 {
			cl.loginErr = fmt.Errorf("no instances reachable")
			return
		}
		// Preserve config order for stable output.
		order := map[string]int{}
		for i, n := range cl.nodes {
			order[n.name] = i
		}
		sort.Slice(live, func(i, j int) bool { return order[live[i].name] < order[live[j].name] })
		cl.nodes = live
	})
	return cl.loginErr
}

// AnnotatedTorrent is a torrent tagged with the instance that holds it.
type AnnotatedTorrent struct {
	qbt.Torrent
	Instance string `json:"instance"`
}

// ListAll fans GetTorrents out to every node and merges the results. A failing
// node yields a warning but does not fail the call.
func (cl *Cluster) ListAll(ctx context.Context, opts qbt.TorrentFilterOptions) ([]AnnotatedTorrent, error) {
	if err := cl.login(ctx); err != nil {
		return nil, err
	}

	var mu sync.Mutex
	var out []AnnotatedTorrent
	g, ctx := errgroup.WithContext(ctx)
	for _, n := range cl.nodes {
		n := n
		g.Go(func() error {
			ts, err := n.client.GetTorrentsCtx(ctx, opts)
			if err != nil {
				warnf("instance %q: list failed: %v", n.name, err)
				return nil
			}
			debugf("instance %q: %d torrent(s)", n.name, len(ts))
			mu.Lock()
			for _, t := range ts {
				out = append(out, AnnotatedTorrent{Torrent: t, Instance: n.name})
			}
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

// action applies fn to every node concurrently, collecting per-node errors.
// It is the shared fan-out path for pause/resume/delete.
func (cl *Cluster) action(ctx context.Context, verb string, fn func(context.Context, *qbt.Client) error) error {
	if err := cl.login(ctx); err != nil {
		return err
	}

	var failed int
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(ctx)
	for _, n := range cl.nodes {
		n := n
		g.Go(func() error {
			if err := fn(ctx, n.client); err != nil {
				warnf("instance %q: %s failed: %v", n.name, verb, err)
				mu.Lock()
				failed++
				mu.Unlock()
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	if failed == len(cl.nodes) {
		return fmt.Errorf("%s failed on all instances", verb)
	}
	return nil
}

// Pause pauses the given hashes on whichever instances hold them. qBittorrent
// ignores hashes it does not know, so fanning out is safe.
func (cl *Cluster) Pause(ctx context.Context, hashes []string) error {
	return cl.action(ctx, "pause", func(ctx context.Context, c *qbt.Client) error {
		return c.PauseCtx(ctx, hashes)
	})
}

// Resume resumes the given hashes on whichever instances hold them.
func (cl *Cluster) Resume(ctx context.Context, hashes []string) error {
	return cl.action(ctx, "resume", func(ctx context.Context, c *qbt.Client) error {
		return c.ResumeCtx(ctx, hashes)
	})
}

// Delete removes the given hashes (optionally with files) from whichever
// instances hold them.
func (cl *Cluster) Delete(ctx context.Context, hashes []string, deleteFiles bool) error {
	return cl.action(ctx, "delete", func(ctx context.Context, c *qbt.Client) error {
		return c.DeleteTorrentsCtx(ctx, hashes, deleteFiles)
	})
}

// deleteOn deletes the given hashes on a single named instance only. Unlike
// Delete (which fans out to every instance), this is used by dedup where we must
// remove one specific copy while keeping the others.
func (cl *Cluster) deleteOn(ctx context.Context, instance string, hashes []string, deleteFiles bool) error {
	if err := cl.login(ctx); err != nil {
		return err
	}
	for _, n := range cl.nodes {
		if n.name == instance {
			return n.client.DeleteTorrentsCtx(ctx, hashes, deleteFiles)
		}
	}
	return fmt.Errorf("instance %q is not reachable or not in config", instance)
}

// leastLoaded returns the node with the most free disk space. instance, when
// non-empty, forces selection of that named node instead.
func (cl *Cluster) leastLoaded(ctx context.Context, instance string) (node, error) {
	if err := cl.login(ctx); err != nil {
		return node{}, err
	}

	if instance != "" {
		for _, n := range cl.nodes {
			if n.name == instance {
				return n, nil
			}
		}
		return node{}, fmt.Errorf("instance %q is not reachable or not in config", instance)
	}

	type result struct {
		n    node
		free int64
		ok   bool
	}
	results := make([]result, len(cl.nodes))
	var wg sync.WaitGroup
	for i, n := range cl.nodes {
		wg.Add(1)
		go func(i int, n node) {
			defer wg.Done()
			free, err := n.client.GetFreeSpaceOnDiskCtx(ctx)
			if err != nil {
				warnf("instance %q: free-space check failed: %v", n.name, err)
				return
			}
			results[i] = result{n: n, free: free, ok: true}
		}(i, n)
	}
	wg.Wait()

	best := result{free: -1}
	for _, r := range results {
		if r.ok && r.free > best.free {
			best = r
		}
	}
	if !best.ok {
		return node{}, fmt.Errorf("could not determine free space on any instance")
	}
	return best.n, nil
}
