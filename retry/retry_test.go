package retry

import (
	"errors"
	"testing"
	"time"
)

func TestNew(t *testing.T) {

	cases := []struct {
		wantErr bool
		retry   func(error) bool
		opt     Options
	}{

		/*
		   Should return errors.
		*/

		// No options.
		{true, nil, Options{}},

		// Attempts is 0.
		{true, nil, Options{
			Base:        time.Millisecond * 30,
			MaxInterval: time.Second * 1,
			MaxWait:     time.Second * 2,
			Exponent:    2,
			Jitter:      0.5,
		}},

		// Base is 0.
		{true, nil, Options{
			Attempts:    3,
			MaxInterval: time.Second * 1,
			MaxWait:     time.Second * 2,
			Exponent:    2,
			Jitter:      0.5,
		}},

		// Base is greater than MaxInterval.
		{true, nil, Options{
			Attempts: 3,
			Base:     time.Millisecond * 30,
			MaxWait:  time.Second * 2,
			Exponent: 2,
			Jitter:   0.5,
		}},

		// Base is greater than MaxWait.
		{true, nil, Options{
			Attempts:    3,
			Base:        time.Millisecond * 30,
			MaxInterval: time.Second * 1,
			Exponent:    2,
			Jitter:      0.5,
		}},

		// Exponent is less than 1.
		{true, nil, Options{
			Attempts:    3,
			Base:        time.Millisecond * 30,
			MaxInterval: time.Second * 1,
			MaxWait:     time.Second * 2,
			Exponent:    0.5,
			Jitter:      0.5,
		}},

		// Jitter is less than 0.
		{true, nil, Options{
			Attempts:    3,
			Base:        time.Millisecond * 30,
			MaxInterval: time.Second * 1,
			MaxWait:     time.Second * 2,
			Exponent:    0.5,
			Jitter:      -0.5,
		}},

		// Jitter is greater than 1.
		{true, nil, Options{
			Attempts:    3,
			Base:        time.Millisecond * 30,
			MaxInterval: time.Second * 1,
			MaxWait:     time.Second * 2,
			Exponent:    0.5,
			Jitter:      1.5,
		}},

		/*
		   Should not return errors.
		*/
		{false, nil, Options{
			Attempts:    3,
			Base:        time.Millisecond * 30,
			MaxInterval: time.Second * 1,
			MaxWait:     time.Second * 2,
			Exponent:    2,
			Jitter:      0.5,
		}},
	}

	for _, c := range cases {
		if got, err := New(c.retry, c.opt); c.wantErr && err == nil {
			retry := "nil"
			if c.retry != nil {
				retry = "func(error) bool"
			}
			errStr := "nil"
			tryerStr := "Tryer"
			if c.wantErr {
				errStr = "error"
				tryerStr = "nil"
			}
			gotStr := "nil"
			if got != nil {
				gotStr = "Tryer"
			}
			t.Errorf(
				"New(%s, %v)\n"+
					"    return %s, %v\n"+
					"    wanted %s, %s\n",
				retry, c.opt, gotStr, err, tryerStr, errStr)
		}
	}
}

func TestTry(t *testing.T) {

	attempts := 0

	cases := []struct {
		wantErr  error
		wantErrs bool
		maxWait  int // in milliseconds
		retry    Retry
		fn       Operation
	}{
		/*
		   Should return errors.
		*/

		// No fn passed to Try.
		{
			errNoFunc,
			false,
			2000,
			nil,
			nil,
		},

		// Retry should cancel after first attempt
		// since fn immediately returns an error.
		{
			ErrCancelled,
			false,
			2000,
			func(error) bool {
				return false
			},
			func() error {
				return errors.New("test")
			},
		},

		// Try's fn always returns an error therefore
		// we should hit the maximum allowed attempts.
		{
			ErrMaxAttempts,
			false,
			2000,
			nil,
			func() error {
				return errors.New("test")
			},
		},

		// Try's fn always returns an error and the maximum
		// wait time is set just above the base therefore
		// we should timeout after the first attempt.
		{
			ErrTimeout,
			false,
			50,
			nil,
			func() error {
				return errors.New("test")
			},
		},

		/*
		   Should not return err, possibly errs.
		*/

		// Try's fn immediately returns nil, signalling the
		// operation was successfully completed.
		{
			nil,
			false,
			50,
			nil,
			func() error {
				return nil
			},
		},

		// Try's fn returns errors until the third attempt,
		// therefore we expect errs to be non-nil while err
		// should be nil becasue the operation eventually
		// succeeded.
		{
			nil,
			true,
			2000,
			nil,
			func() error {
				attempts++
				if attempts == 3 {
					return nil
				}
				return errors.New("test")
			},
		},
	}

	for _, c := range cases {

		tryer, err := New(c.retry, Options{
			Attempts:    3,
			Base:        time.Millisecond * 30,
			MaxInterval: time.Second * 1,
			MaxWait:     time.Millisecond * time.Duration(c.maxWait),
			Exponent:    2,
			Jitter:      0.5,
		})
		if err != nil {
			t.Error("Failed to initialise Tryer while testing method Try:\n    ", err.Error())
			return
		}

		if errs, err := tryer.Try(c.fn); c.wantErrs && errs == nil || err != c.wantErr {
			fn := "nil"
			if c.fn != nil {
				fn = "func() error"
			}
			errsStr := "nil"
			if c.wantErrs {
				errsStr = "[]error"
			}
			t.Errorf(
				"Tryer.Try(%s)\n"+
					"return %v, %v\n"+
					"wanted %s, %v\n",
				fn, errs, err, errsStr, c.wantErr)
		}
	}
}
