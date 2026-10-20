package main

import (
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startAccounts() (*BankAccount, *BankAccount) {
	var bk1 *BankAccount = &BankAccount{
		UUID:    uuid.NewV7(),
		Entries: make([]Entry, 0),
	}

	var bk2 *BankAccount = &BankAccount{
		UUID:    uuid.NewV7(),
		Entries: make([]Entry, 0),
	}

	genesis := Entry{
		Amount: 10000,
		Date:   time.Now().UTC(),
	}

	bk1.Entries = append(bk1.Entries, genesis)
	bk2.Entries = append(bk2.Entries, genesis)

	return bk1, bk2
}

func Test_Trasnfer(t *testing.T) {

	bk1, bk2 := startAccounts()

	err := Transfer(bk1, bk2, 5000)
	require.NoError(t, err)
	require.Equal(t, bk1.Entries[len(bk1.Entries)-1].Amount, int64(-5000))
	require.Equal(t, bk2.Entries[len(bk2.Entries)-1].Amount, int64(5000))
}

func Test_CurrentBalance(t *testing.T) {

	bk1, bk2 := startAccounts()
	err := Transfer(bk1, bk2, 5000)
	require.NoError(t, err)
	assert.Equal(t, int64(5000), bk1.CurrentBalance())
	assert.Equal(t, int64(15000), bk2.CurrentBalance())

}

func Test_Transfer_Concurrent(t *testing.T) {
	bk1, bk2 := startAccounts()

	var wg sync.WaitGroup
	for i := range 1000 {
		wg.Go(func() {
			if i%2 == 0 {
				Transfer(bk1, bk2, 100)
			} else {
				Transfer(bk2, bk1, 100)
			}
		})
	}
	wg.Wait()

	require.Equal(t, int64(20000), bk1.CurrentBalance()+bk2.CurrentBalance())
	require.GreaterOrEqual(t, bk1.CurrentBalance(), int64(0))
	require.GreaterOrEqual(t, bk2.CurrentBalance(), int64(0))
}
