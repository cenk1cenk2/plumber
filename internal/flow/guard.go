package flow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// GuardHandlerFn is the callback of the guards that hand over the control to the caller.
type GuardHandlerFn func()

// DEFAULT_GRACE_TIMEOUT is the duration GuardAlways gives to a job when no other grace period is
// set.
const DEFAULT_GRACE_TIMEOUT = time.Second * 5

// GuardTimeout protects the job from taking too much time on execution. The job is run in its own
// goroutine while the current goroutine waits until the job is finished or the time is out. The
// returned error wraps context.DeadlineExceeded when the time is out.
func GuardTimeout(job Job, timeout time.Duration) Job {
	return guardTimeout(job, timeout, nil)
}

// GuardOnTimeout protects the job from taking too much time on execution. In addition it takes a
// handler that is called when the time is out. The job is run in its own goroutine while the
// current goroutine waits until the job is finished or the time is out.
func GuardOnTimeout(
	job Job,
	fn GuardHandlerFn,
	timeout time.Duration,
) Job {
	return guardTimeout(job, timeout, fn)
}

// GuardPanic protects the flow from a job that falls into panic and returns the panic as an error
// instead. Guarding the job from falling into panic is effective only if the job runs in the
// current goroutine.
func GuardPanic(job Job) Job {
	return func(ctx context.Context) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = newPanicError(recovered)
			}
		}()

		return job(ctx)
	}
}

// GuardIgnorePanic protects the flow from a job that falls into panic and swallows the panic.
func GuardIgnorePanic(job Job) Job {
	return func(ctx context.Context) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = nil
			}
		}()

		return job(ctx)
	}
}

// GuardOnPanic protects the flow from a job that falls into panic. In addition it takes a handler
// that is called in case of panic, the panic itself is swallowed afterwards.
func GuardOnPanic(job Job, fn GuardHandlerFn) Job {
	return func(ctx context.Context) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = nil

				if fn != nil {
					fn()
				}
			}
		}()

		return job(ctx)
	}
}

// GuardIgnoreCancel runs the job in the live context of the flow and swallows its error when it is
// caused by cancellation, so a job that is interrupted does not fail the flow around it while every
// other error still surfaces.
func GuardIgnoreCancel(job Job) Job {
	return func(ctx context.Context) error {
		if err := job(ctx); err != nil && !causedByCancellation(ctx, err) {
			return err
		}

		return nil
	}
}

// GuardResume resumes the execution of the flow that is possibly finished by the job, therefore
// every error of the job is swallowed and only logged when a logger is given.
func GuardResume(job Job, log ...logrus.FieldLogger) Job {
	return func(ctx context.Context) error {
		if err := job(ctx); err != nil {
			if l := resolveLogger(log); l != nil {
				l.Warnf("Job has failed, resuming the flow: %s", err)
			}
		}

		return nil
	}
}

// GuardAlways runs the job even when the flow around it is already cancelled, by detaching it from
// the cancellation of the flow and giving it a grace period instead. Errors that are caused by the
// grace period running out are swallowed, while every other error still surfaces.
func GuardAlways(job Job, grace ...time.Duration) Job {
	timeout := DEFAULT_GRACE_TIMEOUT

	if len(grace) > 0 {
		timeout = grace[0]
	}

	return func(ctx context.Context) error {
		scoped, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()

		if err := job(scoped); err != nil && !causedByCancellation(scoped, err) {
			return err
		}

		return nil
	}
}

// Runs the job in a context that is bound to the given timeout and calls the handler, if there is
// one, when the time is out.
func guardTimeout(job Job, timeout time.Duration, fn GuardHandlerFn) Job {
	return func(ctx context.Context) error {
		scoped, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		done := make(chan error, 1)

		go func() {
			done <- job(scoped)
		}()

		select {
		case err := <-done:
			return err

		case <-scoped.Done():
			if !errors.Is(scoped.Err(), context.DeadlineExceeded) {
				return scoped.Err()
			}

			if fn != nil {
				fn()
			}

			return fmt.Errorf("Job has timed out after %s: %w", timeout, context.DeadlineExceeded)
		}
	}
}

// Reports whether the error of a job is caused by cancellation, which is decided through the state
// of the context the job has been running in instead of the shape of the error, since not every job
// wraps the error of the context it is interrupted in.
func causedByCancellation(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}

	return ctx.Err() != nil
}

// Creates an error out of a recovered panic.
func newPanicError(recovered any) error {
	if err, ok := recovered.(error); ok {
		return fmt.Errorf("Job has panicked: %w", err)
	}

	return fmt.Errorf("Job has panicked: %v", recovered)
}
