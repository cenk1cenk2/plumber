package flow

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type (
	// Job is the unit of work of a flow, it always belongs to the context of the flow it runs in.
	Job func(ctx context.Context) error
	// Predicate is the condition the control jobs branch on.
	Predicate func() bool
)

// JobSequence runs the jobs sequentially, one by one. The context of the flow is checked before
// every job and the first error stops the sequence.
func JobSequence(jobs ...Job) Job {
	return func(ctx context.Context) error {
		for _, job := range jobs {
			if err := ctx.Err(); err != nil {
				return err
			}

			if err := job(ctx); err != nil {
				return err
			}
		}

		return nil
	}
}

// JobParallel runs the jobs in their own goroutines and waits until all of them are finished. The
// first error that is not caused by cancellation cancels the rest of the jobs and is the error that
// is returned, while the errors of the jobs that are cancelled through it are dropped.
func JobParallel(jobs ...Job) Job {
	return func(ctx context.Context) error {
		scoped, cancel := context.WithCancelCause(ctx)
		defer cancel(nil)

		var (
			wg     sync.WaitGroup
			lock   sync.Mutex
			result error
		)

		for _, job := range jobs {
			wg.Add(1)

			go func() {
				defer wg.Done()

				err := job(scoped)

				if err == nil || causedByCancellation(scoped, err) {
					return
				}

				lock.Lock()
				defer lock.Unlock()

				if result == nil {
					result = err

					cancel(err)
				}
			}()
		}

		wg.Wait()

		lock.Lock()
		defer lock.Unlock()

		if result != nil {
			return result
		}

		return ctx.Err()
	}
}

// JobIf runs the first job if the condition is met and runs the second job, if it is given, when
// the condition is not met. The function panics if no or more than two jobs are given.
func JobIf(predicate Predicate, jobs ...Job) Job {
	if len(jobs) == 0 || len(jobs) > 2 {
		panic(fmt.Errorf("Conditional job requires a job and optionally its alternative."))
	}

	return func(ctx context.Context) error {
		if predicate() {
			return jobs[0](ctx)
		}

		if len(jobs) > 1 {
			return jobs[1](ctx)
		}

		return nil
	}
}

// JobIfNot runs the first job if the condition is not met and runs the second job, if it is given,
// when the condition is met. The function panics if no or more than two jobs are given.
func JobIfNot(predicate Predicate, jobs ...Job) Job {
	return JobIf(PredicateNot(predicate), jobs...)
}

// JobWhile repeats running the job while the condition is met.
func JobWhile(predicate Predicate, job Job) Job {
	return func(ctx context.Context) error {
		for predicate() {
			if err := ctx.Err(); err != nil {
				return err
			}

			if err := job(ctx); err != nil {
				return err
			}
		}

		return nil
	}
}

// JobWait waits until the condition is met. The function does not run any job actually and just
// repeatedly checks the return value of the predicate, falling asleep for the given duration
// between the checks. The sleep is interrupted when the flow is cancelled.
func JobWait(predicate Predicate, sleep time.Duration) Job {
	return func(ctx context.Context) error {
		for {
			if err := ctx.Err(); err != nil {
				return err
			}

			if predicate() {
				return nil
			}

			if err := wait(ctx, sleep); err != nil {
				return err
			}
		}
	}
}

// JobLoop repeats running the job until it fails or the flow is cancelled.
func JobLoop(job Job) Job {
	return func(ctx context.Context) error {
		for {
			if err := ctx.Err(); err != nil {
				return err
			}

			if err := job(ctx); err != nil {
				return err
			}
		}
	}
}

// JobLoopWithWaitAfter repeats running the job until it fails or the flow is cancelled, waiting for
// the given duration after every iteration.
func JobLoopWithWaitAfter(job Job, delay time.Duration) Job {
	return JobLoop(
		JobSequence(
			job,
			JobDelay(CreateEmptyJob(), delay),
		),
	)
}

// JobRepeat repeats running the job for the given amount of times.
func JobRepeat(job Job, times int) Job {
	return func(ctx context.Context) error {
		for range times {
			if err := ctx.Err(); err != nil {
				return err
			}

			if err := job(ctx); err != nil {
				return err
			}
		}

		return nil
	}
}

// JobDelay waits for the given duration before starting the job. The wait is interrupted when the
// flow is cancelled, in which case the job is never started.
func JobDelay(job Job, delay time.Duration) Job {
	return func(ctx context.Context) error {
		if err := wait(ctx, delay); err != nil {
			return err
		}

		return job(ctx)
	}
}

// JobBackground starts the job in its own goroutine and returns immediately. The job still runs in
// the context of the flow around it, therefore it is cancelled together with it, but its error can
// not be returned anywhere anymore and is only logged when a logger is given.
func JobBackground(job Job, log ...logrus.FieldLogger) Job {
	return func(ctx context.Context) error {
		go func() {
			if err := job(ctx); err != nil {
				if l := resolveLogger(log); l != nil {
					l.Errorf("Background job has failed: %s", err)
				}
			}
		}()

		return nil
	}
}

// CreateJob creates a job out of a function that does not care about the context of the flow.
func CreateJob(fn func() error) Job {
	return func(_ context.Context) error {
		return fn()
	}
}

// CreateEmptyJob creates a job that does nothing.
func CreateEmptyJob() Job {
	return func(_ context.Context) error {
		return nil
	}
}

// PredicateAnd returns a predicate which chains multiple predicates into a condition with AND
// logic. The result predicate finishes calculation of the condition as fast as the result is known.
//
// The result predicate tests the condition as follows.
//
//	[PRED_1] AND ... AND [PRED_N]
func PredicateAnd(predicates ...Predicate) Predicate {
	return func() bool {
		for _, predicate := range predicates {
			if !predicate() {
				return false
			}
		}

		return true
	}
}

// PredicateOr returns a predicate which chains multiple predicates into a condition with OR logic.
// The result predicate finishes calculation of the condition as fast as the result is known.
//
// The result predicate tests the condition as follows.
//
//	[PRED_1] OR ... OR [PRED_N]
func PredicateOr(predicates ...Predicate) Predicate {
	return func() bool {
		for _, predicate := range predicates {
			if predicate() {
				return true
			}
		}

		return false
	}
}

// PredicateNot returns the negated value of the predicate.
//
// The result predicate tests the condition as follows.
//
//	NOT [PRED]
func PredicateNot(predicate Predicate) Predicate {
	return func() bool {
		return !predicate()
	}
}

// PredicateXor returns a predicate which chains multiple predicates into a condition with XOR
// logic.
//
// The result predicate tests the condition as follows.
//
//	(([PRED_1] XOR [PRED_2]) ... XOR [PRED_N])
func PredicateXor(predicates ...Predicate) Predicate {
	return func() bool {
		result := false

		for i, predicate := range predicates {
			if i == 0 {
				result = predicate()

				continue
			}

			result = result != predicate()
		}

		return result
	}
}

// Waits for the given duration and returns the error of the context if the flow is cancelled while
// waiting.
func wait(ctx context.Context, duration time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if duration <= 0 {
		return nil
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Picks the first logger that is handed over to a combinator, if there is any.
func resolveLogger(log []logrus.FieldLogger) logrus.FieldLogger {
	for _, l := range log {
		if l != nil {
			return l
		}
	}

	return nil
}
