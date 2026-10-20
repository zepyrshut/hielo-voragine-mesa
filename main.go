package main

import (
	"fmt"
	"sync"
	"time"
	"uuid"
)

type Entry struct {
	Amount int64
	Date   time.Time
}

type BankAccount struct {
	mu      sync.Mutex
	UUID    uuid.UUID
	Entries []Entry
}

func (ba *BankAccount) CurrentBalance() int64 {
	var value int64 = 0
	for _, v := range ba.Entries {
		value += v.Amount
	}

	return value
}

func Transfer(from, to *BankAccount, amount int64) error {
	now := time.Now().UTC()

	from.mu.Lock()
	to.mu.Lock()
	defer from.mu.Unlock()
	defer to.mu.Unlock()

	from.Entries = append(from.Entries, Entry{
		Amount: -amount,
		Date:   now,
	})

	to.Entries = append(to.Entries, Entry{
		Amount: amount,
		Date:   now,
	})

	return nil
}

func main() {
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

	fmt.Println("bk1", bk1.CurrentBalance())
	fmt.Println("bk2", bk2.CurrentBalance())

	if err := Transfer(bk1, bk2, 5000); err != nil {
		fmt.Errorf("transaction error")
	}

	fmt.Println("bk1", bk1.CurrentBalance())
	fmt.Println("bk2", bk2.CurrentBalance())

}
