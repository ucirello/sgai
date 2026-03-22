package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func waitForTestCondition(t *testing.T, condition func() bool) {
	t.Helper()
	require.Eventually(t, condition, 2*time.Second, 10*time.Millisecond)
}
