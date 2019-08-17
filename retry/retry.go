/*
Package retry provides a simple way to retry operations that
can fail with exponential backoff and jittering.
*/
package retry

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"
)

type Options struct {
	Attempts int
	Base     int
	Cap      int
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

type Doer struct {
	attempts int
	cap      float64
	base     float64
	exponent float64
	jitter   float64
	retry    func(error) bool
}

// New returns a Doer with Options o. The retry function is passed
// errors return from Do's fn. The return value of retry determines
// whether to continue attempting to calls to Do's fn.
func New(o Options, retry func(error) bool) (*Doer, error) {

	if o.Attempts < 1 {
		return nil, fmt.Errorf("expected an .Attempts value greater than 0, got", o.Attempts)
	}

	if o.Jitter < 0 || o.Jitter > 1 {
		return nil, fmt.Errorf("expected a .Jitter value between 0 and 1, got %.2f", o.Jitter)
	}

	if o.Cap < o.Base {
		return nil, fmt.Errorf(
			"expected .Cap to be greater than .Base, got .Cap %d and .Base %d", o.Cap, o.Base)
	}

	if o.Exponent < 1 {
		return nil, fmt.Errorf(
			"expected .Exponent to be greater than or equal to 1, got", o.Exponent)
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

func (d Doer) Do(fn func() error) (attempts int, err error) {

	rand.Seed(time.Now().Unix())

	attempt := 0

	for ; attempt < d.attempts; attempt++ {

		// Exponential backoff.
		sleep := d.base * math.Pow(d.exponent, float64(attempt))

		// Cap it.
		sleep = math.Min(d.cap, sleep)

		// Add jitter.
		sleep *= (1 - (rand.Float64() * d.jitter))

		time.Sleep(time.Millisecond * time.Duration(sleep))

		err := fn()
		if err == nil {
			return attempt + 1, nil
		}

		if d.retry != nil && !d.retry(err) {
			attempt++
			break
		}
	}

	return attempt, errors.New("failed operation")
}
