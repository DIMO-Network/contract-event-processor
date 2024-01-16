package services

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type Case struct {
	Name  string
	Error error
	Func  func() error
}

func TestRetry(t *testing.T) {

	count := 0
	testErr := errors.New("failedTest")
	for _, c := range []Case{
		{
			Name: "No Error",
			Func: func() error {
				return nil
			},
			Error: nil,
		},
		{
			Name: "Error on first call, then succeed",
			Func: func() error {
				if count == 0 {
					count++
					return testErr
				}
				return nil
			},
			Error: nil,
		},
		{
			Name: "Error on all attempts",
			Func: func() error {
				return testErr
			},
			Error: errors.Join(testErr, testErr, testErr),
		},
	} {
		t.Run(c.Name, func(t *testing.T) {
			err := retry(c.Func, 3, time.Microsecond)
			assert.Equal(t, err, c.Error)
		})
	}

}
