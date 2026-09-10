package dto

// RaftMember identifies a membership mutation and its optimistic-lock index.
type RaftMember struct {
	ID            string
	Address       string
	Suffrage      string
	ExpectedIndex *uint64
}
