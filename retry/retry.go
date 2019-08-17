/*
Package retry provides a simple way to retry operations that
can fail with exponential backoff and jittering.

	var errHi = errors.new("hi")
	var errBye = errors.new("bye")

	func shouldRetry(err error) bool {
		if err == errBye {
			return false
		}
		return true
	}

	func main() {

		d, err := retry.New(shouldRetry, retry.Options{
			Attempts: 5,
			Base: 10,
			Cap: 1000,
			Exponent: 1.5,
			Jitter: 0.5,
		})
		if err != nil {
			panic(err)
		}

		attempts, err := d.Do(func() error {
			f := rand.Float64()
			if f < 0.33 {
				return errBye
			}
			if f < 0.66 {
				return errHi
			}
			return nil
		})
		if err != nil {
			fmt.Printf("after %d attempts: %s", attempts, err.Error())
			return
		}
		fmt.Printf("succeeded after %d attempts", attempts)
	}

*/
package retry

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"
)

/*
	ErrMaxAttempts is returned from Do when it could not complete
	its operation and has tried the maximum allowed times.
*/
var ErrMaxAttempts = errors.New("reached maximum attempts")

/*
	ErrCancelled is returned from Do when the operation it is
	attempting returns an error indicating that no further
	attempts should occur. See Retry for more information.
*/
var ErrCancelled = errors.New("further retries cancelled")

/*
	Retry is a callback that receives errors returned by the fn parameter
	of Do. Retry can test err for particular errors and return a bool
	indicating whether to continue trying the operation or abort. Retry
	will only be called if there is an error - err is never nil.
*/
type Retry func(err error) bool

type Options struct {
	/*
		Attempts is a value of 1 or greater that determines the maximum
		number of times an operation will be retried. It is possible for
		for this number of attempts to never be tried either due to the
		successful execution of the operation or because the retry function
		supplied to Do indicates no further attempts should occur.
	*/
	Attempts int

	/*
		Base is a value greater than 0 that determines the initial delay
		(in milliseconds) before retrying an operation.
	*/
	Base int

	/*
		Cap is a value greater than or equal to Base that determines the
		longest possible time (in milliseconds) Do will wait between calls.
	*/
	Cap int

	/*
		Exponent is a value greater than 1 that determines the growth rate
		of the delay between retries. For example an Exponent would 2 double
		the delay each time meaning a Base of 10 milliseconds on the first
		attempt would become 20 on the second, 40 on the third, and so on.
	*/
	Exponent float64

	/*
	   Jitter is a value between 0 and 1 which is used to determine
	   how much randomness affects wait times between retries.

	   For a wait time of 300:

	       0    // Wait time remains 300
	       0.75 // Wait time is a random number between 225 and 300
	       0.5  // Wait time is a random number between 150 and 300
	       0.25 // Wait time is a random number between 75 and 300
	       1    // Wait time is a random number between 0 and 300

	   An error is returned by New if Jitter is less than 0 or greater
	   than 1.
	*/
	Jitter float64
}

/*
	Exported only for documentation purposes. Use New to initialise a
	new Doer.
*/
type Doer struct {
	attempts int
	cap      float64
	base     float64
	exponent float64
	jitter   float64
	retry    func(error) bool
}

/*
	New returns a Doer with Options o. The retry function is optional -
	if it is nil Do will always retry fn when it fails, up to o.Attempts
	times. See Retry for more information.

	New returns an error if the fields in o contain invalid values. See
	Options for information on what the valid ranges are for each field.
*/
func New(retry Retry, o Options) (*Doer, error) {

	if o.Attempts < 1 {
		return nil, fmt.Errorf("expected an .Attempts value greater than 0, got %d", o.Attempts)
	}

	if o.Base <= 0 {
		return nil, fmt.Errorf("expected .Base to be greater than 0, got %d", o.Base)
	}

	if o.Cap < o.Base {
		return nil, fmt.Errorf(
			"expected .Cap to be greater than .Base, got .Cap %d and .Base %d", o.Cap, o.Base)
	}

	if o.Exponent < 1 {
		return nil, fmt.Errorf(
			"expected .Exponent to be greater than or equal to 1, got %.2f", o.Exponent)
	}

	if o.Jitter < 0 || o.Jitter > 1 {
		return nil, fmt.Errorf("expected a .Jitter value between 0 and 1, got %.2f", o.Jitter)
	}

	return &Doer{
		attempts: o.Attempts,
		base:     float64(o.Base),
		cap:      float64(o.Cap),
		exponent: o.Exponent,
		jitter:   o.Jitter,
		retry:    retry,
	}, nil
}

/*
	Do calls fn repeatedly until it either succeeds, returns an error
	that Retry decides is permanent, or until it has called fn up to the
	maximum number of attempts specified in the Options passed to New.

	Do returns the number of times it attempted to call fn, a slice of errors
	from calls to fn in the order they occured, and an overall error from Do
	indicating whether it was cancelled or reached the maximum attempts.
*/
func (d Doer) Do(fn func() error) (attempts int, errs []error, err error) {

	rand.Seed(time.Now().Unix())

	attempt := 0

	for ; attempt < d.attempts; attempt++ {

		err := fn()
		if err == nil {
			return attempt + 1, errs, nil
		}
		errs = append(errs, err)

		if d.retry != nil && !d.retry(err) {
			attempt++
			return attempt, errs, ErrCancelled
		}

		sleep := d.base * math.Pow(d.exponent, float64(attempt))

		sleep = math.Min(d.cap, sleep)

		sleep *= (1 - (rand.Float64() * d.jitter))

		time.Sleep(time.Millisecond * time.Duration(sleep))
	}

	return attempt, errs, ErrMaxAttempts
}
