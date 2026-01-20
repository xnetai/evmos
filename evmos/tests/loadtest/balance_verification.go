package loadtest

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
)

func VerifyBalanceChanges(conn *grpc.ClientConn, senders []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := NewYourServiceClient(conn) // Replace with your actual service client
	balanceChanges := make(map[string]int64)

	for _, sender := range senders {
		balance, err := client.GetBalance(ctx, &GetBalanceRequest{Sender: sender}) // Replace with your actual request
		if err != nil {
			return fmt.Errorf("failed to get balance for sender %s: %v", sender, err)
		}
		balanceChanges[sender] = balance.Amount // Adjust according to your response structure
	}

	// Logic to verify balance changes
	for sender, balance := range balanceChanges {
		if balance < 0 { // Example condition for verification
			log.Printf("Balance for sender %s is negative: %d", sender, balance)
			return fmt.Errorf("balance verification failed for sender %s", sender)
		}
	}

	return nil
}