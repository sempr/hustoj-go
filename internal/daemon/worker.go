package daemon

import (
	"context"
	"log/slog"
	"time"
)

const (
	OJ_CI = 2 // Compiling & Judging
)

// Worker manages the cycle of fetching and running jobs.
type Worker struct {
	cfg     *Config
	fetcher JobFetcher
	done    chan int    // Channel to receive client IDs of finished jobs
	running map[int]int // Maps clientID to solutionID
}

func NewWorker(cfg *Config, fetcher JobFetcher) *Worker {
	return &Worker{
		cfg:     cfg,
		fetcher: fetcher,
		done:    make(chan int, cfg.MaxRunning),
		running: make(map[int]int),
	}
}

// Run starts the main worker loop.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(w.cfg.SleepTime) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			jobsProcessed := w.work()

			// If in 'once' mode and nothing was processed, exit.
			if w.cfg.Once && jobsProcessed == 0 {
				return
			}

			// If there were no jobs, wait before trying again.
			if jobsProcessed == 0 {
				slog.Debug("Sleeping", "duration_sec", w.cfg.SleepTime)
				<-ticker.C
			}
		}
	}
}

// work performs a single iteration of fetching and assigning jobs.
func (w *Worker) work() int {
	// Clean up finished jobs
	w.cleanupFinishedJobs()

	// Get new jobs
	jobs, err := w.fetcher.GetJobs(w.cfg.MaxRunning)
	if err != nil {
		slog.Error("Could not get jobs", "err", err)
		return 0
	}
	if len(jobs) == 0 && len(w.running) == 0 {
		return 0
	}

	jobCount := 0
	// Assign new jobs
	for _, solutionID := range jobs {
		if len(w.running) >= w.cfg.MaxRunning {
			break // No available slots
		}

		// Find a free clientID
		clientID := -1
		for i := 0; i < w.cfg.MaxRunning; i++ {
			if _, exists := w.running[i]; !exists {
				clientID = i
				break
			}
		}

		if clientID != -1 {
			ok, err := w.fetcher.CheckOut(solutionID, OJ_CI)
			if err != nil {
				slog.Error("Checkout failed for solution", "solution_id", solutionID, "err", err)
				continue
			}
			if ok {
				slog.Info("Starting judgment", "solution_id", solutionID, "client_id", clientID)
				w.running[clientID] = solutionID
				go RunClient(w.cfg, solutionID, clientID, w.done)
				jobCount++
			}
		}
	}
	return jobCount
}

func (w *Worker) cleanupFinishedJobs() {
	for {
		select {
		case clientID := <-w.done:
			solutionID := w.running[clientID]
			slog.Info("Judgment finished", "solution_id", solutionID, "client_id", clientID)
			delete(w.running, clientID)
		default:
			return // No more finished jobs
		}
	}
}
